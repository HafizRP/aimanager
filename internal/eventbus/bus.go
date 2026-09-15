// Package eventbus provides an in-memory publish-subscribe event system for domain events.
package eventbus

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// EventType identifies the kind of domain event.
type EventType string

const (
	EventKeyCreated         EventType = "key.created"
	EventKeyToggled         EventType = "key.toggled"
	EventKeyDeleted         EventType = "key.deleted"
	EventQuotaResetDetected EventType = "quota.reset_detected"
	EventQuotaExhausted     EventType = "quota.exhausted"
	EventRequestLogged      EventType = "request.logged"
	EventSecurityAlert      EventType = "security.alert"
	EventTransactionSettled EventType = "transaction.settled"
)

// Event represents an event payload dispatched through the bus.
type Event struct {
	Type      EventType   `json:"type"`
	Payload   interface{} `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewEvent helper wraps a payload with the current timestamp.
func NewEvent(t EventType, payload interface{}) Event {
	return Event{
		Type:      t,
		Payload:   payload,
		Timestamp: time.Now(),
	}
}

// Handler is a function subscribed to events.
type Handler func(ctx context.Context, event Event)

// EventBus defines the pub-sub interface.
type EventBus interface {
	Publish(ctx context.Context, event Event)
	Subscribe(eventType EventType, handler Handler) (unsubscribe func())
	SubscribeAll(handler Handler) (unsubscribe func())
	Close()
}

type subscriberEntry struct {
	id uint64
	fn Handler
}

// AsyncEventBus implements an asynchronous, buffered pub-sub bus.
type AsyncEventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]subscriberEntry
	wildcard    []subscriberEntry
	nextID      uint64
	ch          chan Event
	quit        chan struct{}
	wg          sync.WaitGroup
}

// NewAsyncEventBus initializes an event bus with a buffered channel and background dispatcher.
func NewAsyncEventBus(bufferSize int) *AsyncEventBus {
	if bufferSize <= 0 {
		bufferSize = 256
	}

	bus := &AsyncEventBus{
		subscribers: make(map[EventType][]subscriberEntry),
		wildcard:    make([]subscriberEntry, 0),
		ch:          make(chan Event, bufferSize),
		quit:        make(chan struct{}),
	}

	bus.wg.Add(1)
	go bus.dispatchLoop()

	return bus
}

// Subscribe registers a handler for a specific event type. Returns an unsubscribe func.
func (b *AsyncEventBus) Subscribe(eventType EventType, handler Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	b.subscribers[eventType] = append(b.subscribers[eventType], subscriberEntry{id: id, fn: handler})

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		list := b.subscribers[eventType]
		for i, entry := range list {
			if entry.id == id {
				b.subscribers[eventType] = append(list[:i], list[i+1:]...)
				break
			}
		}
	}
}

// SubscribeAll registers a handler that receives all published events.
func (b *AsyncEventBus) SubscribeAll(handler Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	b.wildcard = append(b.wildcard, subscriberEntry{id: id, fn: handler})

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		for i, entry := range b.wildcard {
			if entry.id == id {
				b.wildcard = append(b.wildcard[:i], b.wildcard[i+1:]...)
				break
			}
		}
	}
}

// Publish queues an event for non-blocking asynchronous delivery.
func (b *AsyncEventBus) Publish(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case b.ch <- event:
	case <-b.quit:
	default:
		// Queue full: spawn dedicated worker to prevent blocking callers
		go func() {
			select {
			case b.ch <- event:
			case <-time.After(2 * time.Second):
				log.Warn().Str("event_type", string(event.Type)).Msg("Event bus queue full, event dropped")
			}
		}()
	}
}

func (b *AsyncEventBus) dispatchLoop() {
	defer b.wg.Done()

	for {
		select {
		case event, ok := <-b.ch:
			if !ok {
				return
			}
			b.deliver(event)
		case <-b.quit:
			// Drain remaining events
			for {
				select {
				case event := <-b.ch:
					b.deliver(event)
				default:
					return
				}
			}
		}
	}
}

func (b *AsyncEventBus) deliver(event Event) {
	b.mu.RLock()
	var handlers []Handler
	for _, entry := range b.subscribers[event.Type] {
		handlers = append(handlers, entry.fn)
	}
	for _, entry := range b.wildcard {
		handlers = append(handlers, entry.fn)
	}
	b.mu.RUnlock()

	ctx := context.Background()

	for _, h := range handlers {
		b.safeInvoke(ctx, h, event)
	}
}

func (b *AsyncEventBus) safeInvoke(ctx context.Context, h Handler, event Event) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("event_type", string(event.Type)).Msg("Event handler recovered from panic")
		}
	}()
	h(ctx, event)
}

// Close gracefully stops the event bus and flushes queued events.
func (b *AsyncEventBus) Close() {
	b.mu.Lock()
	select {
	case <-b.quit:
		b.mu.Unlock()
		return
	default:
		close(b.quit)
	}
	b.mu.Unlock()

	b.wg.Wait()
}

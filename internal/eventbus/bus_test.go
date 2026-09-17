package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"9router-gateway/internal/entity"
)

type mockSyncer struct {
	mu          sync.Mutex
	syncedKeys  []string
	toggledKeys []string
	deletedKeys []string
}

func (m *mockSyncer) SyncKey(key *entity.APIKey, userName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncedKeys = append(m.syncedKeys, key.ID)
	return nil
}

func (m *mockSyncer) ToggleKey(keyID string, isActive bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toggledKeys = append(m.toggledKeys, keyID)
	return nil
}

func (m *mockSyncer) DeleteKey(keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedKeys = append(m.deletedKeys, keyID)
	return nil
}

type mockReactivator struct {
	triggered int32
}

func (m *mockReactivator) CheckAndReactivateNow() {
	atomic.AddInt32(&m.triggered, 1)
}

func TestAsyncEventBus_PublishSubscribe(t *testing.T) {
	bus := NewAsyncEventBus(64)
	defer bus.Close()

	var receivedCount int32
	unsub := bus.Subscribe(EventKeyCreated, func(ctx context.Context, e Event) {
		atomic.AddInt32(&receivedCount, 1)
	})

	bus.Publish(context.Background(), NewEvent(EventKeyCreated, "key-1"))
	bus.Publish(context.Background(), NewEvent(EventKeyCreated, "key-2"))

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&receivedCount) != 2 {
		t.Fatalf("expected 2 events received, got %d", receivedCount)
	}

	// Test Unsubscribe
	unsub()
	bus.Publish(context.Background(), NewEvent(EventKeyCreated, "key-3"))
	time.Sleep(50 * time.Millisecond)

	// Count should remain 2
	if atomic.LoadInt32(&receivedCount) != 2 {
		t.Fatalf("expected still 2 events after unsubscribe, got %d", receivedCount)
	}
}

func TestAsyncEventBus_KeySyncerSubscriber(t *testing.T) {
	bus := NewAsyncEventBus(64)
	defer bus.Close()

	syncer := &mockSyncer{}
	RegisterKeySyncerSubscriber(bus, syncer)

	key := &entity.APIKey{ID: "key-123", Name: "Test Key"}
	bus.Publish(context.Background(), NewEvent(EventKeyCreated, KeyPayload{Key: key, UserName: "Admin"}))
	bus.Publish(context.Background(), NewEvent(EventKeyToggled, KeyPayload{KeyID: "key-123", IsActive: false}))
	bus.Publish(context.Background(), NewEvent(EventKeyDeleted, KeyPayload{KeyID: "key-123"}))

	time.Sleep(50 * time.Millisecond)

	syncer.mu.Lock()
	defer syncer.mu.Unlock()

	if len(syncer.syncedKeys) != 1 || syncer.syncedKeys[0] != "key-123" {
		t.Errorf("expected synced key-123, got %v", syncer.syncedKeys)
	}
	if len(syncer.toggledKeys) != 1 || syncer.toggledKeys[0] != "key-123" {
		t.Errorf("expected toggled key-123, got %v", syncer.toggledKeys)
	}
	if len(syncer.deletedKeys) != 1 || syncer.deletedKeys[0] != "key-123" {
		t.Errorf("expected deleted key-123, got %v", syncer.deletedKeys)
	}
}

func TestAsyncEventBus_QuotaReactivatorSubscriber(t *testing.T) {
	bus := NewAsyncEventBus(64)
	defer bus.Close()

	reactivator := &mockReactivator{}
	RegisterQuotaReactivatorSubscriber(bus, reactivator)

	bus.Publish(context.Background(), NewEvent(EventQuotaResetDetected, map[string]string{"provider": "antigravity"}))

	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&reactivator.triggered) != 1 {
		t.Errorf("expected reactivator to be triggered 1 time, got %d", reactivator.triggered)
	}
}

func TestAsyncEventBus_PanicRecovery(t *testing.T) {
	bus := NewAsyncEventBus(64)
	defer bus.Close()

	var safeCallCount int32
	// Panic subscriber
	bus.Subscribe(EventSecurityAlert, func(ctx context.Context, e Event) {
		panic("boom!")
	})
	// Healthy subscriber
	bus.Subscribe(EventSecurityAlert, func(ctx context.Context, e Event) {
		atomic.AddInt32(&safeCallCount, 1)
	})

	bus.Publish(context.Background(), NewEvent(EventSecurityAlert, "alert"))

	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&safeCallCount) != 1 {
		t.Fatalf("expected healthy subscriber to execute despite sibling panic, count: %d", safeCallCount)
	}
}

// Package proxy implements the OpenAI/Anthropic-compatible reverse proxy gateway.
package proxy

import (
	"errors"
	"sync"
	"time"
)

// CircuitState represents the operational state of a circuit breaker.
type CircuitState string

const (
	StateClosed   CircuitState = "closed"    // Normal traffic, all requests allowed
	StateHalfOpen CircuitState = "half_open" // Canary testing, limited requests permitted
	StateOpen     CircuitState = "open"      // Tripped due to failures, requests fail fast
)

// ErrCircuitBreakerOpen is returned when an operation is rejected by an open circuit.
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open: upstream service is unavailable")

// CircuitBreakerSettings configures thresholds and recovery parameters.
type CircuitBreakerSettings struct {
	Name             string
	FailureThreshold int           // Consecutive failures before tripping to Open (default: 3)
	SuccessThreshold int           // Consecutive successes in Half-Open to reset to Closed (default: 2)
	Timeout          time.Duration // Time to remain in Open state before testing Half-Open (default: 30s)
	OnStateChange    func(name string, from CircuitState, to CircuitState)
}

// CircuitBreaker tracks the failure state of an upstream target to prevent cascading timeouts.
type CircuitBreaker struct {
	name             string
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	onStateChange    func(name string, from CircuitState, to CircuitState)

	mu                   sync.RWMutex
	state                CircuitState
	consecutiveFailures  int
	consecutiveSuccesses int
	lastStateChange      time.Time
	lastFailure          time.Time
	totalRequests        int64
	totalFailures        int64
	totalSuccesses       int64
	totalShortCircuits   int64
}

// NewCircuitBreaker creates a circuit breaker with given settings.
func NewCircuitBreaker(settings CircuitBreakerSettings) *CircuitBreaker {
	if settings.FailureThreshold <= 0 {
		settings.FailureThreshold = 3
	}
	if settings.SuccessThreshold <= 0 {
		settings.SuccessThreshold = 2
	}
	if settings.Timeout <= 0 {
		settings.Timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		name:             settings.Name,
		failureThreshold: settings.FailureThreshold,
		successThreshold: settings.SuccessThreshold,
		timeout:          settings.Timeout,
		onStateChange:    settings.OnStateChange,
		state:            StateClosed,
		lastStateChange:  time.Now(),
	}
}

// Allow determines if an outbound request is permitted through the circuit.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++
	now := time.Now()

	// If Open, check if timeout has elapsed to transition to Half-Open
	if cb.state == StateOpen {
		if now.Sub(cb.lastStateChange) >= cb.timeout {
			cb.transitionTo(StateHalfOpen, now)
			return true
		}
		cb.totalShortCircuits++
		return false
	}

	return true
}

// RecordSuccess registers a successful upstream response.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses++
	cb.consecutiveFailures = 0

	if cb.state == StateHalfOpen {
		cb.consecutiveSuccesses++
		if cb.consecutiveSuccesses >= cb.successThreshold {
			cb.transitionTo(StateClosed, time.Now())
		}
	}
}

// RecordFailure registers an upstream failure (e.g. 5xx or connection timeout).
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.totalFailures++
	cb.lastFailure = now
	cb.consecutiveFailures++
	cb.consecutiveSuccesses = 0

	if cb.state == StateClosed && cb.consecutiveFailures >= cb.failureThreshold {
		cb.transitionTo(StateOpen, now)
	} else if cb.state == StateHalfOpen {
		// In Half-Open, a single failure immediately trips back to Open
		cb.transitionTo(StateOpen, now)
	}
}

// Execute wraps an upstream action with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.Allow() {
		return ErrCircuitBreakerOpen
	}

	err := fn()
	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Check if Open state should naturally transition to Half-Open
	if cb.state == StateOpen && time.Since(cb.lastStateChange) >= cb.timeout {
		return StateHalfOpen
	}
	return cb.state
}

// Reset restores the circuit breaker to closed initial state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionTo(StateClosed, time.Now())
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
}

func (cb *CircuitBreaker) transitionTo(next CircuitState, now time.Time) {
	if cb.state == next {
		return
	}
	from := cb.state
	cb.state = next
	cb.lastStateChange = now
	if next == StateClosed {
		cb.consecutiveFailures = 0
		cb.consecutiveSuccesses = 0
	} else if next == StateOpen {
		cb.consecutiveSuccesses = 0
	}

	if cb.onStateChange != nil {
		go cb.onStateChange(cb.name, from, next)
	}
}

// CircuitBreakerSnapshot holds serialized metrics for monitoring.
type CircuitBreakerSnapshot struct {
	Name                string       `json:"name"`
	State               CircuitState `json:"state"`
	ConsecutiveFailures int          `json:"consecutive_failures"`
	TotalRequests       int64        `json:"total_requests"`
	TotalFailures       int64        `json:"total_failures"`
	TotalSuccesses      int64        `json:"total_successes"`
	TotalShortCircuits  int64        `json:"total_short_circuits"`
	LastStateChange     time.Time    `json:"last_state_change"`
}

// Snapshot returns a point-in-time metrics view.
func (cb *CircuitBreaker) Snapshot() CircuitBreakerSnapshot {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state := cb.state
	if state == StateOpen && time.Since(cb.lastStateChange) >= cb.timeout {
		state = StateHalfOpen
	}

	return CircuitBreakerSnapshot{
		Name:                cb.name,
		State:               state,
		ConsecutiveFailures: cb.consecutiveFailures,
		TotalRequests:       cb.totalRequests,
		TotalFailures:       cb.totalFailures,
		TotalSuccesses:      cb.totalSuccesses,
		TotalShortCircuits:  cb.totalShortCircuits,
		LastStateChange:     cb.lastStateChange,
	}
}

// CircuitBreakerRegistry manages circuit breakers for upstream targets (providers, models, core).
type CircuitBreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	settings CircuitBreakerSettings
}

// NewCircuitBreakerRegistry creates a registry with default settings.
func NewCircuitBreakerRegistry(defaultSettings CircuitBreakerSettings) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		settings: defaultSettings,
	}
}

// GetOrCreate returns an existing breaker by name or creates a new one.
func (r *CircuitBreakerRegistry) GetOrCreate(name string) *CircuitBreaker {
	r.mu.RLock()
	cb, exists := r.breakers[name]
	r.mu.RUnlock()
	if exists {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, exists = r.breakers[name]; exists {
		return cb
	}

	settings := r.settings
	settings.Name = name
	cb = NewCircuitBreaker(settings)
	r.breakers[name] = cb
	return cb
}

// Get returns an existing breaker if present.
func (r *CircuitBreakerRegistry) Get(name string) (*CircuitBreaker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cb, exists := r.breakers[name]
	return cb, exists
}

// Snapshots returns snapshots of all registered circuit breakers.
func (r *CircuitBreakerRegistry) Snapshots() map[string]CircuitBreakerSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]CircuitBreakerSnapshot, len(r.breakers))
	for k, cb := range r.breakers {
		result[k] = cb.Snapshot()
	}
	return result
}

// Reset resets the state and metrics of a specific circuit breaker by name.
func (r *CircuitBreakerRegistry) Reset(name string) bool {
	r.mu.RLock()
	cb, exists := r.breakers[name]
	r.mu.RUnlock()
	if !exists {
		return false
	}
	cb.Reset()
	return true
}

// ResetAll resets all registered circuit breakers to Closed state.
func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cb := range r.breakers {
		cb.Reset()
	}
}


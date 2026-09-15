package proxy

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerSettings{
		Name:             "test-breaker",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	})

	if cb.State() != StateClosed {
		t.Fatalf("expected initial state Closed, got %s", cb.State())
	}

	// 1. Two failures: should remain Closed
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed after 2 failures, got %s", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected request to be allowed while Closed")
	}

	// 2. Third failure: trips to Open
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected state Open after 3 failures, got %s", cb.State())
	}

	// 3. In Open state: requests must fail fast (short circuit)
	if cb.Allow() {
		t.Fatal("expected request to be rejected when Open")
	}

	// Execute should return ErrCircuitBreakerOpen
	err := cb.Execute(func() error {
		t.Fatal("action should not be executed when circuit is open")
		return nil
	})
	if !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Fatalf("expected ErrCircuitBreakerOpen, got %v", err)
	}

	// 4. Wait for timeout -> should transition to Half-Open
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("expected request to be allowed to probe in Half-Open state")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected state Half-Open after timeout, got %s", cb.State())
	}

	// 5. Success 1 in Half-Open -> still Half-Open
	cb.RecordSuccess()
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected state Half-Open after 1 success, got %s", cb.State())
	}

	// 6. Success 2 in Half-Open -> resets to Closed
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed after reaching success threshold, got %s", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected request to be allowed in Closed state")
	}
}

func TestCircuitBreaker_HalfOpenFailureTripsOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerSettings{
		Name:             "test-half-open-fail",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          30 * time.Millisecond,
	})

	// Trip to Open
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected state Open, got %s", cb.State())
	}

	// Wait for Half-Open
	time.Sleep(35 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("expected Allow() to transition to Half-Open")
	}

	// A single failure in Half-Open must immediately trip back to Open
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected immediate transition to Open on Half-Open failure, got %s", cb.State())
	}
	if cb.Allow() {
		t.Fatal("expected request to be rejected after re-trip")
	}
}

func TestCircuitBreakerRegistry(t *testing.T) {
	registry := NewCircuitBreakerRegistry(CircuitBreakerSettings{
		FailureThreshold: 3,
		Timeout:          50 * time.Millisecond,
	})

	cb1 := registry.GetOrCreate("provider:antigravity")
	cb2 := registry.GetOrCreate("provider:antigravity")
	if cb1 != cb2 {
		t.Fatal("expected same instance from registry")
	}

	cb3 := registry.GetOrCreate("provider:kiro")
	if cb1 == cb3 {
		t.Fatal("expected different instances for different targets")
	}

	snaps := registry.Snapshots()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	if _, ok := snaps["provider:antigravity"]; !ok {
		t.Fatal("expected provider:antigravity in snapshots")
	}
}

package resilience

import (
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 2, 100*time.Millisecond)
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed, got %s", cb.GetState())
	}
	if !cb.Allow() {
		t.Error("expected allow in closed state")
	}
}

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 2, 100*time.Millisecond)

	// Record failures below threshold — should stay closed.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed after 2 failures, got %s", cb.GetState())
	}

	// Third failure trips the breaker.
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Errorf("expected open after 3 failures, got %s", cb.GetState())
	}
	if cb.Allow() {
		t.Error("expected reject in open state")
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 2, 50*time.Millisecond)

	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Fatalf("expected open, got %s", cb.GetState())
	}

	// Wait for cooldown.
	time.Sleep(60 * time.Millisecond)

	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected half-open after cooldown, got %s", cb.GetState())
	}
	if !cb.Allow() {
		t.Error("expected allow in half-open state")
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 2, 50*time.Millisecond)

	// Trip the breaker.
	cb.RecordFailure()

	// Wait for cooldown to transition to half-open.
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // Triggers half-open transition.

	// Record enough successes to close.
	cb.RecordSuccess()
	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected half-open after 1 success, got %s", cb.GetState())
	}
	cb.RecordSuccess()
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed after 2 successes in half-open, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 2, 50*time.Millisecond)

	// Trip the breaker.
	cb.RecordFailure()

	// Wait for cooldown.
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // Triggers half-open.

	// Failure in half-open immediately re-opens.
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Errorf("expected open after failure in half-open, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_SuccessResetsClosed(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 2, 100*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // Should reset failure count.
	cb.RecordFailure()
	cb.RecordFailure()

	// Should still be closed because success reset the counter.
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed after success reset, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 2, time.Hour)

	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Fatalf("expected open, got %s", cb.GetState())
	}

	cb.Reset()
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed after reset, got %s", cb.GetState())
	}
	if !cb.Allow() {
		t.Error("expected allow after reset")
	}
}

func TestBreakerRegistry_LazyCreate(t *testing.T) {
	reg := NewBreakerRegistry(BreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Cooldown:         time.Second,
	})

	cb1 := reg.Get("peer-abc")
	if cb1 == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
	if cb1.Name() != "peer-abc" {
		t.Errorf("expected name 'peer-abc', got %q", cb1.Name())
	}

	// Same name should return same instance.
	cb2 := reg.Get("peer-abc")
	if cb1 != cb2 {
		t.Error("expected same circuit breaker instance")
	}
}

func TestBreakerRegistry_List(t *testing.T) {
	reg := NewBreakerRegistry(BreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Cooldown:         time.Second,
	})

	reg.Get("peer-a")
	cb := reg.Get("peer-b")
	cb.RecordFailure() // Open peer-b.

	statuses := reg.List()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 breakers, got %d", len(statuses))
	}
	if statuses["peer-a"].State != StateClosed {
		t.Errorf("expected peer-a closed, got %s", statuses["peer-a"].State)
	}
	if statuses["peer-b"].State != StateOpen {
		t.Errorf("expected peer-b open, got %s", statuses["peer-b"].State)
	}
}

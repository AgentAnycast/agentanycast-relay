// Package resilience provides circuit breaker and other resilience patterns.
package resilience

import (
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State string

const (
	// StateClosed is the normal operating state. Requests flow through.
	StateClosed State = "closed"
	// StateOpen indicates the circuit is tripped. Requests are rejected.
	StateOpen State = "open"
	// StateHalfOpen is the recovery testing state. A limited number of
	// requests are allowed through to probe whether the downstream has recovered.
	StateHalfOpen State = "half-open"
)

// CircuitBreaker implements the circuit breaker pattern to prevent cascading
// failures in distributed systems.
type CircuitBreaker struct {
	mu               sync.Mutex
	name             string
	state            State
	failureCount     int
	failureThreshold int
	successCount     int // Consecutive successes while in half-open.
	successThreshold int // Required successes to close from half-open.
	openUntil        time.Time
	cooldown         time.Duration
}

// NewCircuitBreaker creates a new circuit breaker with the given thresholds.
func NewCircuitBreaker(name string, failureThreshold, successThreshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		cooldown:         cooldown,
	}
}

// Allow checks if a request should be allowed through.
// Returns true if the circuit is closed or if enough cooldown time has passed
// (transitioning to half-open).
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Now().After(cb.openUntil) {
			cb.state = StateHalfOpen
			cb.successCount = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failureCount = 0
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
			cb.openUntil = time.Now().Add(cb.cooldown)
		}
	case StateHalfOpen:
		// Any failure in half-open immediately trips the breaker again.
		cb.state = StateOpen
		cb.openUntil = time.Now().Add(cb.cooldown)
		cb.successCount = 0
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) GetState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if an open breaker should transition to half-open.
	if cb.state == StateOpen && time.Now().After(cb.openUntil) {
		cb.state = StateHalfOpen
		cb.successCount = 0
	}
	return cb.state
}

// Reset resets the circuit breaker to the closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.openUntil = time.Time{}
}

// Name returns the circuit breaker's name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

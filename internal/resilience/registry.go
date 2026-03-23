package resilience

import (
	"sync"
	"time"
)

// BreakerConfig holds default configuration for new circuit breakers.
type BreakerConfig struct {
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	Cooldown         time.Duration `json:"cooldown"`
}

// BreakerStatus represents the observable state of a circuit breaker.
type BreakerStatus struct {
	Name  string `json:"name"`
	State State  `json:"state"`
}

// BreakerRegistry manages a collection of named circuit breakers.
// Breakers are lazily created on first access using default configuration.
type BreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	defaults BreakerConfig
}

// NewBreakerRegistry creates a new registry with the given default configuration.
func NewBreakerRegistry(defaults BreakerConfig) *BreakerRegistry {
	return &BreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		defaults: defaults,
	}
}

// Get retrieves a circuit breaker by name, creating one with default
// configuration if it does not exist.
func (r *BreakerRegistry) Get(name string) *CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[name]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if cb, ok = r.breakers[name]; ok {
		return cb
	}

	cb = NewCircuitBreaker(name, r.defaults.FailureThreshold, r.defaults.SuccessThreshold, r.defaults.Cooldown)
	r.breakers[name] = cb
	return cb
}

// List returns the current status of all circuit breakers.
func (r *BreakerRegistry) List() map[string]BreakerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]BreakerStatus, len(r.breakers))
	for name, cb := range r.breakers {
		result[name] = BreakerStatus{
			Name:  name,
			State: cb.GetState(),
		}
	}
	return result
}

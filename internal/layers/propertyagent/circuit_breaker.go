package propertyagent

import (
	"errors"
	"sync"
	"time"
)

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation, requests allowed
	CircuitOpen                         // Failing, requests blocked
	CircuitHalfOpen                     // Testing if service recovered
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker implements the circuit breaker pattern for AI calls.
type CircuitBreaker struct {
	config CircuitBreakerConfig
	debug  bool

	mu              sync.RWMutex
	state           CircuitState
	failureCount    int
	lastFailureTime time.Time
	lastStateChange time.Time
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig, debug bool) *CircuitBreaker {
	return &CircuitBreaker{
		config:          config,
		debug:           debug,
		state:           CircuitClosed,
		failureCount:    0,
		lastStateChange: time.Now(),
	}
}

// Allow checks if a request should be allowed through.
func (cb *CircuitBreaker) Allow() error {
	if !cb.config.Enabled {
		return nil
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return nil

	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.config.ResetTimeout {
			cb.state = CircuitHalfOpen
			cb.lastStateChange = time.Now()
			if cb.debug {
				println("[DEBUG] [PropertyAgent CircuitBreaker] Transitioning to half-open state")
			}
			return nil
		}
		return ErrCircuitOpen

	case CircuitHalfOpen:
		return nil
	}

	return nil
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		cb.failureCount = 0
		cb.lastStateChange = time.Now()
		if cb.debug {
			println("[DEBUG] [PropertyAgent CircuitBreaker] Transitioning to closed state (recovered)")
		}
	} else if cb.state == CircuitClosed {
		cb.failureCount = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.debug {
		println("[DEBUG] [PropertyAgent CircuitBreaker] Failure recorded, count:", cb.failureCount)
	}

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
		if cb.debug {
			println("[DEBUG] [PropertyAgent CircuitBreaker] Transitioning to open state (failed in half-open)")
		}
	} else if cb.state == CircuitClosed && cb.failureCount >= cb.config.FailureThreshold {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
		if cb.debug {
			println("[DEBUG] [PropertyAgent CircuitBreaker] Transitioning to open state (threshold reached)")
		}
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns current circuit breaker statistics.
func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":             cb.state.String(),
		"failure_count":     cb.failureCount,
		"failure_threshold": cb.config.FailureThreshold,
		"reset_timeout":     cb.config.ResetTimeout.String(),
		"last_state_change": cb.lastStateChange,
	}
}

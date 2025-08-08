package workflow

import (
	"fmt"
	"sync"
	"time"
)

// CircuitBreakerState represents the state of the circuit breaker
type CircuitBreakerState string

const (
	// CircuitClosed allows requests to pass through
	CircuitClosed CircuitBreakerState = "closed"
	// CircuitOpen blocks all requests
	CircuitOpen CircuitBreakerState = "open"
	// CircuitHalfOpen allows limited requests for testing
	CircuitHalfOpen CircuitBreakerState = "half-open"
)

// CircuitBreaker implements the circuit breaker pattern for worker failures
type CircuitBreaker struct {
	mu             sync.RWMutex
	failures       int
	lastFailure    time.Time
	state          CircuitBreakerState
	successCount   int
	halfOpenStart  time.Time
	
	// Configuration
	failureThreshold   int
	successThreshold   int
	halfOpenTimeout    time.Duration
	openStateDuration  time.Duration
}

// NewCircuitBreaker creates a new circuit breaker with default settings
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		state:              CircuitClosed,
		failureThreshold:   5,
		successThreshold:   3,
		halfOpenTimeout:    30 * time.Second,
		openStateDuration:  30 * time.Second,
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	if cb.state == CircuitHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			// Enough successes, close the circuit
			cb.state = CircuitClosed
			cb.failures = 0
			cb.successCount = 0
		}
	} else if cb.state == CircuitClosed {
		cb.failures = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.failures++
	cb.lastFailure = time.Now()
	
	if cb.state == CircuitHalfOpen {
		// Failure in half-open state, go back to open
		cb.state = CircuitOpen
		cb.successCount = 0
		return fmt.Errorf("circuit breaker open - too many failures")
	}
	
	if cb.failures >= cb.failureThreshold {
		cb.state = CircuitOpen
		return fmt.Errorf("circuit breaker open - failure threshold exceeded")
	}
	
	return nil
}

// CanExecute checks if an operation can be executed
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	if cb.state == CircuitClosed || cb.state == CircuitHalfOpen {
		return true
	}
	
	if cb.state == CircuitOpen {
		// Check if we should try half-open
		if time.Since(cb.lastFailure) > cb.openStateDuration {
			cb.state = CircuitHalfOpen
			cb.halfOpenStart = time.Now()
			cb.successCount = 0
			return true
		}
	}
	
	return false
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.state = CircuitClosed
	cb.failures = 0
	cb.successCount = 0
	cb.lastFailure = time.Time{}
	cb.halfOpenStart = time.Time{}
}
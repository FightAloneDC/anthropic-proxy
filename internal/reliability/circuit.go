package reliability

import (
	"sync"
	"time"
)

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type CircuitBreaker struct {
	mu          sync.Mutex
	enabled     bool
	threshold   int
	cooldown    time.Duration
	state       CircuitState
	failures    int
	openedAt    time.Time
	probeActive bool
	lastSuccess time.Time
	lastFailure time.Time
	lastError   string
}

type CircuitSnapshot struct {
	Enabled     bool         `json:"enabled"`
	State       CircuitState `json:"state"`
	Failures    int          `json:"failures"`
	LastSuccess string       `json:"last_success,omitempty"`
	LastFailure string       `json:"last_failure,omitempty"`
	LastError   string       `json:"last_error,omitempty"`
}

func NewCircuitBreaker(enabled bool, threshold int, cooldown time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreaker{enabled: enabled, threshold: threshold, cooldown: cooldown, state: CircuitClosed}
}

func (cb *CircuitBreaker) Allow() bool {
	if cb == nil || !cb.enabled {
		return true
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitClosed {
		return true
	}
	if cb.state == CircuitOpen && time.Since(cb.openedAt) >= cb.cooldown {
		cb.state = CircuitHalfOpen
		cb.probeActive = false
	}
	if cb.state == CircuitHalfOpen && !cb.probeActive {
		cb.probeActive = true
		return true
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	if cb == nil || !cb.enabled {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = CircuitClosed
	cb.probeActive = false
	cb.lastSuccess = time.Now()
	cb.lastError = ""
}

func (cb *CircuitBreaker) RecordFailure(err string) {
	if cb == nil || !cb.enabled {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = time.Now()
	cb.lastError = err
	cb.probeActive = false
	if cb.state == CircuitHalfOpen || cb.failures >= cb.threshold {
		cb.state = CircuitOpen
		cb.openedAt = time.Now()
	}
}

func (cb *CircuitBreaker) Snapshot() CircuitSnapshot {
	if cb == nil {
		return CircuitSnapshot{State: CircuitClosed}
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	snapshot := CircuitSnapshot{Enabled: cb.enabled, State: cb.state, Failures: cb.failures, LastError: cb.lastError}
	if !cb.lastSuccess.IsZero() {
		snapshot.LastSuccess = cb.lastSuccess.Format(time.RFC3339)
	}
	if !cb.lastFailure.IsZero() {
		snapshot.LastFailure = cb.lastFailure.Format(time.RFC3339)
	}
	return snapshot
}

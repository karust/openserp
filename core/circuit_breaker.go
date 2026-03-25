package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
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

type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	SuccessThreshold int
}

func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  60 * time.Second,
		SuccessThreshold: 2,
	}
}

// CircuitBreaker tracks failure state for one engine.
type CircuitBreaker struct {
	mu              sync.RWMutex
	name            string
	state           CircuitState
	config          CircuitBreakerConfig
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	lastStateChange time.Time
}

func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:            name,
		state:           CircuitClosed,
		config:          cfg,
		lastStateChange: time.Now(),
	}
}

func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
			cb.setState(CircuitHalfOpen)
			logrus.Infof("[CircuitBreaker][%s] Recovery timeout elapsed, moving to half-open", cb.name)
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return true
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.setState(CircuitClosed)
			cb.failureCount = 0
			cb.successCount = 0
			logrus.Infof("[CircuitBreaker][%s] Recovered, circuit closed", cb.name)
		}
	case CircuitClosed:
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failureCount++
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.setState(CircuitOpen)
			logrus.Warnf("[CircuitBreaker][%s] Circuit OPENED after %d consecutive failures (will retry in %s)",
				cb.name, cb.failureCount, cb.config.RecoveryTimeout)
		}
	case CircuitHalfOpen:
		cb.setState(CircuitOpen)
		cb.successCount = 0
		logrus.Warnf("[CircuitBreaker][%s] Failed during half-open, circuit re-opened", cb.name)
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := map[string]interface{}{
		"engine":        cb.name,
		"state":         cb.state.String(),
		"failure_count": cb.failureCount,
		"last_changed":  cb.lastStateChange.Format(time.RFC3339),
	}

	if cb.state == CircuitOpen {
		remaining := cb.config.RecoveryTimeout - time.Since(cb.lastFailureTime)
		if remaining < 0 {
			remaining = 0
		}

		// Expose retry_in as integer seconds for easier client-side processing.
		retryInSeconds := int64(0)
		if remaining > 0 {
			retryInSeconds = int64((remaining + time.Second - time.Nanosecond) / time.Second)
		}
		stats["retry_in"] = retryInSeconds
	}

	return stats
}

func (cb *CircuitBreaker) setState(state CircuitState) {
	cb.state = state
	cb.lastStateChange = time.Now()
}

type CircuitBreakerManager struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

func NewCircuitBreakerManager(cfg CircuitBreakerConfig) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		config:   cfg,
	}
}

func (m *CircuitBreakerManager) Get(engineName string) *CircuitBreaker {
	m.mu.RLock()
	if cb, ok := m.breakers[engineName]; ok {
		m.mu.RUnlock()
		return cb
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if cb, ok := m.breakers[engineName]; ok {
		return cb
	}

	cb := NewCircuitBreaker(engineName, m.config)
	m.breakers[engineName] = cb
	return cb
}

func (m *CircuitBreakerManager) AllStats() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]map[string]interface{}, 0, len(m.breakers))
	for _, cb := range m.breakers {
		stats = append(stats, cb.Stats())
	}
	return stats
}

var ErrCircuitOpen = fmt.Errorf("circuit breaker is open - engine temporarily disabled")

package core

import (
	"testing"
	"time"
)

func newTestCircuitBreaker(t *testing.T, cfg CircuitBreakerConfig) *CircuitBreaker {
	t.Helper()
	return NewCircuitBreaker("test-engine", cfg)
}

// TestCircuitBreaker_OpensAfterThreshold verifies that consecutive failures in closed state
// move the breaker to open exactly on configured threshold and block new requests.
func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  time.Second,
		SuccessThreshold: 1,
	}
	cb := newTestCircuitBreaker(t, cfg)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after 2 failures, got: %s", cb.State())
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after %d failures, got: %s", cfg.FailureThreshold, cb.State())
	}
	if cb.AllowRequest() {
		t.Error("expected request blocked in open state")
	}
}

// TestCircuitBreaker_RecoveryToHalfOpen verifies timed recovery from open to half-open
// when recovery timeout elapses and a new request is attempted.
func TestCircuitBreaker_RecoveryToHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  50 * time.Millisecond,
		SuccessThreshold: 1,
	}
	cb := newTestCircuitBreaker(t, cfg)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("expected open")
	}

	time.Sleep(60 * time.Millisecond)
	if !cb.AllowRequest() {
		t.Error("should allow request after recovery timeout")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected half-open, got: %s", cb.State())
	}
}

// TestCircuitBreaker_HalfOpenSuccessClosesCircuit verifies that half-open state closes
// only after configured number of successful probes.
func TestCircuitBreaker_HalfOpenSuccessClosesCircuit(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  20 * time.Millisecond,
		SuccessThreshold: 2,
	}
	cb := newTestCircuitBreaker(t, cfg)

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got: %s", cb.State())
	}

	time.Sleep(30 * time.Millisecond)
	if !cb.AllowRequest() {
		t.Fatal("expected request to pass in recovery window")
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after recovery timeout, got: %s", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected to stay half-open until success threshold reached, got: %s", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after success threshold reached, got: %s", cb.State())
	}
}

// TestCircuitBreaker_HalfOpenFailureReopens verifies that a failed probe in half-open
// immediately re-opens the circuit.
func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  20 * time.Millisecond,
		SuccessThreshold: 1,
	}
	cb := newTestCircuitBreaker(t, cfg)

	cb.RecordFailure()
	time.Sleep(30 * time.Millisecond)
	if !cb.AllowRequest() {
		t.Fatal("expected probe request in half-open")
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got: %s", cb.State())
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after failed half-open probe, got: %s", cb.State())
	}
}

// TestCircuitBreaker_Stats verifies stats payload fields and that retry_in is exposed
// only when breaker is open.
func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker("test-engine", DefaultCircuitBreakerConfig())
	cb.RecordFailure()

	stats := cb.Stats()
	if stats["engine"] != "test-engine" {
		t.Fatalf("expected engine=test-engine, got: %v", stats["engine"])
	}
	if stats["state"] != "closed" {
		t.Fatalf("expected state=closed, got: %v", stats["state"])
	}
	if stats["failure_count"].(int) != 1 {
		t.Fatalf("expected failure_count=1, got: %v", stats["failure_count"])
	}
	if _, ok := stats["retry_in"]; ok {
		t.Fatalf("did not expect retry_in in closed state, got: %v", stats["retry_in"])
	}

	openCfg := CircuitBreakerConfig{FailureThreshold: 1, RecoveryTimeout: time.Second, SuccessThreshold: 1}
	openCB := NewCircuitBreaker("open-engine", openCfg)
	openCB.RecordFailure()
	openStats := openCB.Stats()
	retryIn, ok := openStats["retry_in"].(int64)
	if !ok {
		t.Fatalf("expected retry_in int64 in open state, got: %T", openStats["retry_in"])
	}
	if retryIn <= 0 {
		t.Fatalf("expected retry_in > 0 in open state, got: %d", retryIn)
	}
}

// TestCircuitBreakerManager_AllStats verifies manager creates and reports per-engine breakers.
func TestCircuitBreakerManager_AllStats(t *testing.T) {
	mgr := NewCircuitBreakerManager(DefaultCircuitBreakerConfig())
	mgr.Get("google")
	mgr.Get("yandex")

	stats := mgr.AllStats()
	if len(stats) != 2 {
		t.Errorf("expected 2 entries, got: %d", len(stats))
	}
}

package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type engineMock struct {
	name        string
	initialized bool
	limiter     *rate.Limiter
	searchFn    func(Query) ([]SearchResult, error)
	imageFn     func(Query) ([]SearchResult, error)

	mu          sync.Mutex
	searchCalls int
}

func (e *engineMock) Name() string { return e.name }
func (e *engineMock) IsInitialized() bool {
	return e.initialized
}
func (e *engineMock) GetRateLimiter() *rate.Limiter { return e.limiter }

func (e *engineMock) Search(q Query) ([]SearchResult, error) {
	e.mu.Lock()
	e.searchCalls++
	e.mu.Unlock()
	if e.searchFn != nil {
		return e.searchFn(q)
	}
	return []SearchResult{{Rank: 1, URL: "https://example.com/" + e.name, Title: e.name}}, nil
}

func (e *engineMock) SearchImage(q Query) ([]SearchResult, error) {
	if e.imageFn != nil {
		return e.imageFn(q)
	}
	return []SearchResult{{Rank: 1, URL: "https://img.example.com/" + e.name, Title: e.name}}, nil
}

func request(t *testing.T, s *Server, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed for %s: %v", path, err)
	}
	return resp
}

func TestHealthEndpointStatusSemantics(t *testing.T) {
	ready := &engineMock{name: "google", initialized: true, limiter: rate.NewLimiter(rate.Every(time.Second), 1)}
	notReady := &engineMock{name: "yandex", initialized: false, limiter: rate.NewLimiter(rate.Every(time.Second), 1)}

	srv := NewServerWithOptions("127.0.0.1", 7070, DefaultServerOptions(), ready, notReady)
	resp := request(t, srv, "/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected degraded health to return 200, got %d", resp.StatusCode)
	}

	var health HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if health.Status != "degraded" {
		t.Fatalf("expected degraded status, got %s", health.Status)
	}

	down := &engineMock{name: "google", initialized: false, limiter: rate.NewLimiter(rate.Every(time.Second), 1)}
	srvUnhealthy := NewServerWithOptions("127.0.0.1", 7071, DefaultServerOptions(), down)
	resp = request(t, srvUnhealthy, "/health")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected unhealthy health to return 503, got %d", resp.StatusCode)
	}
}

func TestDedicatedEndpointNoFallbackByDefault(t *testing.T) {
	primary := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			return nil, errors.New("primary failed")
		},
	}
	fallback := &engineMock{name: "yandex", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = false
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7072, opts, primary, fallback)

	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when primary fails and fallback disabled, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Fallback-Engine"); got != "" {
		t.Fatalf("unexpected fallback header: %s", got)
	}
	if fallback.searchCalls != 0 {
		t.Fatalf("fallback engine should not be called, got %d calls", fallback.searchCalls)
	}
}

func TestResilienceStatsContainsRetryInWhenCircuitOpen(t *testing.T) {
	primary := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			return nil, errors.New("forced failure")
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.Resilience.CircuitBreaker.FailureThreshold = 1
	opts.Resilience.CircuitBreaker.RecoveryTimeout = 5 * time.Minute
	srv := NewServerWithOptions("127.0.0.1", 7074, opts, primary)

	_ = request(t, srv, "/google/search?text=golang")
	statsResp := request(t, srv, "/resilience/stats")
	if statsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected stats endpoint to return 200, got %d", statsResp.StatusCode)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}

	breakers, ok := stats["circuit_breakers"].([]interface{})
	if !ok || len(breakers) == 0 {
		t.Fatalf("expected at least one circuit breaker entry, got %#v", stats["circuit_breakers"])
	}

	first := breakers[0].(map[string]interface{})
	if state, _ := first["state"].(string); state != "open" {
		t.Fatalf("expected circuit to be open, got %q", state)
	}
	retryIn, ok := first["retry_in"].(float64)
	if !ok {
		t.Fatalf("expected retry_in number in JSON response, got %T", first["retry_in"])
	}
	if retryIn <= 0 {
		t.Fatalf("expected retry_in to be present when circuit is open, got %v", first["retry_in"])
	}
}

func TestRetryAppliesRateLimiterOnEachAttempt(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		limiter:     rate.NewLimiter(rate.Every(120*time.Millisecond), 1),
		searchFn: func(q Query) ([]SearchResult, error) {
			return nil, errors.New("always fail")
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 2
	opts.Resilience.Retry.InitialBackoff = 0
	opts.Resilience.Retry.MaxBackoff = 0
	opts.Resilience.Retry.BackoffFactor = 1
	srv := NewServerWithOptions("127.0.0.1", 7075, opts, engine)

	start := time.Now()
	resp := request(t, srv, "/google/search?text=golang")
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected failure response, got %d", resp.StatusCode)
	}

	if engine.searchCalls != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 retries), got %d", engine.searchCalls)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("expected limiter to delay retries, elapsed only %s", elapsed)
	}
}

// Server-level CORS tests are intentionally smoke-level:
// they verify middleware registration and option wiring, not header semantics.
func TestServerOptions_WiresCustomCORSConfig(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}

	opts := DefaultServerOptions()
	opts.CORS = CORSConfig{
		AllowOrigins: "https://client.local",
		AllowMethods: "GET,OPTIONS",
		AllowHeaders: "Authorization",
		MaxAge:       600,
	}

	srv := NewServerWithOptions("127.0.0.1", 7076, opts, engine)
	resp := request(t, srv, "/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://client.local" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
}

func TestServerOptions_DisableCORSMiddleware(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}

	opts := DefaultServerOptions()
	opts.EnableCORS = false

	srv := NewServerWithOptions("127.0.0.1", 7077, opts, engine)
	resp := request(t, srv, "/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected CORS headers to be absent when disabled, got allow-origin=%q", got)
	}
}

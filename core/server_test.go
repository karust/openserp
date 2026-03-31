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
	imageCalls  int
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
	e.mu.Lock()
	e.imageCalls++
	e.mu.Unlock()
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

func TestDedicatedEndpointCachesSearchResults(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = false
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7073, opts, engine)

	first := request(t, srv, "/google/search?text=golang")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", first.StatusCode)
	}
	if got := first.Header.Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected cache MISS on first request, got %q", got)
	}

	second := request(t, srv, "/google/search?text=golang")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second request to succeed, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected cache HIT on second request, got %q", got)
	}
	if engine.searchCalls != 1 {
		t.Fatalf("expected engine to be called once, got %d", engine.searchCalls)
	}
}

func TestDedicatedEndpointCachesImageResults(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = false
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7074, opts, engine)

	first := request(t, srv, "/google/image?text=golang")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first image request to succeed, got %d", first.StatusCode)
	}
	if got := first.Header.Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected cache MISS on first image request, got %q", got)
	}

	second := request(t, srv, "/google/image?text=golang")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second image request to succeed, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected cache HIT on second image request, got %q", got)
	}
	if engine.imageCalls != 1 {
		t.Fatalf("expected image engine to be called once, got %d", engine.imageCalls)
	}
}

func TestDedicatedEndpointFallbackBypassesCache(t *testing.T) {
	primary := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			return nil, errors.New("primary failed")
		},
	}
	fallback := &engineMock{name: "yandex", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = true
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7078, opts, primary, fallback)

	first := request(t, srv, "/google/search?text=golang")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first fallback request to succeed, got %d", first.StatusCode)
	}
	if got := first.Header.Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("expected bypass on fallback response, got %q", got)
	}
	if got := first.Header.Get("X-Fallback-Engine"); got != "yandex" {
		t.Fatalf("expected fallback engine header, got %q", got)
	}

	second := request(t, srv, "/google/search?text=golang")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected repeated fallback request to succeed, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("expected repeated fallback request to bypass cache, got %q", got)
	}
	if got := second.Header.Get("X-Fallback-Engine"); got != "yandex" {
		t.Fatalf("expected fallback engine header on repeated request, got %q", got)
	}
	if primary.searchCalls != 2 || fallback.searchCalls != 2 {
		t.Fatalf("expected repeated live fallback calls, got primary=%d fallback=%d", primary.searchCalls, fallback.searchCalls)
	}
}

func TestDedicatedEndpointFallbackDoesNotBypassProxyPolicy(t *testing.T) {
	primary := &engineMock{name: "google", initialized: true}
	fallback := &engineMock{name: "yandex", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = true
	opts.Resilience.Retry.MaxRetries = 0
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{},
		},
		EnginePolicies: map[string]string{"google": "missing"},
	}

	srv := NewServerWithOptions("127.0.0.1", 7103, opts, primary, fallback)
	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected fail-closed 503 when primary proxy policy cannot be satisfied, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Fallback-Engine"); got != "" {
		t.Fatalf("unexpected fallback header when proxy policy fails closed: %q", got)
	}
	if fallback.searchCalls != 0 {
		t.Fatalf("fallback engine should not be called when proxy policy fails closed, got %d calls", fallback.searchCalls)
	}
}

func TestMegaSearchCachesWholeQueryWithEngineOrderNormalization(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/shared", Title: "shared"}}, nil
		},
	}
	yandex := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/shared", Title: "shared"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7083, opts, google, yandex)

	first := request(t, srv, "/mega/search?text=golang&engines=yandex,google")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first mega request to succeed, got %d", first.StatusCode)
	}
	if got := first.Header.Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected cache MISS on first mega request, got %q", got)
	}

	second := request(t, srv, "/mega/search?text=golang&engines=google,yandex")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second mega request to succeed, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected cache HIT on reordered mega request, got %q", got)
	}

	if google.searchCalls != 1 || yandex.searchCalls != 1 {
		t.Fatalf("expected one live mega call per engine before cache hit, got google=%d yandex=%d", google.searchCalls, yandex.searchCalls)
	}
}

func TestMegaImageCachesWholeQueryWithEngineOrderNormalization(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		imageFn: func(q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://img.example.com/shared", Title: "shared"}}, nil
		},
	}
	yandex := &engineMock{
		name:        "yandex",
		initialized: true,
		imageFn: func(q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://img.example.com/shared", Title: "shared"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7084, opts, google, yandex)

	first := request(t, srv, "/mega/image?text=golang+logo&engines=yandex,google")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first mega image request to succeed, got %d", first.StatusCode)
	}
	if got := first.Header.Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected cache MISS on first mega image request, got %q", got)
	}

	second := request(t, srv, "/mega/image?text=golang+logo&engines=google,yandex")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second mega image request to succeed, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected cache HIT on reordered mega image request, got %q", got)
	}

	if google.imageCalls != 1 || yandex.imageCalls != 1 {
		t.Fatalf("expected one live mega image call per engine before cache hit, got google=%d yandex=%d", google.imageCalls, yandex.imageCalls)
	}
}

func TestMegaSearchDeduplicatesRepeatedEngineNames(t *testing.T) {
	google := &engineMock{name: "google", initialized: true}
	yandex := &engineMock{name: "yandex", initialized: true}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7085, opts, google, yandex)

	resp := request(t, srv, "/mega/search?text=golang&engines=google,google,yandex,google")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected mega request to succeed, got %d", resp.StatusCode)
	}
	if google.searchCalls != 1 || yandex.searchCalls != 1 {
		t.Fatalf("expected deduplicated engine execution, got google=%d yandex=%d", google.searchCalls, yandex.searchCalls)
	}
}

func TestMegaSearchCachesForHealthySubsetWhenOneCircuitIsOpen(t *testing.T) {
	google := &engineMock{name: "google", initialized: true}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			return nil, errors.New("bing failed")
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.Resilience.CircuitBreaker.FailureThreshold = 1
	opts.Resilience.CircuitBreaker.RecoveryTimeout = 5 * time.Minute
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7086, opts, google, bing)

	first := request(t, srv, "/mega/search?text=golang&engines=google,bing")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first mega request to succeed with partial results, got %d", first.StatusCode)
	}
	if got := first.Header.Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected first response to populate cache, got %q", got)
	}

	second := request(t, srv, "/mega/search?text=golang&engines=google,bing")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second mega request to succeed, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected second response to hit subset cache, got %q", got)
	}

	if google.searchCalls != 1 || bing.searchCalls != 1 {
		t.Fatalf("expected subset cache hit to avoid new engine calls, got google=%d bing=%d", google.searchCalls, bing.searchCalls)
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
	srv := NewServerWithOptions("127.0.0.1", 7075, opts, primary)

	_ = request(t, srv, "/google/search?text=golang")
	statsResp := request(t, srv, "/stats/cb")
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

func TestStatsEndpointsContract(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	srv := NewServerWithOptions("127.0.0.1", 7090, opts, engine)

	for _, path := range []string{"/stats", "/stats/cache", "/stats/proxy", "/stats/cb"} {
		resp := request(t, srv, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected %s to return 200, got %d", path, resp.StatusCode)
		}
	}

	for _, oldPath := range []string{"/cache/stats", "/resilience/stats"} {
		resp := request(t, srv, oldPath)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d", oldPath, resp.StatusCode)
		}
	}
}

func TestStatsProxyV2Payload(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{
				{URL: "http://user:pass@proxy1:8080", Tags: []string{"default", "us"}},
				{URL: "http://proxy2:8080", Tags: []string{"default"}},
			},
			Health: ProxiesHealthConfig{FailureThreshold: 1},
		},
		EnginePolicies: map[string]string{"google": "default"},
	}

	srv := NewServerWithOptions("127.0.0.1", 7092, opts, engine)
	resp := request(t, srv, "/stats/proxy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /stats/proxy to return 200, got %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /stats/proxy response: %v", err)
	}

	if got := payload["configured_count"].(float64); got != 2 {
		t.Fatalf("expected configured_count=2, got %v", got)
	}
	if got := payload["healthy_count"].(float64); got != 2 {
		t.Fatalf("expected healthy_count=2, got %v", got)
	}
	if got := payload["unhealthy_count"].(float64); got != 0 {
		t.Fatalf("expected unhealthy_count=0, got %v", got)
	}

	if _, exists := payload["defaults"]; exists {
		t.Fatalf("defaults must not be exposed in proxy stats payload")
	}

	entries := payload["entries"].([]interface{})
	first := entries[0].(map[string]interface{})
	if got := first["proxy"].(string); got == "http://user:pass@proxy1:8080" {
		t.Fatalf("expected masked proxy, got %q", got)
	}
	if _, exists := payload["runtime"]; exists {
		t.Fatalf("runtime must not be exposed in V2 stats payload")
	}
	if _, exists := payload["source"]; exists {
		t.Fatalf("source must not be exposed in V2 stats payload")
	}
	engines := payload["engines"].(map[string]interface{})
	google := engines["google"].(map[string]interface{})
	if _, exists := google["mode"]; exists {
		t.Fatalf("engine mode must not be exposed in proxy stats payload")
	}
	if got := google["tag"]; got != "default" {
		t.Fatalf("expected engine tag default, got %#v", got)
	}
	if got := google["selected_proxy"]; got != "pooled" {
		t.Fatalf("expected pooled engine proxy stats, got %#v", got)
	}
}

func TestResilientRawProxyPoolRotatesOnRetry(t *testing.T) {
	var attemptedProxies []string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			attemptedProxies = append(attemptedProxies, q.ProxyURL)
			if q.ProxyURL == "http://bad-proxy:8080" {
				return nil, errors.New("proxy failed")
			}
			return []SearchResult{{Rank: 1, URL: "https://example.com/success", Title: "ok"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 1
	opts.Resilience.Retry.InitialBackoff = 0
	opts.Resilience.Retry.MaxBackoff = 0
	opts.Resilience.Retry.BackoffFactor = 1
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{
				{URL: "http://bad-proxy:8080", Tags: []string{"default"}},
				{URL: "http://good-proxy:8080", Tags: []string{"default"}},
			},
			Health: ProxiesHealthConfig{FailureThreshold: 1},
		},
		EnginePolicies: map[string]string{"google": "default"},
	}

	srv := NewServerWithOptions("127.0.0.1", 7091, opts, engine)
	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected search to recover through rotated proxy, got %d", resp.StatusCode)
	}
	if len(attemptedProxies) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attemptedProxies))
	}
	if attemptedProxies[0] != "http://bad-proxy:8080" || attemptedProxies[1] != "http://good-proxy:8080" {
		t.Fatalf("unexpected proxy rotation order: %#v", attemptedProxies)
	}

	statsResp := request(t, srv, "/stats/proxy")
	var stats map[string]interface{}
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}
	if got := stats["healthy_count"].(float64); got != 1 {
		t.Fatalf("expected healthy_count=1, got %v", got)
	}
	if got := stats["unhealthy_count"].(float64); got != 1 {
		t.Fatalf("expected unhealthy_count=1, got %v", got)
	}
}

func TestProxyHeadersDirectAndTagPool(t *testing.T) {
	directEngine := &engineMock{name: "google", initialized: true}
	directSrv := NewServerWithOptions("127.0.0.1", 7093, DefaultServerOptions(), directEngine)

	directResp := request(t, directSrv, "/google/search?text=golang")
	if directResp.StatusCode != http.StatusOK {
		t.Fatalf("expected direct request to succeed, got %d", directResp.StatusCode)
	}
	if got := directResp.Header.Get("X-Proxy-Mode"); got != ProxyModeOff {
		t.Fatalf("expected X-Proxy-Mode=%s, got %q", ProxyModeOff, got)
	}
	if got := directResp.Header.Get("X-Proxy-Tag"); got != "" {
		t.Fatalf("expected empty X-Proxy-Tag in off mode, got %q", got)
	}
	if got := directResp.Header.Get("X-Proxy-Used"); got != "direct" {
		t.Fatalf("expected X-Proxy-Used=direct, got %q", got)
	}

	proxiedEngine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{
				{URL: "http://proxy1:8080", Tags: []string{"default"}},
			},
		},
		EnginePolicies: map[string]string{"google": "default"},
	}
	proxiedSrv := NewServerWithOptions("127.0.0.1", 7094, opts, proxiedEngine)
	proxiedResp := request(t, proxiedSrv, "/google/search?text=golang")
	if proxiedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected proxied request to succeed, got %d", proxiedResp.StatusCode)
	}
	if got := proxiedResp.Header.Get("X-Proxy-Mode"); got != ProxyModeTagPool {
		t.Fatalf("expected X-Proxy-Mode=%s, got %q", ProxyModeTagPool, got)
	}
	if got := proxiedResp.Header.Get("X-Proxy-Tag"); got != "default" {
		t.Fatalf("expected X-Proxy-Tag=default, got %q", got)
	}
	if got := proxiedResp.Header.Get("X-Proxy-Used"); got != "http://proxy1:8080" {
		t.Fatalf("expected masked selected proxy, got %q", got)
	}
}

func TestGlobalProxyForcesAllEnginesRaw(t *testing.T) {
	var googleProxy string
	var yandexProxy string

	googleEngine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	yandexEngine := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			yandexProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/yandex", Title: "yandex"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Global: "http://global-proxy:8080",
		},
		EnginePolicies: map[string]string{
			"google": "us",
		},
	}

	srv := NewServerWithOptions("127.0.0.1", 7097, opts, googleEngine, yandexEngine)
	if resp := request(t, srv, "/google/search?text=golang"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected google request to succeed, got %d", resp.StatusCode)
	}
	if resp := request(t, srv, "/yandex/search?text=golang"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected yandex request to succeed, got %d", resp.StatusCode)
	}

	if googleProxy != "http://global-proxy:8080" {
		t.Fatalf("expected google to use global proxy, got %q", googleProxy)
	}
	if yandexProxy != "http://global-proxy:8080" {
		t.Fatalf("expected yandex to use global proxy, got %q", yandexProxy)
	}
}

func TestBrowserProxyPoolRotatesPerRequest(t *testing.T) {
	var attemptedProxies []string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			attemptedProxies = append(attemptedProxies, q.ProxyURL)
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeBrowser,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{
				{URL: "http://proxy1:8080", Tags: []string{"default"}},
				{URL: "http://proxy2:8080", Tags: []string{"default"}},
			},
		},
		EnginePolicies: map[string]string{"google": "default"},
	}

	srv := NewServerWithOptions("127.0.0.1", 7098, opts, engine)
	if resp := request(t, srv, "/google/search?text=golang"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first browser-style request to succeed, got %d", resp.StatusCode)
	}
	if resp := request(t, srv, "/google/search?text=golang+2"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected second browser-style request to succeed, got %d", resp.StatusCode)
	}

	if len(attemptedProxies) != 2 {
		t.Fatalf("expected 2 browser-style proxy attempts, got %d", len(attemptedProxies))
	}
	if attemptedProxies[0] != "http://proxy1:8080" || attemptedProxies[1] != "http://proxy2:8080" {
		t.Fatalf("expected browser proxy rotation order, got %#v", attemptedProxies)
	}
}

func TestProxyFailClosedWhenNoHealthyProxy(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{},
		},
		EnginePolicies: map[string]string{"google": "missing"},
	}

	srv := NewServerWithOptions("127.0.0.1", 7095, opts, engine)
	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected fail-closed 503 when no healthy proxy exists, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Proxy-Mode"); got != ProxyModeTagPool {
		t.Fatalf("expected X-Proxy-Mode=%s on fail-closed response, got %q", ProxyModeTagPool, got)
	}
}

func TestEngineOverrideProxyBehaviorRaw(t *testing.T) {
	var googleProxy string
	var yandexProxy string

	googleEngine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	yandexEngine := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			yandexProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/yandex", Title: "yandex"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Entries: []ProxyEntryConfig{
				{URL: "http://proxy-us:8080", Tags: []string{"us"}},
			},
		},
		EnginePolicies: map[string]string{"google": "us"},
	}

	srv := NewServerWithOptions("127.0.0.1", 7096, opts, googleEngine, yandexEngine)
	if resp := request(t, srv, "/google/search?text=golang"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected google request to succeed, got %d", resp.StatusCode)
	}
	if resp := request(t, srv, "/yandex/search?text=golang"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected yandex request to succeed, got %d", resp.StatusCode)
	}

	if googleProxy == "" {
		t.Fatalf("expected proxied google request, got empty proxy")
	}
	if yandexProxy != "" {
		t.Fatalf("expected direct yandex request, got proxy %q", yandexProxy)
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
	srv := NewServerWithOptions("127.0.0.1", 7076, opts, engine)

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

func TestCacheStatsDisabled(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}

	opts := DefaultServerOptions()
	opts.CacheTTL = 0
	opts.CacheMaxSize = 0
	srv := NewServerWithOptions("127.0.0.1", 7079, opts, engine)

	resp := request(t, srv, "/stats/cache")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected disabled cache stats to return 200, got %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode cache stats: %v", err)
	}
	if payload["status"] != false {
		t.Fatalf("expected disabled status, got %#v", payload)
	}
}

func TestCacheStatsReflectActivity(t *testing.T) {
	primary := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(q Query) ([]SearchResult, error) {
			if q.Text == "fallback" {
				return nil, errors.New("fallback path")
			}
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	fallback := &engineMock{name: "yandex", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = true
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	srv := NewServerWithOptions("127.0.0.1", 7080, opts, primary, fallback)

	_ = request(t, srv, "/google/search?text=golang")
	_ = request(t, srv, "/google/search?text=golang")
	_ = request(t, srv, "/google/search?text=fallback")

	resp := request(t, srv, "/stats/cache")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected cache stats endpoint to return 200, got %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode cache stats response: %v", err)
	}
	if got := payload["status"].(bool); got != true {
		t.Fatalf("expected enabled status, got %v", got)
	}
	if got := payload["entries"].(float64); got != 1 {
		t.Fatalf("expected 1 cache entry, got %v", got)
	}
	if got := payload["hits"].(float64); got != 1 {
		t.Fatalf("expected 1 cache hit, got %v", got)
	}
	if got := payload["misses"].(float64); got != 2 {
		t.Fatalf("expected 2 cache misses, got %v", got)
	}
	if got := payload["bypasses"].(float64); got != 1 {
		t.Fatalf("expected 1 bypass, got %v", got)
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

	srv := NewServerWithOptions("127.0.0.1", 7081, opts, engine)
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

	srv := NewServerWithOptions("127.0.0.1", 7082, opts, engine)
	resp := request(t, srv, "/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected CORS headers to be absent when disabled, got allow-origin=%q", got)
	}
}

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

type engineMock struct {
	name        string
	initialized bool
	limiter     *rate.Limiter
	searchFn    func(context.Context, Query) ([]SearchResult, error)
	imageFn     func(context.Context, Query) ([]SearchResult, error)

	mu                 sync.Mutex
	searchCalls        int
	imageCalls         int
	droppedLaneQueries []Query
}

func (e *engineMock) Name() string { return e.name }
func (e *engineMock) IsInitialized() bool {
	return e.initialized
}
func (e *engineMock) GetRateLimiter() *rate.Limiter { return e.limiter }

func (e *engineMock) Search(ctx context.Context, q Query) ([]SearchResult, error) {
	e.mu.Lock()
	e.searchCalls++
	e.mu.Unlock()
	if e.searchFn != nil {
		return e.searchFn(ctx, q)
	}
	return []SearchResult{{Rank: 1, URL: "https://example.com/" + e.name, Title: e.name}}, nil
}

func (e *engineMock) SearchImage(ctx context.Context, q Query) ([]SearchResult, error) {
	e.mu.Lock()
	e.imageCalls++
	e.mu.Unlock()
	if e.imageFn != nil {
		return e.imageFn(ctx, q)
	}
	return []SearchResult{{Rank: 1, URL: "https://img.example.com/" + e.name, Title: e.name}}, nil
}

func (e *engineMock) DropProxyLaneCookies(_ context.Context, q Query) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.droppedLaneQueries = append(e.droppedLaneQueries, q)
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

func requestWithHeader(t *testing.T, s *Server, path string, header string, value string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(header, value)
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed for %s: %v", path, err)
	}
	return resp
}

func TestRequestIDHeaderIsEchoedWhenProvided(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7110, DefaultServerOptions(), engine)

	resp := requestWithHeader(t, srv, "/google/search?text=golang", "X-Request-ID", "foo")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected request to succeed, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-ID"); got != "foo" {
		t.Fatalf("expected X-Request-ID=foo, got %q", got)
	}
}

func TestRequestIDHeaderIsGeneratedWhenMissing(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7111, DefaultServerOptions(), engine)

	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected request to succeed, got %d", resp.StatusCode)
	}
	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("expected non-empty X-Request-ID header")
	}
	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("expected X-Request-ID to be a UUID, got %q (%v)", requestID, err)
	}
}

func TestErrorResponseIncludesRequestID(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7112, DefaultServerOptions(), engine)

	resp := requestWithHeader(t, srv, "/google/search?text=", "X-Request-ID", "req-error")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var payload JSONErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.RequestID != "req-error" {
		t.Fatalf("expected request_id=req-error, got %q", payload.RequestID)
	}
}

func TestOpenAPISpecEndpoint(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7107, DefaultServerOptions(), engine)

	resp := request(t, srv, "/openapi.yaml")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /openapi.yaml to return 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/yaml") {
		t.Fatalf("expected YAML content-type, got %q", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /openapi.yaml body: %v", err)
	}
	if !strings.Contains(string(body), "openapi: 3.0.3") {
		t.Fatalf("expected OpenAPI version marker in body")
	}
}

func TestDocsEndpointServesSwaggerUI(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7108, DefaultServerOptions(), engine)

	resp := request(t, srv, "/docs")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /docs to return 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected HTML content-type, got %q", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /docs body: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, "SwaggerUIBundle") {
		t.Fatalf("expected SwaggerUI bundle script on docs page")
	}
	if !strings.Contains(content, "/openapi.yaml") {
		t.Fatalf("expected docs page to reference /openapi.yaml")
	}
}

func TestDebugFingerprintEndpointDisabledByDefault(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7109, DefaultServerOptions(), engine)

	resp := request(t, srv, "/debug/fingerprint-check")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected disabled debug endpoint to return 404, got %d", resp.StatusCode)
	}
}

func TestDebugFingerprintEndpointValidatesDetectorParam(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.EnableDebugEndpoints = true
	srv := NewServerWithOptions("127.0.0.1", 7112, opts, engine)

	resp := request(t, srv, "/debug/fingerprint-check?detector=unknown")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid detector to return 400, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Network-Bytes"); got != "0" {
		t.Fatalf("expected X-Network-Bytes=0, got %q", got)
	}
}

func TestDebugFingerprintEndpointValidatesWaitParam(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.EnableDebugEndpoints = true
	srv := NewServerWithOptions("127.0.0.1", 7113, opts, engine)

	resp := request(t, srv, "/debug/fingerprint-check?detector=sannysoft&wait_ms=-1")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid wait_ms to return 400, got %d", resp.StatusCode)
	}
}

func TestDebugFingerprintEndpointCustomDetectorRequiresURL(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.EnableDebugEndpoints = true
	srv := NewServerWithOptions("127.0.0.1", 7114, opts, engine)

	resp := request(t, srv, "/debug/fingerprint-check?detector=custom")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected custom detector without url to return 400, got %d", resp.StatusCode)
	}
}

func TestDebugFingerprintEndpointCustomDetectorValidatesInsecureParam(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.EnableDebugEndpoints = true
	srv := NewServerWithOptions("127.0.0.1", 7115, opts, engine)

	resp := request(t, srv, "/debug/fingerprint-check?detector=custom&url=https://localhost:9000&insecure=notabool")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid insecure query value to return 400, got %d", resp.StatusCode)
	}
}

func TestDebugFingerprintEndpointCustomDetectorValidatesURL(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.EnableDebugEndpoints = true
	srv := NewServerWithOptions("127.0.0.1", 7116, opts, engine)

	resp := request(t, srv, "/debug/fingerprint-check?detector=custom&url=not-a-url")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid custom URL to return 400, got %d", resp.StatusCode)
	}
}

func TestInvalidQueryParametersReturnJSONError(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7104, DefaultServerOptions(), engine)

	tests := []struct {
		name    string
		path    string
		message string
		reason  string
	}{
		{
			name:    "invalid limit",
			path:    "/google/search?text=golang&limit=abc",
			message: "limit must be an integer",
			reason:  ReasonInvalidLimit,
		},
		{
			name:    "negative start",
			path:    "/google/search?text=golang&start=-1",
			message: "start must be >= 0",
			reason:  ReasonInvalidStart,
		},
		{
			name:    "invalid filter flag",
			path:    "/google/search?text=golang&filter=notabool",
			message: "invalid syntax",
			reason:  ReasonInvalidParam,
		},
		{
			name:    "invalid answers flag on mega endpoint",
			path:    "/mega/search?text=golang&answers=notabool",
			message: "invalid syntax",
			reason:  ReasonInvalidParam,
		},
		{
			name:    "empty text query",
			path:    "/google/search?text=",
			message: "query cannot be empty",
			reason:  ReasonEmptyQuery,
		},
		{
			name:    "limit too high",
			path:    "/google/search?text=golang&limit=999",
			message: "limit must be between 1 and 100",
			reason:  ReasonInvalidLimit,
		},
		{
			name:    "zero limit",
			path:    "/google/search?text=golang&limit=0",
			message: "limit must be between 1 and 100",
			reason:  ReasonInvalidLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := request(t, srv, tt.path)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid query params, got %d", resp.StatusCode)
			}

			var payload JSONErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if payload.Code != http.StatusBadRequest {
				t.Fatalf("expected code=400, got %d", payload.Code)
			}
			if payload.Error != "bad_request" {
				t.Fatalf("expected error=bad_request, got %q", payload.Error)
			}
			if payload.Message == "" {
				t.Fatal("expected error message to be present")
			}
			if tt.message != "" && !strings.Contains(strings.ToLower(payload.Message), strings.ToLower(tt.message)) {
				t.Fatalf("expected message to contain %q, got %q", tt.message, payload.Message)
			}
			if tt.reason != "" && payload.Reason != tt.reason {
				t.Fatalf("expected reason=%q, got %q", tt.reason, payload.Reason)
			}
		})
	}
}

func TestMegaEnginesEndpointResponseFormat(t *testing.T) {
	google := &engineMock{name: "google", initialized: true}
	yandex := &engineMock{name: "yandex", initialized: false}
	srv := NewServerWithOptions("127.0.0.1", 7105, DefaultServerOptions(), google, yandex)

	// Prime circuit breaker stats so circuit_state is populated for both engines.
	_ = request(t, srv, "/google/search?text=golang")
	_ = request(t, srv, "/yandex/search?text=golang")

	resp := request(t, srv, "/mega/engines")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /mega/engines to return 200, got %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /mega/engines response: %v", err)
	}

	total, ok := payload["total"].(float64)
	if !ok {
		t.Fatalf("expected numeric total field, got %T", payload["total"])
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %v", total)
	}

	engines, ok := payload["engines"].([]interface{})
	if !ok {
		t.Fatalf("expected engines array, got %T", payload["engines"])
	}
	if len(engines) != 2 {
		t.Fatalf("expected 2 engines in payload, got %d", len(engines))
	}

	byName := map[string]map[string]interface{}{}
	for _, entry := range engines {
		engineData, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("expected engine object, got %T", entry)
		}
		name, _ := engineData["name"].(string)
		if name == "" {
			t.Fatalf("expected non-empty engine name, got %#v", engineData["name"])
		}
		if _, ok := engineData["initialized"].(bool); !ok {
			t.Fatalf("expected initialized bool for engine %s, got %T", name, engineData["initialized"])
		}
		state, ok := engineData["circuit_state"].(string)
		if !ok || state == "" {
			t.Fatalf("expected non-empty circuit_state for engine %s, got %#v", name, engineData["circuit_state"])
		}
		byName[name] = engineData
	}

	if _, ok := byName["google"]; !ok {
		t.Fatal("expected google engine in response")
	}
	if _, ok := byName["yandex"]; !ok {
		t.Fatal("expected yandex engine in response")
	}
}

func TestStatsEndpointStructure(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7106, DefaultServerOptions(), engine)

	// Ensure circuit breaker stats are initialized.
	_ = request(t, srv, "/google/search?text=golang")

	resp := request(t, srv, "/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /stats to return 200, got %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /stats response: %v", err)
	}

	cache, ok := payload["cache"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected cache object, got %T", payload["cache"])
	}
	if _, ok := cache["status"].(bool); !ok {
		t.Fatalf("expected cache.status bool, got %T", cache["status"])
	}
	if _, ok := cache["entries"].(float64); !ok {
		t.Fatalf("expected cache.entries number, got %T", cache["entries"])
	}
	if _, ok := cache["hits"].(float64); !ok {
		t.Fatalf("expected cache.hits number, got %T", cache["hits"])
	}
	if _, ok := cache["misses"].(float64); !ok {
		t.Fatalf("expected cache.misses number, got %T", cache["misses"])
	}
	if _, ok := cache["bypasses"].(float64); !ok {
		t.Fatalf("expected cache.bypasses number, got %T", cache["bypasses"])
	}

	proxy, ok := payload["proxy"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected proxy object, got %T", payload["proxy"])
	}
	if _, ok := proxy["configured_count"].(float64); !ok {
		t.Fatalf("expected proxy.configured_count number, got %T", proxy["configured_count"])
	}
	if _, ok := proxy["healthy_count"].(float64); !ok {
		t.Fatalf("expected proxy.healthy_count number, got %T", proxy["healthy_count"])
	}
	if _, ok := proxy["unhealthy_count"].(float64); !ok {
		t.Fatalf("expected proxy.unhealthy_count number, got %T", proxy["unhealthy_count"])
	}
	if _, ok := proxy["tags"].(map[string]interface{}); !ok {
		t.Fatalf("expected proxy.tags object, got %T", proxy["tags"])
	}
	if _, ok := proxy["entries"].([]interface{}); !ok {
		t.Fatalf("expected proxy.entries array, got %T", proxy["entries"])
	}

	engines, ok := proxy["engines"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected proxy.engines object, got %T", proxy["engines"])
	}
	googleStats, ok := engines["google"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected proxy.engines.google object, got %T", engines["google"])
	}
	if got := googleStats["selected_proxy"]; got != "direct" {
		t.Fatalf("expected proxy.engines.google.selected_proxy=direct, got %#v", got)
	}

	breakers, ok := payload["circuit_breakers"].([]interface{})
	if !ok {
		t.Fatalf("expected circuit_breakers array, got %T", payload["circuit_breakers"])
	}
	if len(breakers) == 0 {
		t.Fatal("expected at least one circuit breaker entry")
	}
	first, ok := breakers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected circuit breaker object, got %T", breakers[0])
	}
	if _, ok := first["engine"].(string); !ok {
		t.Fatalf("expected circuit_breakers[0].engine string, got %T", first["engine"])
	}
	if _, ok := first["state"].(string); !ok {
		t.Fatalf("expected circuit_breakers[0].state string, got %T", first["state"])
	}
	if _, ok := first["failure_count"].(float64); !ok {
		t.Fatalf("expected circuit_breakers[0].failure_count number, got %T", first["failure_count"])
	}

	captchaSolver, ok := payload["captcha"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected captcha object, got %T", payload["captcha"])
	}
	if _, ok := captchaSolver["solver_attempts"].(float64); !ok {
		t.Fatalf("expected captcha.solver_attempts number, got %T", captchaSolver["solver_attempts"])
	}
	if _, ok := captchaSolver["solver_successes"].(float64); !ok {
		t.Fatalf("expected captcha.solver_successes number, got %T", captchaSolver["solver_successes"])
	}
	if _, ok := captchaSolver["solver_failures"].(float64); !ok {
		t.Fatalf("expected captcha.solver_failures number, got %T", captchaSolver["solver_failures"])
	}
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

func TestReadinessEndpointStatusSemantics(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7077, DefaultServerOptions(), engine)

	resp := request(t, srv, "/ready")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected ready endpoint to return 200 while serving, got %d", resp.StatusCode)
	}

	var ready ReadinessStatus
	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		t.Fatalf("decode ready response: %v", err)
	}
	if ready.Status != "ready" {
		t.Fatalf("expected readiness status=ready, got %q", ready.Status)
	}

	srv.SetDraining(true)
	resp = request(t, srv, "/ready")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected ready endpoint to return 503 while draining, got %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		t.Fatalf("decode draining response: %v", err)
	}
	if ready.Status != "draining" {
		t.Fatalf("expected readiness status=draining, got %q", ready.Status)
	}
}

func TestDedicatedEndpointNoFallbackByDefault(t *testing.T) {
	primary := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return nil, errors.New("primary failed")
		},
	}
	fallback := &engineMock{name: "yandex", initialized: true}

	opts := DefaultServerOptions()
	opts.AllowEndpointFallback = false
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7072, opts, primary, fallback)

	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 when primary fails and fallback disabled, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Fallback-Engine"); got != "" {
		t.Fatalf("unexpected fallback header: %s", got)
	}
	if fallback.searchCalls != 0 {
		t.Fatalf("fallback engine should not be called, got %d calls", fallback.searchCalls)
	}
}

func TestDedicatedEndpointReturnsNetworkBytesHeader(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(ctx context.Context, q Query) ([]SearchResult, error) {
			AddNetworkBytes(ctx, 123)
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7211, opts, engine)

	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Network-Bytes"); got != "123" {
		t.Fatalf("expected X-Network-Bytes=123, got %q", got)
	}
}

func TestDedicatedEndpointReturnsBrowserProfileHeader(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(ctx context.Context, q Query) ([]SearchResult, error) {
			SetBrowserProfileID(ctx, "chrome-win-us")
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7212, opts, engine)

	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(browserProfileIDHeader); got != "chrome-win-us" {
		t.Fatalf("expected %s=chrome-win-us, got %q", browserProfileIDHeader, got)
	}
}

func TestMegaEndpointReturnsBrowserProfileHeader(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(ctx context.Context, q Query) ([]SearchResult, error) {
			SetBrowserProfileID(ctx, "chrome-win-us")
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(ctx context.Context, q Query) ([]SearchResult, error) {
			SetBrowserProfileID(ctx, "chrome-linux-us")
			return []SearchResult{{Rank: 1, URL: "https://example.com/bing", Title: "bing"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7213, opts, google, bing)

	resp := request(t, srv, "/mega/search?text=golang&engines=google,bing")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	got := strings.Split(resp.Header.Get(browserProfileIDHeader), ",")
	if len(got) != 2 || !slices.Contains(got, "chrome-win-us") || !slices.Contains(got, "chrome-linux-us") {
		t.Fatalf("expected both profile IDs, got %q", got)
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

func TestProxiedRequestWithoutMarketMetadataBypassesCache(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7124, opts, engine)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
		req.Header.Set("X-Proxy-URL", "http://proxy.example:8080")
		resp, err := srv.app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request to succeed, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("X-Cache"); got != "BYPASS" {
			t.Fatalf("expected X-Cache=BYPASS, got %q", got)
		}
	}
	if engine.searchCalls != 2 {
		t.Fatalf("expected cache bypass to execute both searches, got %d calls", engine.searchCalls)
	}
}

func TestProxiedRequestWithMarketMetadataUsesCache(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = time.Minute
	opts.CacheMaxSize = 10
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7125, opts, engine)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
		req.Header.Set("X-Proxy-URL", "http://proxy.example:8080")
		req.Header.Set("X-Proxy-Country", "us")
		resp, err := srv.app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request to succeed, got %d", resp.StatusCode)
		}
	}
	if engine.searchCalls != 1 {
		t.Fatalf("expected second market-scoped request to hit cache, got %d calls", engine.searchCalls)
	}
}

func TestDedicatedEndpointFallbackBypassesCache(t *testing.T) {
	primary := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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
	var env Envelope
	if err := json.NewDecoder(first.Body).Decode(&env); err != nil {
		t.Fatalf("decode fallback envelope: %v", err)
	}
	if len(env.Meta.EnginesFailed) != 1 || env.Meta.EnginesFailed[0] != "google" {
		t.Fatalf("expected primary engine in engines_failed, got %v", env.Meta.EnginesFailed)
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/shared", Title: "shared"}}, nil
		},
	}
	yandex := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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
		imageFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://img.example.com/shared", Title: "shared"}}, nil
		},
	}
	yandex := &engineMock{
		name:        "yandex",
		initialized: true,
		imageFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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

func TestMegaSearchInvalidModeReturnsBadRequest(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7181, opts, engine)

	resp := request(t, srv, "/mega/search?text=golang&mode=turbo")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid mode, got %d", resp.StatusCode)
	}

	var payload JSONErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Reason != ReasonInvalidParam {
		t.Fatalf("expected reason=%q, got %q", ReasonInvalidParam, payload.Reason)
	}
}

func TestMegaSearchAnyModeStopsAfterFirstSuccessInRequestedOrder(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return nil, errors.New("google failed")
		},
	}
	yandex := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/yandex", Title: "yandex"}}, nil
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/bing", Title: "bing"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = 0
	srv := NewServerWithOptions("127.0.0.1", 7182, opts, google, yandex, bing)

	resp := request(t, srv, "/mega/search?text=golang&mode=any&engines=google,yandex,bing")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Results) == 0 || env.Results[0].Engine != "yandex" {
		t.Fatalf("expected yandex result in any mode, got %#v", env.Results)
	}

	if google.searchCalls != 1 || yandex.searchCalls != 1 || bing.searchCalls != 0 {
		t.Fatalf("expected google=1 yandex=1 bing=0 calls, got google=%d yandex=%d bing=%d", google.searchCalls, yandex.searchCalls, bing.searchCalls)
	}
}

func TestMegaSearchFastModeUsesFastestEngineFromCircuitStats(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			time.Sleep(25 * time.Millisecond)
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	yandex := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			time.Sleep(1 * time.Millisecond)
			return []SearchResult{{Rank: 1, URL: "https://example.com/yandex", Title: "yandex"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = 0
	srv := NewServerWithOptions("127.0.0.1", 7183, opts, google, yandex)

	_ = request(t, srv, "/google/search?text=warm-google")
	_ = request(t, srv, "/yandex/search?text=warm-yandex")

	google.mu.Lock()
	beforeGoogle := google.searchCalls
	google.mu.Unlock()
	yandex.mu.Lock()
	beforeYandex := yandex.searchCalls
	yandex.mu.Unlock()

	resp := request(t, srv, "/mega/search?text=golang&mode=fast&engines=google,yandex")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Results) == 0 || env.Results[0].Engine != "yandex" {
		t.Fatalf("expected fast mode to use yandex, got %#v", env.Results)
	}

	google.mu.Lock()
	afterGoogle := google.searchCalls
	google.mu.Unlock()
	yandex.mu.Lock()
	afterYandex := yandex.searchCalls
	yandex.mu.Unlock()

	if afterGoogle-beforeGoogle != 0 || afterYandex-beforeYandex != 1 {
		t.Fatalf("expected delta calls google=0 yandex=1, got google=%d yandex=%d", afterGoogle-beforeGoogle, afterYandex-beforeYandex)
	}
}

func TestMegaSearchBalancedModeMergeAndDedupeFlags(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/shared", Title: "google"}}, nil
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 2, URL: "https://example.com/shared", Title: "bing"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = 0
	srv := NewServerWithOptions("127.0.0.1", 7184, opts, google, bing)

	respNoDedupe := request(t, srv, "/mega/search?text=golang&mode=balanced&dedupe=false&engines=google,bing")
	if respNoDedupe.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for dedupe=false, got %d", respNoDedupe.StatusCode)
	}
	var envNoDedupe Envelope
	if err := json.NewDecoder(respNoDedupe.Body).Decode(&envNoDedupe); err != nil {
		t.Fatalf("decode dedupe=false envelope: %v", err)
	}
	if len(envNoDedupe.Results) != 2 {
		t.Fatalf("expected 2 results with dedupe=false, got %d", len(envNoDedupe.Results))
	}

	respNoMerge := request(t, srv, "/mega/search?text=golang&mode=balanced&merge=false&engines=google,bing")
	if respNoMerge.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for merge=false, got %d", respNoMerge.StatusCode)
	}
	var envNoMerge Envelope
	if err := json.NewDecoder(respNoMerge.Body).Decode(&envNoMerge); err != nil {
		t.Fatalf("decode merge=false envelope: %v", err)
	}
	if len(envNoMerge.Results) != 1 || envNoMerge.Results[0].Engine != "google" {
		t.Fatalf("expected merge=false to keep first requested engine results, got %#v", envNoMerge.Results)
	}
}

func TestMegaSearchCachesForHealthySubsetWhenOneCircuitIsOpen(t *testing.T) {
	google := &engineMock{name: "google", initialized: true}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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
			AllowRequestProxyURL: true,
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
	if got := payload["request_proxy_url_enabled"].(bool); !got {
		t.Fatalf("expected request_proxy_url_enabled=true")
	}
	lanes, ok := payload["lanes"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected lanes object, got %T", payload["lanes"])
	}
	for _, field := range []string{"active", "evicted_lru", "cookies_dropped"} {
		if _, ok := lanes[field].(float64); !ok {
			t.Fatalf("expected lanes.%s number, got %T", field, lanes[field])
		}
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			attemptedProxies = append(attemptedProxies, q.ProxyURL)
			if q.ProxyURL == "http://bad-proxy:8080" {
				return nil, fmt.Errorf("%w: dial tcp bad-proxy:8080: connection refused", ErrProxyConnect)
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	yandexEngine := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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

func TestRequestProxyOverrideDirectBeatsGlobal(t *testing.T) {
	var googleProxy string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Global: "http://global-proxy:8080",
		},
	}

	srv := NewServerWithOptions("127.0.0.1", 7099, opts, engine)
	resp := requestWithHeader(t, srv, "/google/search?text=golang", "X-Use-Proxy", "direct")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected direct override request to succeed, got %d", resp.StatusCode)
	}
	if googleProxy != "" {
		t.Fatalf("expected direct override to disable proxy, got %q", googleProxy)
	}
	if got := resp.Header.Get("X-Proxy-Mode"); got != ProxyModeOff {
		t.Fatalf("expected X-Proxy-Mode=%s, got %q", ProxyModeOff, got)
	}
	if got := resp.Header.Get("X-Proxy-Used"); got != "direct" {
		t.Fatalf("expected X-Proxy-Used=direct, got %q", got)
	}
}

func TestRequestProxyURLDisabledByDefault(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7117, DefaultServerOptions(), engine)

	resp := requestWithHeader(t, srv, "/google/search?text=golang", "X-Proxy-URL", "http://user:pass@proxy.example:8080")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected disabled request proxy URL to return 400, got %d", resp.StatusCode)
	}

	var payload JSONErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != "bad_request" {
		t.Fatalf("expected bad_request error, got %q", payload.Error)
	}
	if payload.Reason != ReasonRequestProxyURLDisabled {
		t.Fatalf("expected reason=%q, got %q", ReasonRequestProxyURLDisabled, payload.Reason)
	}
}

func TestRequestProxyURLHonoredWhenEnabled(t *testing.T) {
	var got Query
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			got = q
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7118, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
	req.Header.Set("X-Proxy-URL", "http://user:pass@proxy.example:8080")
	req.Header.Set("X-Proxy-Country", " US ")
	req.Header.Set("X-Proxy-Class", " Residential ")
	req.Header.Set("X-Proxy-Provider", " WebShare ")
	req.Header.Set("X-Proxy-Session-ID", "SID-1")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected request proxy URL request to succeed, got %d", resp.StatusCode)
	}
	if got.ProxyURL != "http://user:pass@proxy.example:8080" {
		t.Fatalf("expected raw proxy URL on query, got %q", got.ProxyURL)
	}
	if got.ProxyCountry != "us" || got.ProxyClass != "residential" || got.ProxyProvider != "webshare" || got.ProxySessionID != "SID-1" {
		t.Fatalf("unexpected normalized proxy metadata: %#v", got)
	}
	if header := resp.Header.Get("X-Proxy-Mode"); header != ProxyModeRequestURL {
		t.Fatalf("expected X-Proxy-Mode=%s, got %q", ProxyModeRequestURL, header)
	}
	if header := resp.Header.Get("X-Proxy-Tag"); header != "" {
		t.Fatalf("expected empty X-Proxy-Tag, got %q", header)
	}
	if header := resp.Header.Get("X-Proxy-Used"); header != "http://proxy.example:8080" {
		t.Fatalf("expected masked X-Proxy-Used, got %q", header)
	}
}

func TestRequestProxyURLBeatsTagOverride(t *testing.T) {
	var googleProxy string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
			Entries: []ProxyEntryConfig{
				{URL: "http://tag-proxy:8080", Tags: []string{"us"}},
			},
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7123, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
	req.Header.Set("X-Use-Proxy", "us")
	req.Header.Set("X-Proxy-URL", "http://request-proxy:8080")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected request to succeed, got %d", resp.StatusCode)
	}
	if googleProxy != "http://request-proxy:8080" {
		t.Fatalf("expected request proxy URL to beat tag override, got %q", googleProxy)
	}
	if got := resp.Header.Get("X-Proxy-Mode"); got != ProxyModeRequestURL {
		t.Fatalf("expected X-Proxy-Mode=%s, got %q", ProxyModeRequestURL, got)
	}
}

func TestRequestProxyOverrideDirectIgnoresRequestProxyURL(t *testing.T) {
	var googleProxy string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7119, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
	req.Header.Set("X-Use-Proxy", "direct")
	req.Header.Set("X-Proxy-URL", "http://user:pass@proxy.example:8080")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected direct override to succeed, got %d", resp.StatusCode)
	}
	if googleProxy != "" {
		t.Fatalf("expected direct override to clear proxy, got %q", googleProxy)
	}
	if header := resp.Header.Get("X-Proxy-Mode"); header != ProxyModeOff {
		t.Fatalf("expected X-Proxy-Mode=%s, got %q", ProxyModeOff, header)
	}
}

func TestRequestProxyOverrideTagBeatsGlobal(t *testing.T) {
	var googleProxy string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Global: "http://global-proxy:8080",
			Entries: []ProxyEntryConfig{
				{URL: "http://proxy-us:8080", Tags: []string{"us"}},
			},
		},
	}

	srv := NewServerWithOptions("127.0.0.1", 7100, opts, engine)
	resp := requestWithHeader(t, srv, "/google/search?text=golang", "X-Use-Proxy", "us")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected tagged override request to succeed, got %d", resp.StatusCode)
	}
	if googleProxy != "http://proxy-us:8080" {
		t.Fatalf("expected tagged override to use pool proxy, got %q", googleProxy)
	}
	if got := resp.Header.Get("X-Proxy-Tag"); got != "us" {
		t.Fatalf("expected X-Proxy-Tag=us, got %q", got)
	}
	if got := resp.Header.Get("X-Proxy-Used"); got != "http://proxy-us:8080" {
		t.Fatalf("expected X-Proxy-Used to reflect override proxy, got %q", got)
	}
}

func TestCaptchaDropsProxyLaneCookies(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
			return nil, ErrCaptcha
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
			Lanes:                DefaultProxyLanesConfig(),
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7120, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
	req.Header.Set("X-Proxy-URL", "http://user:pass@proxy.example:8080")
	req.Header.Set("X-Proxy-Session-ID", "sid-a")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected captcha failure to return 429, got %d", resp.StatusCode)
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.droppedLaneQueries) != 1 {
		t.Fatalf("expected one cookie-drop hook call, got %d", len(engine.droppedLaneQueries))
	}
	if got := engine.droppedLaneQueries[0].ProxySessionID; got != "sid-a" {
		t.Fatalf("expected drop hook to receive session id sid-a, got %q", got)
	}
}

func TestProxyErrorDoesNotDropProxyLaneCookies(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
			return nil, ErrProxyConnect
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
			Lanes:                DefaultProxyLanesConfig(),
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7121, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
	req.Header.Set("X-Proxy-URL", "http://user:pass@proxy.example:8080")
	req.Header.Set("X-Proxy-Session-ID", "sid-a")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected proxy failure before P5 status mapping, got %d", resp.StatusCode)
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.droppedLaneQueries) != 0 {
		t.Fatalf("expected no cookie-drop hook for proxy error, got %d", len(engine.droppedLaneQueries))
	}
}

func TestStableSearchErrorJSONWithProxyMeta(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{name: "captcha", err: ErrCaptcha, wantStatus: http.StatusTooManyRequests, wantError: "captcha_detected"},
		{name: "blocked", err: ErrBlocked, wantStatus: http.StatusForbidden, wantError: "blocked"},
		{name: "rate limited", err: ErrRateLimited, wantStatus: http.StatusTooManyRequests, wantError: "rate_limited"},
		{name: "search timeout", err: ErrSearchTimeout, wantStatus: http.StatusGatewayTimeout, wantError: "search_timeout"},
		{name: "proxy connect", err: ErrProxyConnect, wantStatus: http.StatusServiceUnavailable, wantError: "proxy_connect"},
		{name: "proxy auth", err: ErrProxyAuth, wantStatus: http.StatusServiceUnavailable, wantError: "proxy_auth"},
		{name: "proxy timeout", err: ErrTimeout, wantStatus: http.StatusServiceUnavailable, wantError: "proxy_timeout"},
		{name: "proxy unavailable", err: ErrProxyUnavailable, wantStatus: http.StatusServiceUnavailable, wantError: "proxy_unavailable"},
		{name: "parser", err: ErrParser, wantStatus: http.StatusBadGateway, wantError: "parser_failure"},
		{name: "engine internal", err: ErrEngineInternal, wantStatus: http.StatusBadGateway, wantError: "engine_internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &engineMock{
				name:        "google",
				initialized: true,
				searchFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
					return nil, tt.err
				},
			}

			opts := DefaultServerOptions()
			opts.Resilience.Retry.MaxRetries = 0
			opts.Resilience.Proxy = ProxyConfig{
				Runtime: ProxyRuntimeRaw,
				Proxies: ProxiesConfig{
					AllowRequestProxyURL: true,
				},
			}
			srv := NewServerWithOptions("127.0.0.1", 7122, opts, engine)

			req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
			req.Header.Set("X-Proxy-URL", "http://user:sentinel-password@proxy.example:8080")
			req.Header.Set("X-Proxy-Country", "US")
			req.Header.Set("X-Proxy-Class", "Residential")
			req.Header.Set("X-Proxy-Provider", "WebShare")
			req.Header.Set("X-Proxy-Session-ID", "sid-a")
			resp, err := srv.app.Test(req, -1)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if strings.Contains(string(body), "sentinel-password") {
				t.Fatalf("response leaked proxy password: %s", string(body))
			}

			var payload JSONErrorResponse
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if payload.Error != tt.wantError {
				t.Fatalf("expected error=%q, got %q", tt.wantError, payload.Error)
			}
			if payload.Meta["proxy_used"] != "http://proxy.example:8080" {
				t.Fatalf("expected masked proxy_used, got %#v", payload.Meta["proxy_used"])
			}
			if payload.Meta["proxy_country"] != "us" || payload.Meta["proxy_class"] != "residential" ||
				payload.Meta["proxy_provider"] != "webshare" || payload.Meta["proxy_session_id"] != "sid-a" {
				t.Fatalf("unexpected proxy meta: %#v", payload.Meta)
			}
		})
	}
}

func TestSearchErrorIncludesSanitizedDetail(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return nil, fmt.Errorf("%w: dial via %s failed", ErrProxyConnect, q.ProxyURL)
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			AllowRequestProxyURL: true,
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7123, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/search?text=golang", nil)
	req.Header.Set("X-Proxy-URL", "http://user:sentinel-password@proxy.example:8080")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if strings.Contains(string(body), "sentinel-password") {
		t.Fatalf("response leaked proxy password: %s", string(body))
	}

	var payload JSONErrorResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Meta["error_detail"] == "" {
		t.Fatalf("expected error_detail, got %#v", payload.Meta)
	}
	if !strings.Contains(payload.Meta["error_detail"].(string), "http://proxy.example:8080") {
		t.Fatalf("expected masked proxy in error_detail, got %#v", payload.Meta["error_detail"])
	}
}

func TestBrowserProxyPoolRotatesPerRequest(t *testing.T) {
	var attemptedProxies []string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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

func TestRequestProxyOverrideMissingTagFailsClosed(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Global: "http://global-proxy:8080",
		},
	}

	srv := NewServerWithOptions("127.0.0.1", 7101, opts, engine)
	resp := requestWithHeader(t, srv, "/google/search?text=golang", "X-Use-Proxy", "missing")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected missing tag override to fail closed, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Proxy-Mode"); got != ProxyModeTagPool {
		t.Fatalf("expected X-Proxy-Mode=%s on missing tag response, got %q", ProxyModeTagPool, got)
	}
	if got := resp.Header.Get("X-Proxy-Tag"); got != "missing" {
		t.Fatalf("expected X-Proxy-Tag=missing, got %q", got)
	}
}

func TestMegaProxyOverrideHeaderBeatsGlobal(t *testing.T) {
	var googleProxy string
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeRaw,
		Proxies: ProxiesConfig{
			Global: "http://global-proxy:8080",
			Entries: []ProxyEntryConfig{
				{URL: "http://proxy-us:8080", Tags: []string{"us"}},
			},
		},
	}

	srv := NewServerWithOptions("127.0.0.1", 7102, opts, engine)
	resp := requestWithHeader(t, srv, "/mega/search?text=golang&engines=google", "X-Use-Proxy", "us")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected mega override request to succeed, got %d", resp.StatusCode)
	}
	if googleProxy != "http://proxy-us:8080" {
		t.Fatalf("expected mega override to use pool proxy, got %q", googleProxy)
	}
	if got := resp.Header.Get("X-Proxy-Tag"); got != "us" {
		t.Fatalf("expected mega X-Proxy-Tag=us, got %q", got)
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			googleProxy = q.ProxyURL
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	yandexEngine := &engineMock{
		name:        "yandex",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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
	if resp.StatusCode != http.StatusBadGateway {
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
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
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

func TestDedicatedEndpointReturnsV2Envelope(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7200, opts, engine)

	resp := request(t, srv, "/google/search?text=golang")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.Version != "2.1" {
		t.Fatalf("expected meta.version=2.1, got %q", env.Meta.Version)
	}
	if env.Meta.RequestID == "" {
		t.Fatal("expected non-empty meta.request_id")
	}
	if env.Query.Text != "golang" {
		t.Fatalf("expected query.text=golang, got %q", env.Query.Text)
	}
	if len(env.Query.EnginesRequested) == 0 {
		t.Fatal("expected engines_requested to be populated")
	}
	if len(env.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	r := env.Results[0]
	if r.ID == "" {
		t.Fatal("expected result.id to be set")
	}
	if r.Domain == "" {
		t.Fatal("expected result.domain to be set")
	}
	if r.Snippet == "" && r.Title == "" {
		t.Fatal("expected result to have title or snippet")
	}
	if env.Pagination.Page < 1 {
		t.Fatalf("expected pagination.page >= 1, got %d", env.Pagination.Page)
	}
}

func TestMegaSearchReturnsV2EnvelopeWithEnginesFailed(t *testing.T) {
	good := &engineMock{name: "google", initialized: true}
	bad := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return nil, errors.New("bing down")
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7201, opts, good, bad)

	resp := request(t, srv, "/mega/search?text=golang&engines=google,bing")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 even with one engine failing, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.Version != "2.1" {
		t.Fatalf("expected meta.version=2.1, got %q", env.Meta.Version)
	}
	if len(env.Meta.EnginesFailed) == 0 {
		t.Fatal("expected engines_failed to contain bing")
	}
	found := false
	for _, name := range env.Meta.EnginesFailed {
		if name == "bing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected bing in engines_failed, got %v", env.Meta.EnginesFailed)
	}
	if len(env.Meta.EngineErrors) != 1 || env.Meta.EngineErrors[0].Engine != "bing" || env.Meta.EngineErrors[0].Message == "" {
		t.Fatalf("expected bing engine error detail, got %#v", env.Meta.EngineErrors)
	}
}

func TestMegaSearchReturnsAggregateNetworkBytesHeader(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(ctx context.Context, q Query) ([]SearchResult, error) {
			AddNetworkBytes(ctx, 10)
			return []SearchResult{{Rank: 1, URL: "https://example.com/google", Title: "google"}}, nil
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(ctx context.Context, q Query) ([]SearchResult, error) {
			AddNetworkBytes(ctx, 20)
			return []SearchResult{{Rank: 1, URL: "https://example.com/bing", Title: "bing"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7212, opts, google, bing)

	resp := request(t, srv, "/mega/search?text=golang&engines=google,bing")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Network-Bytes"); got != "30" {
		t.Fatalf("expected X-Network-Bytes=30, got %q", got)
	}
}

func TestMegaSearchAllFailuresReturnsDetails(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return nil, fmt.Errorf("%w: selector timeout", ErrSearchTimeout)
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return nil, fmt.Errorf("%w: 403", ErrBlocked)
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7210, opts, google, bing)

	resp := requestWithHeader(t, srv, "/mega/search?text=golang&engines=google,bing", "X-Request-ID", "mega-fail")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}

	var payload JSONErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.RequestID != "mega-fail" {
		t.Fatalf("expected request_id=mega-fail, got %q", payload.RequestID)
	}
	if payload.Error != "all_engines_failed" {
		t.Fatalf("expected all_engines_failed, got %q", payload.Error)
	}
	engineErrors, ok := payload.Meta["engine_errors"].([]interface{})
	if !ok || len(engineErrors) != 2 {
		t.Fatalf("expected two engine_errors, got %#v", payload.Meta["engine_errors"])
	}
}

func TestMegaSearchClustersGroupSameURL(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://go.dev/", Title: "Go"}}, nil
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 2, URL: "https://go.dev/", Title: "Go"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7202, opts, google, bing)

	resp := request(t, srv, "/mega/search?text=golang&engines=google,bing")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Clusters == nil || len(*env.Clusters) == 0 {
		t.Fatal("expected clusters to be populated")
	}
	c := (*env.Clusters)[0]
	if c.EnginesCount < 2 {
		t.Fatalf("expected cluster with 2 engines, got %d", c.EnginesCount)
	}
	if c.Score <= 0 {
		t.Fatalf("expected positive cluster score, got %f", c.Score)
	}
}

func TestFormatParamReturnsCorrectContentType(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7204, opts, engine)

	tests := []struct {
		format       string
		wantCT       string
		wantContains string
	}{
		{"json", "application/json", `"version"`},
		{"markdown", "text/markdown", "# Search results"},
		{"text", "text/plain", "Search:"},
		{"ndjson", "application/x-ndjson", `"id"`},
	}
	for i, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			// Use a unique query per subtest to avoid cache cross-contamination.
			resp := request(t, srv, fmt.Sprintf("/google/search?text=query%d&format=%s", i, tt.format))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, tt.wantCT) {
				t.Fatalf("expected Content-Type to contain %q, got %q", tt.wantCT, ct)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tt.wantContains) {
				t.Fatalf("expected body to contain %q, got: %s", tt.wantContains, string(body)[:min(200, len(body))])
			}
		})
	}
}

func TestJSONOmitsRedundantResultFields(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
			return []SearchResult{{
				Rank:         1,
				AbsoluteRank: 2,
				URL:          "https://example.gov/page",
				Title:        "Gov Result",
				Description:  "Snippet",
			}}, nil
		},
	}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7210, opts, engine)

	resp := request(t, srv, "/google/search?text=clean-json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	payload := string(body)
	for _, omitted := range []string{`"is_ad"`, `"on_page"`, `"is_gov"`} {
		if strings.Contains(payload, omitted) {
			t.Fatalf("json payload should not contain %s: %s", omitted, payload)
		}
	}
	if strings.Contains(payload, `"position":{"absolute":2,"page"`) {
		t.Fatalf("json payload should not contain per-result page: %s", payload)
	}
	if !strings.Contains(payload, `"type":"organic"`) {
		t.Fatalf("json payload should keep result type: %s", payload)
	}
	if !strings.Contains(payload, `"category":"gov"`) {
		t.Fatalf("json payload should collapse domain category: %s", payload)
	}
	if !strings.Contains(payload, `"absolute":2`) {
		t.Fatalf("json payload should keep meaningful absolute position: %s", payload)
	}
}

func TestFormatParamBypassesJSONCache(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7205, opts, engine)

	first := request(t, srv, "/google/search?text=cache-format&format=json")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", first.StatusCode)
	}

	second := request(t, srv, "/google/search?text=cache-format&format=markdown")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second request 200, got %d", second.StatusCode)
	}
	if ct := second.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Fatalf("expected markdown content type, got %q", ct)
	}
	body, _ := io.ReadAll(second.Body)
	if !strings.Contains(string(body), "# Search results") {
		t.Fatalf("expected markdown body, got %q", string(body))
	}

	engine.mu.Lock()
	searchCalls := engine.searchCalls
	engine.mu.Unlock()
	if searchCalls != 2 {
		t.Fatalf("expected markdown request to bypass JSON cache, got %d search calls", searchCalls)
	}
}

func TestCachedEnvelopeRefreshesRequestID(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7206, opts, engine)

	first := requestWithHeader(t, srv, "/google/search?text=cache-id", "X-Request-ID", "req-first")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", first.StatusCode)
	}
	second := requestWithHeader(t, srv, "/google/search?text=cache-id", "X-Request-ID", "req-second")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected second request 200, got %d", second.StatusCode)
	}
	if got := second.Header.Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected cache hit, got %q", got)
	}

	var env Envelope
	if err := json.NewDecoder(second.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.RequestID != "req-second" {
		t.Fatalf("expected refreshed meta.request_id, got %q", env.Meta.RequestID)
	}
	if got := second.Header.Get("X-Request-ID"); got != env.Meta.RequestID {
		t.Fatalf("expected header/body request IDs to match, header=%q body=%q", got, env.Meta.RequestID)
	}
}

func TestPaginatedPositionUsesAbsoluteRank(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 11, URL: "https://example.com/page", Title: "Page"}}, nil
		},
	}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7207, opts, engine)

	resp := request(t, srv, "/google/search?text=page&start=10&limit=10")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(env.Results))
	}
	if env.Results[0].Position == nil || env.Results[0].Position.Absolute != 11 {
		t.Fatalf("expected absolute position 11, got %+v", env.Results[0].Position)
	}
}

func TestImageEnvelopeEnrichesMetadataFromDescription(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		imageFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{
				Rank:        1,
				URL:         "https://cdn.example.com/image.jpg",
				Title:       "Image",
				Description: "Height:800, Width:1200, Source Page: https://example.com/article",
			}}, nil
		},
	}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7208, opts, engine)

	resp := request(t, srv, "/google/image?text=image")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env ImageEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Results) != 1 {
		t.Fatalf("expected one image result, got %d", len(env.Results))
	}
	got := env.Results[0]
	if got.Image.Width != 1200 || got.Image.Height != 800 {
		t.Fatalf("expected image dimensions 1200x800, got %dx%d", got.Image.Width, got.Image.Height)
	}
	if got.Source.PageURL != "https://example.com/article" || got.Source.Domain != "example.com" {
		t.Fatalf("unexpected source: %+v", got.Source)
	}
}

func TestMegaSearchDeduplicatesByNormalizedURLDeterministically(t *testing.T) {
	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 2, URL: "https://example.com/page?utm_source=test", Title: "Google"}}, nil
		},
	}
	bing := &engineMock{
		name:        "bing",
		initialized: true,
		searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: "https://example.com/page", Title: "Bing"}}, nil
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7209, opts, google, bing)

	resp := request(t, srv, "/mega/search?text=dedupe&engines=google,bing")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Results) != 1 {
		t.Fatalf("expected one deduped result, got %d", len(env.Results))
	}
	if env.Results[0].Engine != "bing" || env.Results[0].Rank != 1 {
		t.Fatalf("expected best-ranked bing result, got engine=%q rank=%d", env.Results[0].Engine, env.Results[0].Rank)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestResultIDIsStableAcrossRequests(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7203, opts, engine)

	first := request(t, srv, "/google/search?text=golang")
	second := request(t, srv, "/google/search?text=golang&limit=26") // different limit → bypasses cache

	var e1, e2 Envelope
	if err := json.NewDecoder(first.Body).Decode(&e1); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.NewDecoder(second.Body).Decode(&e2); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if len(e1.Results) == 0 || len(e2.Results) == 0 {
		t.Skip("no results to compare IDs")
	}
	if e1.Results[0].ID != e2.Results[0].ID {
		t.Fatalf("expected stable ID across requests, got %q vs %q", e1.Results[0].ID, e2.Results[0].ID)
	}
}

func TestUseProfileHeaderRejectsUnknownID(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7220, DefaultServerOptions(), engine)

	resp := requestWithHeader(t, srv, "/google/search?text=test", "X-Use-Profile", "does-not-exist")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown profile id, got %d", resp.StatusCode)
	}
}

func TestUseProfileHeaderAcceptsValidID(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7221, DefaultServerOptions(), engine)

	resp := requestWithHeader(t, srv, "/google/search?text=test", "X-Use-Profile", "chrome-linux-mesa-uhd620")
	// The mock engine doesn't do a real browser search, so we just verify the
	// request was not rejected at the profile-validation layer.
	if resp.StatusCode == http.StatusBadRequest {
		t.Fatalf("expected request with valid profile id to pass validation, got 400")
	}
}

func TestUseProfileHeaderFingerprintCheckRejectsUnknownID(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	opts := DefaultServerOptions()
	opts.EnableDebugEndpoints = true
	srv := NewServerWithOptions("127.0.0.1", 7222, opts, engine)

	resp := requestWithHeader(t, srv, "/debug/fingerprint-check?detector=sannysoft", "X-Use-Profile", "does-not-exist")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown profile id on fingerprint endpoint, got %d", resp.StatusCode)
	}
}

func TestUseProfileHeaderExposedInCORS(t *testing.T) {
	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7223, DefaultServerOptions(), engine)

	req := httptest.NewRequest(http.MethodOptions, "/google/search", nil)
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "X-Use-Profile") {
		t.Fatalf("expected X-Use-Profile in Access-Control-Allow-Headers, got %q", allowHeaders)
	}
}

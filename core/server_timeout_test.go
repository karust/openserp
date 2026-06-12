package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FP-2: every endpoint that doesn't manage its own deadline budget must get
// one from RequestTimeoutMiddleware; /mega/* (MegaTimeout) and /extract
// (batch budget) keep theirs.
func TestRequestTimeoutMiddlewareSetsDeadlineExceptBudgetedPaths(t *testing.T) {
	app := fiber.New()
	app.Use(RequestTimeoutMiddleware(time.Minute))

	deadlines := map[string]bool{}
	// c.Path() aliases fasthttp's reusable buffer, so capture the path
	// explicitly instead of using it as a map key after the request ends.
	record := func(path string) fiber.Handler {
		return func(c *fiber.Ctx) error {
			_, ok := c.UserContext().Deadline()
			deadlines[path] = ok
			return c.SendStatus(http.StatusOK)
		}
	}
	app.Get("/google/search", record("/google/search"))
	app.Post("/google/parse", record("/google/parse"))
	app.Get("/mega/search", record("/mega/search"))
	app.Get("/extract", record("/extract"))

	for path, method := range map[string]string{
		"/google/search": http.MethodGet,
		"/google/parse":  http.MethodPost,
		"/mega/search":   http.MethodGet,
		"/extract":       http.MethodGet,
	} {
		req := httptest.NewRequest(method, path, nil)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed for %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: handler did not run, status %d", path, resp.StatusCode)
		}
	}

	for path, want := range map[string]bool{
		"/google/search": true,
		"/google/parse":  true,
		"/mega/search":   false,
		"/extract":       false,
	} {
		if deadlines[path] != want {
			t.Errorf("%s: deadline attached = %v, want %v", path, deadlines[path], want)
		}
	}
}

// The derived deadline must never truncate a healthy request that exhausts
// its full retry budget: attempts + worst-case jittered backoffs + slack.
func TestRequestTimeoutForRetriesCoversFullRetryBudget(t *testing.T) {
	cases := []struct {
		name           string
		attemptTimeout time.Duration
		cfg            RetryConfig
		want           time.Duration
	}{
		{
			name:           "defaults: 4x15s attempts + (1.5+3+6)s backoff + 5s slack",
			attemptTimeout: 15 * time.Second,
			cfg:            DefaultRetryConfig(),
			want:           75500 * time.Millisecond,
		},
		{
			name:           "no retries: one attempt + slack",
			attemptTimeout: 15 * time.Second,
			cfg:            RetryConfig{MaxRetries: 0},
			want:           20 * time.Second,
		},
		{
			name:           "backoffs capped at MaxBackoff",
			attemptTimeout: 10 * time.Second,
			cfg: RetryConfig{
				MaxRetries:     2,
				InitialBackoff: time.Minute,
				MaxBackoff:     2 * time.Second,
				BackoffFactor:  2.0,
			},
			want: 30*time.Second + 4*time.Second + 5*time.Second,
		},
	}

	for _, tc := range cases {
		if got := RequestTimeoutForRetries(tc.attemptTimeout, tc.cfg); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestHungEngineReturns504RequestTimeoutAndServerSurvives(t *testing.T) {
	hung := func(ctx context.Context, _ Query) ([]SearchResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn:    hung,
		imageFn:     hung,
	}

	opts := DefaultServerOptions()
	opts.RequestTimeout = 100 * time.Millisecond
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7140, opts, engine)

	for _, path := range []string{"/google/search?text=golang", "/google/image?text=golang"} {
		started := time.Now()
		resp := request(t, srv, path)
		if resp.StatusCode != http.StatusGatewayTimeout {
			t.Fatalf("%s: expected 504 for hung engine, got %d", path, resp.StatusCode)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("%s: request took %s, deadline did not bound it", path, elapsed)
		}
		var payload JSONErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("%s: decode error response: %v", path, err)
		}
		if payload.Error != "request_timeout" {
			t.Fatalf("%s: expected error=request_timeout, got %q", path, payload.Error)
		}
	}

	// Server keeps serving after timed-out requests.
	engine.searchFn = nil
	second := request(t, srv, "/google/search?text=golang")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected server to keep serving after timeouts, got %d", second.StatusCode)
	}
}

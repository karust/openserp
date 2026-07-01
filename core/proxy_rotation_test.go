package core

import (
	"context"
	"sync"
	"testing"

	"golang.org/x/time/rate"
)

// captchaThenSuccessEngine returns ErrCaptcha for every proxy URL except the
// one designated as healthy, where it succeeds. It records the proxy URL of
// each attempt so the test can assert rotation happened.
type captchaThenSuccessEngine struct {
	name        string
	goodProxy   string
	mu          sync.Mutex
	seenProxies []string
}

func (e *captchaThenSuccessEngine) Name() string                  { return e.name }
func (e *captchaThenSuccessEngine) IsInitialized() bool           { return true }
func (e *captchaThenSuccessEngine) GetRateLimiter() *rate.Limiter { return nil }

func (e *captchaThenSuccessEngine) Search(ctx context.Context, q Query) ([]SearchResult, error) {
	e.mu.Lock()
	e.seenProxies = append(e.seenProxies, q.ProxyURL)
	e.mu.Unlock()
	if q.ProxyURL == e.goodProxy {
		return []SearchResult{{Title: "ok", URL: "https://example.com", Rank: 1}}, nil
	}
	return nil, ErrCaptcha
}

func (e *captchaThenSuccessEngine) SearchImage(ctx context.Context, q Query) ([]SearchResult, error) {
	return e.Search(ctx, q)
}

func (e *captchaThenSuccessEngine) attempts() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.seenProxies)
}

func tagPoolSearcher(t *testing.T, engine SearchEngine, entries []ProxyEntryConfig) *ResilientSearcher {
	t.Helper()
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 0
	cfg.CircuitBreaker.FailureThreshold = 5
	cfg.Proxy = ProxyConfig{
		Runtime:        ProxyRuntimeBrowser,
		Proxies:        ProxiesConfig{Entries: entries},
		EnginePolicies: map[string]string{engine.Name(): "rot"},
	}
	return NewResilientSearcher([]SearchEngine{engine}, cfg)
}

func TestReportChallengedDeprioritizesWithoutDisabling(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"rot"}},
		{URL: "http://proxy2:8080", Tags: []string{"rot"}},
	}, 3)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}
	ctx := context.Background()

	// Challenge proxy1 from a fresh index; the next selection must skip it.
	registry.ReportChallenged(ctx, "http://proxy1:8080")
	if got := registry.NextByTag("rot"); got != "http://proxy2:8080" {
		t.Fatalf("expected challenged proxy to be skipped, got %q", got)
	}

	// Health is untouched: both proxies still count as healthy.
	if n := registry.HealthyCountForTag("rot"); n != 2 {
		t.Fatalf("challenge must not degrade health, healthy=%d", n)
	}

	// When both are challenged, rotation still serves one (relaxed second pass).
	registry.ReportChallenged(ctx, "http://proxy2:8080")
	if got := registry.NextByTag("rot"); got == "" {
		t.Fatal("expected a proxy even when all are challenged")
	}
}

func TestSearchWithProtection_RotatesProxyOnCaptcha(t *testing.T) {
	// Two proxies in the same tag pool; the second one is the one that works.
	// NextByTag serves proxy1 first, so the first attempt gets a captcha and the
	// retry should pick proxy2 and succeed.
	good := "http://proxy2:8080"
	engine := &captchaThenSuccessEngine{name: "google", goodProxy: good}
	rs := tagPoolSearcher(t, engine, []ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"rot"}},
		{URL: good, Tags: []string{"rot"}},
	})

	results, _, meta, err := rs.SearchPrimary(context.Background(), engine, Query{Text: "rotate"})
	if err != nil {
		t.Fatalf("expected success after rotation, got %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if meta.Attempts != 2 {
		t.Fatalf("expected 2 proxy attempts, got %d", meta.Attempts)
	}
	if got := engine.attempts(); got != 2 {
		t.Fatalf("expected engine called twice, got %d", got)
	}
}

func TestSearchWithProtection_NoRotationWithSingleProxy(t *testing.T) {
	// Only one proxy in the pool: captcha must fail fast without a second
	// attempt (HealthyCountForTag < 2).
	engine := &captchaThenSuccessEngine{name: "google", goodProxy: "http://unused:8080"}
	rs := tagPoolSearcher(t, engine, []ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"rot"}},
	})

	_, _, meta, err := rs.SearchPrimary(context.Background(), engine, Query{Text: "single"})
	if err == nil {
		t.Fatal("expected captcha failure with a single proxy")
	}
	if meta.Attempts != 1 {
		t.Fatalf("expected exactly 1 attempt with single proxy, got %d", meta.Attempts)
	}
	if got := engine.attempts(); got != 1 {
		t.Fatalf("expected engine called once, got %d", got)
	}
}

func TestSearchWithProtection_GlobalProxyChallengeReportsNoRotation(t *testing.T) {
	engine := &captchaThenSuccessEngine{name: "google", goodProxy: "http://unused:8080"}
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 0
	cfg.CircuitBreaker.FailureThreshold = 5
	cfg.Proxy = ProxyConfig{
		Runtime: ProxyRuntimeBrowser,
		Proxies: ProxiesConfig{
			Global: "http://global-proxy:8080",
		},
	}
	rs := NewResilientSearcher([]SearchEngine{engine}, cfg)

	_, _, meta, err := rs.SearchPrimary(context.Background(), engine, Query{Text: "global"})
	if err == nil {
		t.Fatal("expected captcha failure with a global proxy")
	}
	if meta.Attempts != 1 {
		t.Fatalf("expected exactly 1 attempt with global proxy, got %d", meta.Attempts)
	}
	if got := engine.attempts(); got != 1 {
		t.Fatalf("expected engine called once, got %d", got)
	}
}

func TestSearchWithProtection_DirectModeFailsFastOnCaptcha(t *testing.T) {
	// Direct mode (no proxy config): captcha is non-retryable and rotation must
	// not kick in.
	engine := &captchaThenSuccessEngine{name: "google", goodProxy: "http://never:8080"}
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 0
	rs := NewResilientSearcher([]SearchEngine{engine}, cfg)

	_, _, meta, err := rs.SearchPrimary(context.Background(), engine, Query{Text: "direct"})
	if err == nil {
		t.Fatal("expected captcha failure in direct mode")
	}
	if meta.Attempts > 1 {
		t.Fatalf("direct mode must not rotate proxies, attempts=%d", meta.Attempts)
	}
	if got := engine.attempts(); got != 1 {
		t.Fatalf("expected engine called once in direct mode, got %d", got)
	}
}

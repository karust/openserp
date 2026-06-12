package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type blockingContextEngine struct {
	started chan struct{}
}

func (e *blockingContextEngine) Name() string {
	return "blocking"
}

func (e *blockingContextEngine) IsInitialized() bool {
	return true
}

func (e *blockingContextEngine) GetRateLimiter() *rate.Limiter {
	return nil
}

func (e *blockingContextEngine) Search(ctx context.Context, q Query) ([]SearchResult, error) {
	_ = q
	close(e.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (e *blockingContextEngine) SearchImage(ctx context.Context, q Query) ([]SearchResult, error) {
	_ = q
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestResilientSearchPrimary_CancelledContextStopsWithin100ms(t *testing.T) {
	engine := &blockingContextEngine{started: make(chan struct{})}
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 2
	rs := NewResilientSearcher([]SearchEngine{engine}, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, _, _, err := rs.SearchPrimary(ctx, engine, Query{Text: "cancel-me"})
		done <- err
	}()

	select {
	case <-engine.started:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not start")
	}

	start := time.Now()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("expected cancellation within 100ms, got %s", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("search did not stop after context cancellation")
	}
}

type staticErrorEngine struct {
	name string
	err  error
}

func (e *staticErrorEngine) Name() string { return e.name }

func (e *staticErrorEngine) IsInitialized() bool { return true }

func (e *staticErrorEngine) GetRateLimiter() *rate.Limiter { return nil }

func (e *staticErrorEngine) Search(context.Context, Query) ([]SearchResult, error) {
	return nil, e.err
}

func (e *staticErrorEngine) SearchImage(context.Context, Query) ([]SearchResult, error) {
	return nil, e.err
}

func TestResilientSearchPrimary_ContextErrorsDoNotOpenCircuit(t *testing.T) {
	engine := &staticErrorEngine{name: "cancelled", err: context.Canceled}
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 0
	cfg.CircuitBreaker.FailureThreshold = 1
	rs := NewResilientSearcher([]SearchEngine{engine}, cfg)

	_, _, _, err := rs.SearchPrimary(context.Background(), engine, Query{Text: "cancel-me"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if state := rs.cbManager.Get(engine.Name()).State(); state != CircuitClosed {
		t.Fatalf("circuit state = %s, want closed", state)
	}
}

type slowLimiterEngine struct {
	staticErrorEngine
	limiter *rate.Limiter
}

func (e *slowLimiterEngine) GetRateLimiter() *rate.Limiter { return e.limiter }

func TestResilientSearchPrimary_LimiterDeadlineDoesNotOpenCircuit(t *testing.T) {
	// Drained limiter with a 1h refill: any deadline-bounded Wait fails with
	// rate's bare "would exceed context deadline" error, not a context error.
	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	if !limiter.Allow() {
		t.Fatal("expected initial burst token")
	}
	engine := &slowLimiterEngine{
		staticErrorEngine: staticErrorEngine{name: "ratelimited"},
		limiter:           limiter,
	}
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 0
	cfg.CircuitBreaker.FailureThreshold = 1
	rs := NewResilientSearcher([]SearchEngine{engine}, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, _, err := rs.SearchPrimary(ctx, engine, Query{Text: "limited"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if state := rs.cbManager.Get(engine.Name()).State(); state != CircuitClosed {
		t.Fatalf("circuit state = %s, want closed", state)
	}
}

func TestResilientSearchPrimary_EngineFailureStillOpensCircuit(t *testing.T) {
	engine := &staticErrorEngine{name: "parser", err: ErrParser}
	cfg := DefaultResilientConfig()
	cfg.Retry.MaxRetries = 0
	cfg.CircuitBreaker.FailureThreshold = 1
	rs := NewResilientSearcher([]SearchEngine{engine}, cfg)

	_, _, _, err := rs.SearchPrimary(context.Background(), engine, Query{Text: "break-me"})
	if !errors.Is(err, ErrParser) {
		t.Fatalf("expected ErrParser, got %v", err)
	}
	if state := rs.cbManager.Get(engine.Name()).State(); state != CircuitOpen {
		t.Fatalf("circuit state = %s, want open", state)
	}
}

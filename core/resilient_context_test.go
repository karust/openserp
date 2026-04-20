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

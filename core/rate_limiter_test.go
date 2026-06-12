package core

import (
	"context"
	"testing"
	"time"
)

func TestSearchEngineOptionsGetRateLimiterCachesLimiterAndPaces(t *testing.T) {
	opts := SearchEngineOptions{
		RateRequests:    20,
		RateTime:        1,
		RateBurst:       1,
		SelectorTimeout: 1,
	}

	limiter := opts.GetRateLimiter()
	if limiter == nil {
		t.Fatal("expected limiter")
	}
	if got := opts.GetRateLimiter(); got != limiter {
		t.Fatal("expected GetRateLimiter to return the cached limiter")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}

	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("second wait failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("second wait elapsed %s, want pacing near 50ms", elapsed)
	}
}

package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryableSearch_SuccessOnFirstAttempt(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 3, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, BackoffFactor: 2.0}
	calls := 0

	result := RetryableSearch(context.Background(), cfg, "test", func(ctx context.Context) ([]SearchResult, error) {
		calls++
		if ctx == nil {
			t.Fatal("expected non-nil context")
		}
		return []SearchResult{{Title: "result1"}}, nil
	})

	if result.Err != nil {
		t.Fatalf("expected no error, got: %v", result.Err)
	}
	if result.Attempts != 1 {
		t.Errorf("expected 1 attempt, got: %d", result.Attempts)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got: %d", calls)
	}
}

func TestRetryableSearch_AllAttemptsFail(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 2, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, BackoffFactor: 2.0}
	calls := 0

	result := RetryableSearch(context.Background(), cfg, "test", func(context.Context) ([]SearchResult, error) {
		calls++
		return nil, errors.New("persistent failure")
	})

	if result.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (1 + 2 retries), got: %d", calls)
	}
	if result.Attempts != 3 {
		t.Errorf("expected 3 attempts, got: %d", result.Attempts)
	}
}

func TestRetryableSearch_CaptchaNotRetried(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 3, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, BackoffFactor: 2.0}
	calls := 0

	result := RetryableSearch(context.Background(), cfg, "test", func(context.Context) ([]SearchResult, error) {
		calls++
		return nil, ErrCaptcha
	})

	if !errors.Is(result.Err, ErrCaptcha) {
		t.Fatalf("expected ErrCaptcha, got: %v", result.Err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got: %d", calls)
	}
}

func TestRetryableSearch_ContextCanceledStopsBackoffImmediately(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: time.Second,
		MaxBackoff:     time.Second,
		BackoffFactor:  2.0,
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	start := time.Now()
	result := RetryableSearch(ctx, cfg, "test", func(context.Context) ([]SearchResult, error) {
		calls++
		cancel()
		return nil, errors.New("trigger retry")
	})
	elapsed := time.Since(start)

	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", result.Err)
	}
	if calls != 1 {
		t.Fatalf("expected a single attempt before cancel, got %d", calls)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("expected fast cancel, got %s", elapsed)
	}
}

func TestCalculateBackoff(t *testing.T) {
	cfg := RetryConfig{InitialBackoff: 1 * time.Second, MaxBackoff: 10 * time.Second, BackoffFactor: 2.0}

	tests := []struct {
		attempt int
		min     time.Duration
		max     time.Duration
	}{
		{1, 500 * time.Millisecond, 1500 * time.Millisecond},
		{2, 1 * time.Second, 3 * time.Second},
		{3, 2 * time.Second, 6 * time.Second},
		{4, 4 * time.Second, 10 * time.Second},
		{5, 5 * time.Second, 10 * time.Second},
	}

	for _, tt := range tests {
		got := calculateBackoff(cfg, tt.attempt)
		if got < tt.min || got > tt.max {
			t.Errorf("attempt %d: expected range [%s,%s], got %s", tt.attempt, tt.min, tt.max, got)
		}
	}
}

package core

import (
	"errors"
	"testing"
	"time"
)

func TestRetryableSearch_SuccessOnFirstAttempt(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 3, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, BackoffFactor: 2.0}
	calls := 0

	result := RetryableSearch(cfg, "test", func() ([]SearchResult, error) {
		calls++
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

	result := RetryableSearch(cfg, "test", func() ([]SearchResult, error) {
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

	result := RetryableSearch(cfg, "test", func() ([]SearchResult, error) {
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

func TestCalculateBackoff(t *testing.T) {
	cfg := RetryConfig{InitialBackoff: 1 * time.Second, MaxBackoff: 10 * time.Second, BackoffFactor: 2.0}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second},
	}

	for _, tt := range tests {
		got := calculateBackoff(cfg, tt.attempt)
		if got != tt.expected {
			t.Errorf("attempt %d: expected %s, got %s", tt.attempt, tt.expected, got)
		}
	}
}

package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"
)

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
	}
}

type RetryResult struct {
	Results  []SearchResult
	Err      error
	Attempts int
	Engine   string
}

// RetryableSearch executes searchFn with exponential backoff retries.
// CAPTCHA, parser, engine-internal, and proxy-unavailable errors are not retried.
func RetryableSearch(ctx context.Context, cfg RetryConfig, engineName string, searchFn func(context.Context) ([]SearchResult, error)) RetryResult {
	ctx = EnsureContext(ctx)
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return RetryResult{
				Err:      err,
				Attempts: attempt,
				Engine:   engineName,
			}
		}

		if attempt > 0 {
			backoff := calculateBackoff(cfg, attempt)
			logrus.Warnf("[%s] Retry attempt %d/%d after %s", engineName, attempt, cfg.MaxRetries, backoff)
			if err := SleepContext(ctx, backoff); err != nil {
				return RetryResult{
					Err:      err,
					Attempts: attempt,
					Engine:   engineName,
				}
			}
		}

		results, err := searchFn(ctx)
		if err == nil {
			if attempt > 0 {
				logrus.Infof("[%s] Succeeded on retry attempt %d", engineName, attempt)
			}
			return RetryResult{
				Results:  results,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}

		lastErr = err
		if errors.Is(err, ErrCaptcha) {
			logrus.Warnf("[%s] CAPTCHA detected, skipping retries", engineName)
			return RetryResult{
				Err:      err,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}
		if errors.Is(err, ErrProxyUnavailable) {
			logrus.Warnf("[%s] Proxy unavailable, skipping retries", engineName)
			return RetryResult{
				Err:      err,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}
		if errors.Is(err, ErrParser) {
			logrus.Warnf("[%s] Parser failure, skipping retries", engineName)
			return RetryResult{
				Err:      err,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}
		if errors.Is(err, ErrEngineInternal) {
			logrus.Warnf("[%s] Engine panic recovered, skipping retries", engineName)
			return RetryResult{
				Err:      err,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}
		if IsContextDone(err) {
			logrus.Warnf("[%s] Context canceled/deadline exceeded, skipping retries", engineName)
			return RetryResult{
				Err:      err,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}

		logrus.Warnf("[%s] Attempt %d failed: %s", engineName, attempt+1, err)
	}

	return RetryResult{
		Err:      fmt.Errorf("all %d attempts failed for %s: %w", cfg.MaxRetries+1, engineName, lastErr),
		Attempts: cfg.MaxRetries + 1,
		Engine:   engineName,
	}
}

func calculateBackoff(cfg RetryConfig, attempt int) time.Duration {
	backoff := float64(cfg.InitialBackoff) * math.Pow(cfg.BackoffFactor, float64(attempt-1))
	if backoff > float64(cfg.MaxBackoff) {
		backoff = float64(cfg.MaxBackoff)
	}
	if backoff < 0 {
		backoff = 0
	}
	backoff = backoff * (0.5 + rand.Float64())
	if backoff > float64(cfg.MaxBackoff) {
		backoff = float64(cfg.MaxBackoff)
	}
	return time.Duration(backoff)
}

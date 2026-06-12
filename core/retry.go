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
// CAPTCHA, block, rate-limit, parser, engine-internal, and proxy-unavailable errors are not retried.
func RetryableSearch(ctx context.Context, cfg RetryConfig, engineName string, searchFn func(context.Context) ([]SearchResult, error)) RetryResult {
	ctx = WithEngine(EnsureContext(ctx), engineName)
	logger := WithRequest(ctx)
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
			logger.WithFields(logrus.Fields{
				"attempt": attempt,
				"backoff": backoff.String(),
			}).Warnf("Retry %d/%d after %s", attempt, cfg.MaxRetries, backoff)
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
			return RetryResult{
				Results:  results,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}

		lastErr = err
		if reason, skip := nonRetryableReason(err); skip {
			logger.Warnf("%s, skipping retries", reason)
			return RetryResult{
				Err:      err,
				Attempts: attempt + 1,
				Engine:   engineName,
			}
		}

		logger.WithField("attempt", attempt+1).Debugf("Attempt %d failed: %s", attempt+1, err)
	}

	return RetryResult{
		Err:      fmt.Errorf("all %d attempts failed for %s: %w", cfg.MaxRetries+1, engineName, lastErr),
		Attempts: cfg.MaxRetries + 1,
		Engine:   engineName,
	}
}

// requestTimeoutSlack covers per-request pipeline overhead that happens
// outside engine attempts: rate-limiter waits, proxy selection, parsing.
const requestTimeoutSlack = 5 * time.Second

// RequestTimeoutForRetries derives a per-request deadline that a healthy
// request exhausting its full retry budget cannot hit: worst-case attempts
// (each bounded by attemptTimeout) plus worst-case jittered backoffs plus
// slack. Used as the server-wide request timeout so raising MaxRetries or
// the engine timeout never silently truncates retries.
func RequestTimeoutForRetries(attemptTimeout time.Duration, cfg RetryConfig) time.Duration {
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}

	budget := time.Duration(cfg.MaxRetries+1) * attemptTimeout
	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		// calculateBackoff jitters by (0.5 + rand[0,1)), so 1.5x is the worst
		// case before the MaxBackoff cap.
		worst := time.Duration(1.5 * float64(cfg.InitialBackoff) * math.Pow(cfg.BackoffFactor, float64(attempt-1)))
		if worst > cfg.MaxBackoff || worst < 0 {
			worst = cfg.MaxBackoff
		}
		budget += worst
	}
	return budget + requestTimeoutSlack
}

var nonRetryableSentinels = []struct {
	err    error
	reason string
}{
	{ErrCaptcha, "CAPTCHA detected"},
	{ErrBlocked, "Blocked response detected"},
	{ErrRateLimited, "Rate limited response detected"},
	{ErrProxyUnavailable, "Proxy unavailable"},
	{ErrParser, "Parser failure"},
	{ErrEngineInternal, "Engine panic recovered"},
}

func nonRetryableReason(err error) (string, bool) {
	for _, s := range nonRetryableSentinels {
		if errors.Is(err, s.err) {
			return s.reason, true
		}
	}
	if IsContextDone(err) {
		return "Context canceled/deadline exceeded", true
	}
	return "", false
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

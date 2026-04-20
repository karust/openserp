package core

import (
	"context"
	"errors"
	"time"
)

// EnsureContext returns ctx when set; otherwise a non-nil placeholder context.
func EnsureContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.TODO()
}

// SleepContext blocks for d or until ctx is canceled.
func SleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	ctx = EnsureContext(ctx)

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsContextDone reports whether err is a cancellation/deadline error.
func IsContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

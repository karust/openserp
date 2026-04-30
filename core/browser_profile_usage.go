package core

import (
	"context"
	"slices"
	"strings"
	"sync"
)

type browserProfileUsageContextKey struct{}

type browserProfileUsageTracker struct {
	mu  sync.Mutex
	ids []string
}

func WithBrowserProfileUsage(ctx context.Context) context.Context {
	ctx = EnsureContext(ctx)
	if browserProfileUsageFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, browserProfileUsageContextKey{}, &browserProfileUsageTracker{})
}

func SetBrowserProfileID(ctx context.Context, profileID string) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return
	}
	tracker := browserProfileUsageFromContext(ctx)
	if tracker == nil {
		return
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if slices.Contains(tracker.ids, profileID) {
		return
	}
	tracker.ids = append(tracker.ids, profileID)
}

func BrowserProfileIDsFromContext(ctx context.Context) []string {
	tracker := browserProfileUsageFromContext(ctx)
	if tracker == nil {
		return nil
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	out := make([]string, len(tracker.ids))
	copy(out, tracker.ids)
	return out
}

func browserProfileUsageFromContext(ctx context.Context) *browserProfileUsageTracker {
	if ctx == nil {
		return nil
	}
	tracker, _ := ctx.Value(browserProfileUsageContextKey{}).(*browserProfileUsageTracker)
	return tracker
}

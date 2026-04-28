package core

import (
	"context"
	"sync/atomic"
)

type networkUsageContextKey struct{}

type networkUsageTracker struct {
	bytes atomic.Int64
}

func WithNetworkUsage(ctx context.Context) context.Context {
	ctx = EnsureContext(ctx)
	if networkUsageFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, networkUsageContextKey{}, &networkUsageTracker{})
}

func AddNetworkBytes(ctx context.Context, n int64) {
	if n <= 0 {
		return
	}
	if tracker := networkUsageFromContext(ctx); tracker != nil {
		tracker.bytes.Add(n)
	}
}

func NetworkBytesFromContext(ctx context.Context) int64 {
	if tracker := networkUsageFromContext(ctx); tracker != nil {
		return tracker.bytes.Load()
	}
	return 0
}

func networkUsageFromContext(ctx context.Context) *networkUsageTracker {
	if ctx == nil {
		return nil
	}
	tracker, _ := ctx.Value(networkUsageContextKey{}).(*networkUsageTracker)
	return tracker
}

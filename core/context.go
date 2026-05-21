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

// PrepareEngineContext applies request-scoped metadata expected by all engine
// search implementations.
func PrepareEngineContext(ctx context.Context, query Query, engineName string, minimalBrowserProfile bool) context.Context {
	ctx = WithEngine(EnsureContext(ctx), engineName)
	ctx = WithProfileRegion(ctx, profileRegionHint(query))
	if minimalBrowserProfile {
		ctx = WithMinimalBrowserProfile(ctx)
	}
	return WithQueryHash(ctx, QueryHashFromQuery(query))
}

// profileRegionHint picks the strongest market signal for browser fingerprint
// matching. An explicit country-code Region wins (e.g. region=DE → de or en-DE),
// since it's what the user asked the engine to localize to. Engine-native
// numeric region IDs (Yandex lr) are ignored here and we fall back to LangCode.
func profileRegionHint(q Query) string {
	country := CountryFromRegion(q.Region)
	if country == "" {
		return q.LangCode
	}
	if lang := ParseLocale(q.LangCode).Language; lang != "" {
		return lang + "-" + country
	}
	return country
}

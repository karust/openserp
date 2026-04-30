package core

import (
	"context"
	"strings"
)

type profileContextKey string

const profileRegionContextKey profileContextKey = "profile_region"
const forcedProfileIDContextKey profileContextKey = "forced_profile_id"
const minimalProfileContextKey profileContextKey = "minimal_profile"

func WithProfileRegion(ctx context.Context, region string) context.Context {
	region = strings.TrimSpace(region)
	if region == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), profileRegionContextKey, region)
}

func profileRegionFromContext(ctx context.Context) string {
	value, _ := EnsureContext(ctx).Value(profileRegionContextKey).(string)
	return strings.TrimSpace(value)
}

func engineFromContext(ctx context.Context) string {
	value, _ := EnsureContext(ctx).Value(engineContextKey).(string)
	return strings.TrimSpace(value)
}

func WithForcedProfileID(ctx context.Context, profileID string) context.Context {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), forcedProfileIDContextKey, profileID)
}

func forcedProfileIDFromContext(ctx context.Context) string {
	value, _ := EnsureContext(ctx).Value(forcedProfileIDContextKey).(string)
	return strings.TrimSpace(value)
}

func WithMinimalBrowserProfile(ctx context.Context) context.Context {
	return context.WithValue(EnsureContext(ctx), minimalProfileContextKey, true)
}

func minimalBrowserProfileFromContext(ctx context.Context) bool {
	value, _ := EnsureContext(ctx).Value(minimalProfileContextKey).(bool)
	return value
}

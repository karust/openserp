package core

import (
	"context"
	"strings"
)

type profileContextKey string

const profileRegionContextKey profileContextKey = "profile_region"

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

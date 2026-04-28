package core

import (
	"context"
	"strings"
)

type proxyContextKey string

const requestProxyURLContextKey proxyContextKey = "request_proxy_url"
const proxyLaneKeyContextKey proxyContextKey = "proxy_lane_key"

func WithRequestProxyURL(ctx context.Context, proxyURL string) context.Context {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), requestProxyURLContextKey, proxyURL)
}

func requestProxyURLFromContext(ctx context.Context) string {
	value, _ := EnsureContext(ctx).Value(requestProxyURLContextKey).(string)
	return strings.TrimSpace(value)
}

func WithProxyLaneKey(ctx context.Context, key ProxyLaneKey) context.Context {
	key = NormalizeProxyLaneKey(key)
	if key.Empty() {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), proxyLaneKeyContextKey, key)
}

func proxyLaneKeyFromContext(ctx context.Context) ProxyLaneKey {
	value, _ := EnsureContext(ctx).Value(proxyLaneKeyContextKey).(ProxyLaneKey)
	return NormalizeProxyLaneKey(value)
}

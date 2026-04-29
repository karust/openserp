package core

import (
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

func TestBuildTrackingDomainURLPatterns(t *testing.T) {
	patterns := buildTrackingDomainURLPatterns([]string{"google-analytics.com"})
	if len(patterns) != 2 {
		t.Fatalf("expected 2 URL patterns, got %d", len(patterns))
	}
	if patterns[0] != "*://google-analytics.com/*" {
		t.Fatalf("unexpected root pattern: %q", patterns[0])
	}
	if patterns[1] != "*://*.google-analytics.com/*" {
		t.Fatalf("unexpected subdomain pattern: %q", patterns[1])
	}
}

func TestShouldBlockResourceType(t *testing.T) {
	blockedTypes := blockedResourceTypeSet([]proto.NetworkResourceType{
		proto.NetworkResourceTypeImage,
		proto.NetworkResourceTypeFont,
		proto.NetworkResourceTypeMedia,
		proto.NetworkResourceTypeStylesheet,
		proto.NetworkResourceTypeScript,
	})

	tests := []struct {
		resourceType proto.NetworkResourceType
		wantBlocked  bool
	}{
		{resourceType: proto.NetworkResourceTypeImage, wantBlocked: true},
		{resourceType: proto.NetworkResourceTypeFont, wantBlocked: true},
		{resourceType: proto.NetworkResourceTypeMedia, wantBlocked: true},
		{resourceType: proto.NetworkResourceTypeStylesheet, wantBlocked: true},
		{resourceType: proto.NetworkResourceTypeScript, wantBlocked: true},
		{resourceType: proto.NetworkResourceTypeDocument, wantBlocked: false},
		{resourceType: proto.NetworkResourceTypeXHR, wantBlocked: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.resourceType), func(t *testing.T) {
			_, got := blockedTypes[tt.resourceType]
			if got != tt.wantBlocked {
				t.Fatalf("resource type %s: got blocked=%t want %t", tt.resourceType, got, tt.wantBlocked)
			}
		})
	}
}

func TestProxyAuthFetchPatternsOnlyInterceptDocuments(t *testing.T) {
	patterns := proxyAuthFetchPatterns()
	if len(patterns) != 2 {
		t.Fatalf("expected 2 proxy auth fetch patterns, got %d", len(patterns))
	}

	for _, pattern := range patterns {
		if pattern.URLPattern != "http://*/*" && pattern.URLPattern != "https://*/*" {
			t.Fatalf("unexpected proxy auth URL pattern: %q", pattern.URLPattern)
		}
		if pattern.ResourceType != proto.NetworkResourceTypeDocument {
			t.Fatalf("expected document-only proxy auth interception, got %s", pattern.ResourceType)
		}
		if pattern.RequestStage != proto.FetchRequestStageRequest {
			t.Fatalf("expected request-stage proxy auth interception, got %s", pattern.RequestStage)
		}
	}
}

func TestParseBlockedResourceTypes(t *testing.T) {
	got, err := ParseBlockedResourceTypes("image,font,css,js,media")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expectedSet := map[proto.NetworkResourceType]struct{}{
		proto.NetworkResourceTypeImage:      {},
		proto.NetworkResourceTypeFont:       {},
		proto.NetworkResourceTypeStylesheet: {},
		proto.NetworkResourceTypeScript:     {},
		proto.NetworkResourceTypeMedia:      {},
	}
	gotSet := blockedResourceTypeSet(got)
	if len(gotSet) != len(expectedSet) {
		t.Fatalf("expected %d unique resource types, got %d", len(expectedSet), len(gotSet))
	}
	for resourceType := range expectedSet {
		if _, ok := gotSet[resourceType]; !ok {
			t.Fatalf("expected resource type %s to be present", resourceType)
		}
	}
}

func TestParseBlockedResourceTypesInvalid(t *testing.T) {
	if _, err := ParseBlockedResourceTypes("image,unknown"); err == nil {
		t.Fatal("expected invalid token to return error")
	}
}

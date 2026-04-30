//go:build integration
// +build integration

package browser_test

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	browserprofile "github.com/karust/openserp/core/browser"
	"github.com/karust/openserp/testutil"
)

//go:embed profile_surface_test.js
var profileSurfaceScript string

func TestProfileCoherence(t *testing.T) {
	testutil.RequireIntegration(t)

	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>coherence</title></head><body>ok</body></html>`))
	}))
	defer fixture.Close()

	browser, err := core.NewBrowser(core.BrowserOpts{
		IsHeadless: true,
		IsLeakless: false,
		Timeout:    20 * time.Second,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer func() {
		if closeErr := browser.Close(); closeErr != nil {
			t.Fatalf("close browser: %v", closeErr)
		}
	}()

	cases := []struct {
		name   string
		engine string
		region string
	}{
		{
			name:   "windows lane",
			engine: "google",
			region: "ru",
		},
		{
			name:   "mac lane",
			engine: "bing",
			region: "en-US",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := core.WithEngine(context.Background(), tc.engine)
			ctx = core.WithProfileRegion(ctx, tc.region)
			ctx = core.WithBrowserProfileUsage(ctx)

			page, err := browser.Navigate(ctx, fixture.URL)
			if err != nil {
				t.Fatalf("navigate fixture: %v", err)
			}
			defer func() {
				if closeErr := core.ClosePageWithTimeout(context.Background(), page, time.Second); closeErr != nil {
					t.Fatalf("close page: %v", closeErr)
				}
			}()

			got, err := browserProfileSurface(page)
			if err != nil {
				t.Fatalf("collect profile surfaces: %v", err)
			}

			expected := selectedProfileFromContext(t, ctx)
			expected.UserAgent = expectedUserAgentForRuntime(expected.UserAgent, got.UserAgent)
			if got.UserAgent != expected.UserAgent {
				t.Fatalf("navigator.userAgent mismatch:\nexpected: %s\nactual:   %s", expected.UserAgent, got.UserAgent)
			}
			if got.Platform != expected.Platform {
				t.Fatalf("navigator.userAgentData.platform mismatch: expected %q got %q", expected.Platform, got.Platform)
			}
			if got.NavigatorPlatform != expectedNavigatorPlatform(expected.Platform) {
				t.Fatalf("navigator.platform mismatch: expected %q got %q", expectedNavigatorPlatform(expected.Platform), got.NavigatorPlatform)
			}
			if got.Locale != expected.Locale {
				t.Fatalf("Intl locale mismatch: expected %q got %q", expected.Locale, got.Locale)
			}
			if got.Timezone != expected.Timezone {
				t.Fatalf("Intl timezone mismatch: expected %q got %q", expected.Timezone, got.Timezone)
			}
			if len(got.NavigatorLanguages) == 0 {
				t.Fatal("navigator.languages is empty")
			}
			if got.NavigatorLanguages[0] != expected.NavigatorLangs[0] {
				t.Fatalf("navigator.languages[0] mismatch: expected %q got %q", expected.NavigatorLangs[0], got.NavigatorLanguages[0])
			}
			if got.WebdriverType != "undefined" {
				t.Fatalf("navigator.webdriver expected undefined, got %q", got.WebdriverType)
			}
			if got.WebdriverOwnPropPresent {
				t.Fatal("navigator own property 'webdriver' should not be present")
			}
			if got.WorkerUserAgent != got.UserAgent {
				t.Fatalf("worker userAgent mismatch: main %q worker %q", got.UserAgent, got.WorkerUserAgent)
			}
			if got.WorkerPlatform != got.NavigatorPlatform {
				t.Fatalf("worker platform mismatch: main %q worker %q", got.NavigatorPlatform, got.WorkerPlatform)
			}
			if len(got.WorkerNavigatorLangs) == 0 {
				t.Fatal("worker navigator.languages is empty")
			}
			if got.WorkerNavigatorLangs[0] != got.NavigatorLanguages[0] {
				t.Fatalf("worker navigator.languages[0] mismatch: main %q worker %q", got.NavigatorLanguages[0], got.WorkerNavigatorLangs[0])
			}
			if got.WorkerTimezone != got.Timezone {
				t.Fatalf("worker timezone mismatch: main %q worker %q", got.Timezone, got.WorkerTimezone)
			}
			if got.WorkerWebGLVendor != expected.WebGLVendor {
				t.Fatalf("worker WebGL vendor mismatch: expected %q got %q", expected.WebGLVendor, got.WorkerWebGLVendor)
			}
			if got.WorkerWebGLRenderer != expected.WebGLRenderer {
				t.Fatalf("worker WebGL renderer mismatch: expected %q got %q", expected.WebGLRenderer, got.WorkerWebGLRenderer)
			}
			if got.InnerHeight >= got.OuterHeight {
				t.Fatalf("innerHeight should be smaller than outerHeight, got inner=%d outer=%d", got.InnerHeight, got.OuterHeight)
			}
			if got.OuterHeight > got.ScreenAvailHeight {
				t.Fatalf("outerHeight should fit in screen.availHeight, got outer=%d avail=%d", got.OuterHeight, got.ScreenAvailHeight)
			}
			if got.ScreenAvailHeight >= got.ScreenHeight {
				t.Fatalf("screen.availHeight should be smaller than screen.height, got avail=%d screen=%d", got.ScreenAvailHeight, got.ScreenHeight)
			}
		})
	}
}

type profileSurface struct {
	UserAgent               string   `json:"userAgent"`
	Platform                string   `json:"platform"`
	NavigatorPlatform       string   `json:"navigatorPlatform"`
	NavigatorLanguages      []string `json:"navigatorLanguages"`
	Timezone                string   `json:"timezone"`
	Locale                  string   `json:"locale"`
	WebdriverType           string   `json:"webdriverType"`
	WebdriverOwnPropPresent bool     `json:"webdriverOwnPropPresent"`
	WorkerUserAgent         string   `json:"workerUserAgent"`
	WorkerPlatform          string   `json:"workerPlatform"`
	WorkerNavigatorLangs    []string `json:"workerNavigatorLangs"`
	WorkerTimezone          string   `json:"workerTimezone"`
	WorkerWebGLVendor       string   `json:"workerWebGLVendor"`
	WorkerWebGLRenderer     string   `json:"workerWebGLRenderer"`
	InnerHeight             int      `json:"innerHeight"`
	OuterHeight             int      `json:"outerHeight"`
	ScreenHeight            int      `json:"screenHeight"`
	ScreenAvailHeight       int      `json:"screenAvailHeight"`
}

func browserProfileSurface(page *rod.Page) (profileSurface, error) {
	result, err := page.Eval(profileSurfaceScript)
	if err != nil {
		return profileSurface{}, err
	}

	var out profileSurface
	if err := result.Value.Unmarshal(&out); err != nil {
		return profileSurface{}, fmt.Errorf("decode eval payload: %w", err)
	}
	return out, nil
}

func expectedNavigatorPlatform(platform string) string {
	switch platform {
	case "Windows":
		return "Win32"
	case "macOS":
		return "MacIntel"
	default:
		return "Linux x86_64"
	}
}

func selectedProfileFromContext(t *testing.T, ctx context.Context) browserprofile.Profile {
	t.Helper()

	ids := core.BrowserProfileIDsFromContext(ctx)
	if len(ids) == 0 {
		t.Fatal("expected selected browser profile id")
	}
	profile, ok := browserprofile.ProfileByID(ids[0])
	if !ok {
		t.Fatalf("selected browser profile %q not found", ids[0])
	}
	return profile
}

func expectedUserAgentForRuntime(profileUserAgent, runtimeUserAgent string) string {
	runtimeChrome := chromeToken(runtimeUserAgent)
	if runtimeChrome == "" {
		return profileUserAgent
	}
	profileChrome := chromeToken(profileUserAgent)
	if profileChrome == "" {
		return profileUserAgent
	}
	return strings.Replace(profileUserAgent, profileChrome, runtimeChrome, 1)
}

func chromeToken(userAgent string) string {
	const prefix = "Chrome/"
	start := strings.Index(userAgent, prefix)
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(userAgent[start:], ' ')
	if end < 0 {
		return userAgent[start:]
	}
	return userAgent[start : start+end]
}

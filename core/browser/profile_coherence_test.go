//go:build integration
// +build integration

package browser_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
	browserprofile "github.com/karust/openserp/core/browser"
	"github.com/karust/openserp/testutil"
)

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
			expected := browserprofile.SelectProfile(tc.engine, tc.region)
			ctx := core.WithEngine(context.Background(), tc.engine)
			ctx = core.WithProfileRegion(ctx, tc.region)

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
		})
	}
}

type profileSurface struct {
	UserAgent                string   `json:"userAgent"`
	Platform                 string   `json:"platform"`
	NavigatorPlatform        string   `json:"navigatorPlatform"`
	NavigatorLanguages       []string `json:"navigatorLanguages"`
	Timezone                 string   `json:"timezone"`
	Locale                   string   `json:"locale"`
	WebdriverType            string   `json:"webdriverType"`
	WebdriverOwnPropPresent  bool     `json:"webdriverOwnPropPresent"`
	WorkerUserAgent          string   `json:"workerUserAgent"`
	WorkerPlatform           string   `json:"workerPlatform"`
	WorkerNavigatorLangs     []string `json:"workerNavigatorLangs"`
	WorkerTimezone           string   `json:"workerTimezone"`
}

func browserProfileSurface(page *rod.Page) (profileSurface, error) {
	result, err := page.Eval(`async () => {
		const workerData = await new Promise((resolve) => {
			try {
				const source = "self.onmessage=()=>{self.postMessage({userAgent:self.navigator.userAgent||'',platform:self.navigator.platform||'',navigatorLanguages:Array.from(self.navigator.languages||[]),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||''});};";
				const blob = new Blob([source], { type: 'application/javascript' });
				const url = URL.createObjectURL(blob);
				const worker = new Worker(url);
				worker.onmessage = (event) => {
					resolve(event.data || {});
					worker.terminate();
					URL.revokeObjectURL(url);
				};
				worker.onerror = () => {
					resolve({});
					worker.terminate();
					URL.revokeObjectURL(url);
				};
				worker.postMessage('run');
			} catch (_) {
				resolve({});
			}
		});
		return {
			userAgent: navigator.userAgent || "",
			platform: navigator.userAgentData ? (navigator.userAgentData.platform || "") : "",
			navigatorPlatform: navigator.platform || "",
			navigatorLanguages: Array.from(navigator.languages || []),
			timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "",
			locale: Intl.DateTimeFormat().resolvedOptions().locale || "",
			webdriverType: typeof navigator.webdriver,
			webdriverOwnPropPresent: Object.getOwnPropertyNames(navigator).includes('webdriver'),
			workerUserAgent: workerData.userAgent || "",
			workerPlatform: workerData.platform || "",
			workerNavigatorLangs: Array.from(workerData.navigatorLanguages || []),
			workerTimezone: workerData.timezone || "",
		};
	}`)
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

//go:build integration
// +build integration

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/karust/openserp/core/fpcheck"
	"github.com/karust/openserp/core/fpcheck/detectors"
	"github.com/karust/openserp/testutil"
)

const botFingerprintTestsEnv = "OPENSERP_BOT_TESTS"
const botFingerprintArtifactDir = "testdata"

func TestCreateBrowser(t *testing.T) {
	testutil.RequireIntegration(t)

	opts := BrowserOpts{IsHeadless: true, IsLeakless: false}
	browser, err := NewBrowser(opts)
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}

	page, err := browser.Navigate(context.Background(), "about:blank")
	if err != nil {
		t.Fatalf("navigate about:blank: %v", err)
	}
	defer closeTestBrowser(t, browser)
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), page, time.Second); err != nil {
			t.Logf("close page: %v", err)
		}
	}()
}

func TestNavigateUsesIsolatedBrowserContext(t *testing.T) {
	testutil.RequireIntegration(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cookies/set":
			http.SetCookie(w, &http.Cookie{
				Name:  "openserp_session",
				Value: "request-a",
				Path:  "/",
			})
			_, _ = w.Write([]byte("cookie-set"))
		case "/cookies":
			_, _ = w.Write([]byte(r.Header.Get("Cookie")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	opts := BrowserOpts{IsHeadless: true, IsLeakless: false, Timeout: 15 * time.Second}
	browser, err := NewBrowser(opts)
	if err != nil {
		t.Fatalf("failed initializing browser: %s", err)
	}
	defer closeTestBrowser(t, browser)

	pageA, err := browser.Navigate(context.Background(), srv.URL+"/cookies/set")
	if err != nil {
		t.Fatalf("navigate cookie setter: %v", err)
	}
	if err := ClosePageWithTimeout(context.Background(), pageA, time.Second); err != nil {
		t.Fatalf("close setter page: %v", err)
	}

	pageB, err := browser.Navigate(context.Background(), srv.URL+"/cookies")
	if err != nil {
		t.Fatalf("navigate cookie reader: %v", err)
	}
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), pageB, time.Second); err != nil {
			t.Logf("close reader page: %v", err)
		}
	}()

	body, err := pageB.Timeout(5 * time.Second).Element("body")
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	cookieHeader, err := body.Text()
	if err != nil {
		t.Fatalf("extract response text: %v", err)
	}

	if strings.Contains(cookieHeader, "openserp_session=request-a") {
		t.Fatalf("cookie leaked between requests; got header %q", cookieHeader)
	}
}

func TestNavigateReusesCookiesForSameProxyLane(t *testing.T) {
	testutil.RequireIntegration(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cookies/set":
			http.SetCookie(w, &http.Cookie{Name: "openserp_lane", Value: "same-lane", Path: "/"})
			_, _ = w.Write([]byte("cookie-set"))
		case "/cookies":
			_, _ = w.Write([]byte(r.Header.Get("Cookie")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	browser, err := NewBrowser(BrowserOpts{
		IsHeadless:     true,
		IsLeakless:     false,
		Timeout:        15 * time.Second,
		ProxyLaneStore: NewLaneStore(10),
	})
	if err != nil {
		t.Fatalf("failed initializing browser: %s", err)
	}
	defer closeTestBrowser(t, browser)

	laneCtx := WithProxyLaneKey(WithEngine(context.Background(), "google"), ProxyLaneKey{Engine: "google", SessionID: "sid-a"})
	pageA, err := browser.Navigate(laneCtx, srv.URL+"/cookies/set")
	if err != nil {
		t.Fatalf("navigate cookie setter: %v", err)
	}
	if err := ClosePageWithTimeout(context.Background(), pageA, time.Second); err != nil {
		t.Fatalf("close setter page: %v", err)
	}

	pageB, err := browser.Navigate(laneCtx, srv.URL+"/cookies")
	if err != nil {
		t.Fatalf("navigate cookie reader: %v", err)
	}
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), pageB, time.Second); err != nil {
			t.Logf("close reader page: %v", err)
		}
	}()

	body, err := pageB.Timeout(5 * time.Second).Element("body")
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	cookieHeader, err := body.Text()
	if err != nil {
		t.Fatalf("extract response text: %v", err)
	}
	if !strings.Contains(cookieHeader, "openserp_lane=same-lane") {
		t.Fatalf("expected same proxy lane to restore cookie, got %q", cookieHeader)
	}
}

func TestNavigateDoesNotShareCookiesAcrossProxyLanes(t *testing.T) {
	testutil.RequireIntegration(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cookies/set":
			http.SetCookie(w, &http.Cookie{Name: "openserp_lane", Value: "lane-a", Path: "/"})
			_, _ = w.Write([]byte("cookie-set"))
		case "/cookies":
			_, _ = w.Write([]byte(r.Header.Get("Cookie")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	browser, err := NewBrowser(BrowserOpts{
		IsHeadless:     true,
		IsLeakless:     false,
		Timeout:        15 * time.Second,
		ProxyLaneStore: NewLaneStore(10),
	})
	if err != nil {
		t.Fatalf("failed initializing browser: %s", err)
	}
	defer closeTestBrowser(t, browser)

	laneA := WithProxyLaneKey(WithEngine(context.Background(), "google"), ProxyLaneKey{Engine: "google", SessionID: "sid-a"})
	laneB := WithProxyLaneKey(WithEngine(context.Background(), "google"), ProxyLaneKey{Engine: "google", SessionID: "sid-b"})
	pageA, err := browser.Navigate(laneA, srv.URL+"/cookies/set")
	if err != nil {
		t.Fatalf("navigate cookie setter: %v", err)
	}
	if err := ClosePageWithTimeout(context.Background(), pageA, time.Second); err != nil {
		t.Fatalf("close setter page: %v", err)
	}

	pageB, err := browser.Navigate(laneB, srv.URL+"/cookies")
	if err != nil {
		t.Fatalf("navigate cookie reader: %v", err)
	}
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), pageB, time.Second); err != nil {
			t.Logf("close reader page: %v", err)
		}
	}()

	body, err := pageB.Timeout(5 * time.Second).Element("body")
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	cookieHeader, err := body.Text()
	if err != nil {
		t.Fatalf("extract response text: %v", err)
	}
	if strings.Contains(cookieHeader, "openserp_lane=lane-a") {
		t.Fatalf("cookie leaked across proxy lanes; got header %q", cookieHeader)
	}
}

func TestNavigateClassifiesMainDocumentStatus(t *testing.T) {
	testutil.RequireIntegration(t)

	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "blocked", status: http.StatusForbidden, want: ErrBlocked},
		{name: "rate limited", status: http.StatusTooManyRequests, want: ErrRateLimited},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("classified"))
			}))
			defer srv.Close()

			browser, err := NewBrowser(BrowserOpts{IsHeadless: true, IsLeakless: false, Timeout: 15 * time.Second})
			if err != nil {
				t.Fatalf("failed initializing browser: %s", err)
			}
			defer closeTestBrowser(t, browser)

			page, err := browser.Navigate(context.Background(), srv.URL)
			if page != nil {
				_ = ClosePageWithTimeout(context.Background(), page, time.Second)
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestFingerprintDetectors(t *testing.T) {
	testutil.RequireIntegration(t)
	if strings.TrimSpace(os.Getenv(botFingerprintTestsEnv)) != "1" {
		t.Skipf("set %s=1 to run fingerprint tests", botFingerprintTestsEnv)
	}

	reports := make(map[string]fpcheck.Report)
	criticalFailures := make([]string, 0)

	for _, detector := range detectors.All() {
		report := runFingerprintDetector(t, detector)
		key := detector.Name()
		reports[key] = report

		if len(report.Summary.Critical) > 0 {
			for _, critical := range report.Summary.Critical {
				criticalFailures = append(criticalFailures, fmt.Sprintf("%s:%s", key, critical))
			}
		}
	}

	reportPath := filepath.Join(botFingerprintArtifactDir, "fingerprint_report.json")
	if err := writeFingerprintReport(reportPath, reports); err != nil {
		t.Fatalf("write fingerprint report: %v", err)
	}
	absReportPath, err := filepath.Abs(reportPath)
	if err == nil {
		t.Logf("Fingerprint report artifact: %s", absReportPath)
	} else {
		t.Logf("Fingerprint report artifact: core/%s", filepath.ToSlash(reportPath))
	}

	if len(criticalFailures) > 0 {
		t.Fatalf("critical fingerprint detections found: %s", strings.Join(criticalFailures, ", "))
	}
}

func runFingerprintDetector(t *testing.T, detector fpcheck.Detector) fpcheck.Report {
	t.Helper()

	opts := BrowserOpts{
		IsHeadless: true,
		IsLeakless: false,
		Timeout:    20 * time.Second,
	}
	browser, err := NewBrowser(opts)
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer closeTestBrowser(t, browser)

	report, err := fpcheck.Run(context.Background(), browser, detector, botFingerprintArtifactDir)
	if err != nil {
		t.Fatalf("run detector %s: %v", detector.Name(), err)
	}

	t.Logf(
		"Fingerprint %s: passed=%d failed=%d critical=%d",
		detector.Name(),
		report.Summary.Passed,
		report.Summary.Failed,
		len(report.Summary.Critical),
	)
	return report
}

func writeFingerprintReport(path string, reports map[string]fpcheck.Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}

	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report file %s: %w", path, err)
	}
	return nil
}

func closeTestBrowser(t *testing.T, browser *Browser) {
	t.Helper()
	if browser == nil {
		return
	}
	if err := browser.Close(); err != nil {
		t.Logf("close browser: %v", err)
	}
}

//go:build integration
// +build integration

package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/karust/openserp/testutil"
)

// TestPerContextProxyIsolation verifies the supported per-request proxy path:
// a shared Chrome (launched without a process-level proxy) routes each request
// through the unauthenticated proxy from its context, with no cross-context
// leakage between concurrent navigations.
//
// Authenticated per-request proxies are intentionally NOT supported on a
// shared browser: Chrome's Fetch-based proxy auth is browser-global, so
// concurrent contexts with different credentials race and fail with
// ERR_INVALID_AUTH_CREDENTIALS. The server routes authenticated proxies to a
// dedicated Chrome process per auth identity instead (see browserPool in
// cmd/serve.go).
func TestPerContextProxyIsolation(t *testing.T) {
	testutil.RequireIntegration(t)

	newProxy := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, "<html><body>%s</body></html>", name)
		}))
	}

	proxyA := newProxy("proxy-a")
	defer proxyA.Close()
	proxyB := newProxy("proxy-b")
	defer proxyB.Close()

	browser, err := NewBrowser(BrowserOpts{IsHeadless: true, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer closeTestBrowser(t, browser)

	run := func(proxyURL, want string) error {
		ctx := WithRequestProxyURL(context.Background(), proxyURL)
		page, err := browser.Navigate(ctx, "http://per-context-proxy.invalid/")
		if err != nil {
			return err
		}
		defer func() {
			_ = ClosePageWithTimeout(context.Background(), page, time.Second)
		}()
		body, err := page.Timeout(5 * time.Second).Element("body")
		if err != nil {
			return err
		}
		text, err := body.Text()
		if err != nil {
			return err
		}
		if !strings.Contains(text, want) {
			return fmt.Errorf("expected response from %s, got %q", want, text)
		}
		return nil
	}

	errCh := make(chan error, 2)
	go func() { errCh <- run(proxyA.URL, "proxy-a") }()
	go func() { errCh <- run(proxyB.URL, "proxy-b") }()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("proxied navigation failed: %v", err)
		}
	}
}

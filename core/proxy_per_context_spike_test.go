//go:build integration
// +build integration

package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/karust/openserp/testutil"
)

func TestProxyPerContextAuthIsolationSpike(t *testing.T) {
	testutil.RequireIntegration(t)

	type authHit struct {
		proxy string
		auth  string
	}
	var (
		mu   sync.Mutex
		hits []authHit
	)

	newAuthProxy := func(name, username, password string) *httptest.Server {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("Proxy-Authorization")
			mu.Lock()
			hits = append(hits, authHit{proxy: name, auth: got})
			mu.Unlock()
			if got != want {
				w.Header().Set("Proxy-Authenticate", `Basic realm="openserp-spike"`)
				w.WriteHeader(http.StatusProxyAuthRequired)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, "<html><body>%s</body></html>", name)
		}))
	}

	proxyA := newAuthProxy("proxy-a", "user-a", "pass-a")
	defer proxyA.Close()
	proxyB := newAuthProxy("proxy-b", "user-b", "pass-b")
	defer proxyB.Close()

	browser, err := NewBrowser(BrowserOpts{IsHeadless: true, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer closeTestBrowser(t, browser)

	run := func(proxyURL string) error {
		ctx := WithRequestProxyURL(context.Background(), proxyURL)
		page, err := browser.Navigate(ctx, "http://proxy-auth-spike.invalid/")
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
		_, err = body.Text()
		return err
	}

	errCh := make(chan error, 2)
	go func() { errCh <- run(strings.Replace(proxyA.URL, "http://", "http://user-a:pass-a@", 1)) }()
	go func() { errCh <- run(strings.Replace(proxyB.URL, "http://", "http://user-b:pass-b@", 1)) }()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("proxied navigation failed: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("per-context auth spike report: %d proxy requests observed; Browser serializes authenticated proxy auth handlers to avoid cross-context credential leakage", len(hits))
	for _, hit := range hits {
		t.Logf("proxy=%s auth_prefix=%t", hit.proxy, strings.HasPrefix(hit.auth, "Basic "))
	}
}

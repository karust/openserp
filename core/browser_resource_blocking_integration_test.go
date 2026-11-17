//go:build integration
// +build integration

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/karust/openserp/testutil"
)

func TestTrackerBlockingEmitsBlockedNetworkEvent(t *testing.T) {
	testutil.RequireIntegration(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<img src="https://improving.duckduckgo.com/pixel.png">`))
	}))
	defer server.Close()

	browser, err := NewBrowser(BrowserOpts{IsHeadless: true, Timeout: 15 * time.Second, BlockTrackers: true})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer func() { _ = browser.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	page, err := browser.Navigate(ctx, "about:blank")
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	defer DeferClosePage(ctx, page, browser)()

	eventCtx, stopEvents := context.WithCancel(ctx)
	defer stopEvents()
	var mu sync.Mutex
	requests := map[proto.NetworkRequestID]string{}
	blocked := make(chan proto.NetworkLoadingFailed, 1)
	wait := page.Context(eventCtx).EachEvent(
		func(e *proto.NetworkRequestWillBeSent) bool {
			mu.Lock()
			requests[e.RequestID] = e.Request.URL
			mu.Unlock()
			return false
		},
		func(e *proto.NetworkLoadingFailed) bool {
			mu.Lock()
			requestURL := requests[e.RequestID]
			mu.Unlock()
			if strings.Contains(requestURL, "improving.duckduckgo.com") {
				select {
				case blocked <- *e:
				default:
				}
			}
			return false
		},
	)
	go wait()

	if err := page.Navigate(server.URL); err != nil {
		t.Fatalf("navigate fixture: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	select {
	case failure := <-blocked:
		if failure.BlockedReason != proto.NetworkBlockedReasonInspector {
			t.Fatalf("tracker request had blocked reason %q, want %q (error: %q)", failure.BlockedReason, proto.NetworkBlockedReasonInspector, failure.ErrorText)
		}
	case <-ctx.Done():
		t.Fatal("no blocked network event for improving.duckduckgo.com")
	}
}

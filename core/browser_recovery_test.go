package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserConnectionRejectsCanceledAndClosed(t *testing.T) {
	browser := &Browser{conn: &browserConnection{addr: "ws://unused", closed: true}}
	if browser.IsInitialized() {
		t.Fatal("closed browser is initialized")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := browser.ensureConnectedBrowser(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled connect: %v", err)
	}
	if _, err := browser.ensureConnectedBrowser(context.Background(), nil); err == nil {
		t.Fatal("closed browser must not reconnect")
	}
}

func TestBrowserRelaunchesOnlyAfterDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name       string
		disconnect bool
		canceled   bool
	}{
		{name: "disconnected", disconnect: true},
		{name: "bad handshake"},
		{name: "canceled handshake", disconnect: true, canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tc.disconnect {
					http.NotFound(w, r)
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				if tc.canceled {
					cancel()
				}
				conn.Close()
			}))
			defer server.Close()
			// An invalid binary path detects relaunch without starting Chrome.
			browser := &Browser{
				BrowserOpts: BrowserOpts{BrowserPath: t.TempDir()},
				conn:        &browserConnection{addr: "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/x"},
			}
			_, err := browser.ensureConnectedBrowser(ctx, nil)
			if err == nil {
				t.Fatal("unusable endpoint connected")
			}
			if tc.canceled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled connect: %v", err)
				}
				return
			}
			if relaunched := strings.Contains(err.Error(), "invalid browser_path"); relaunched != tc.disconnect {
				t.Fatalf("relaunched=%v, error=%v", relaunched, err)
			}
		})
	}
}

func TestBrowserDisconnectErrors(t *testing.T) {
	for _, message := range []string{
		"dial tcp: connection refused",
		"connectex: target machine actively refused it",
		"read tcp: connection reset by peer",
		"wsarecv: An existing connection was forcibly closed by the remote host",
		"EOF",
	} {
		if !isBrowserClosedError(errors.New(message)) {
			t.Errorf("missed browser disconnect: %s", message)
		}
	}
	if isBrowserClosedError(context.DeadlineExceeded) {
		t.Fatal("request timeout must not trigger relaunch")
	}
}

//go:build integration

package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/karust/openserp/testutil"
)

func TestBrowserRecoversAfterProcessDeath(t *testing.T) {
	testutil.RequireIntegration(t)
	for _, insecure := range []bool{false, true} {
		t.Run("insecure="+strconv.FormatBool(insecure), func(t *testing.T) {
			testBrowserRecovery(t, insecure)
		})
	}
}

func testBrowserRecovery(t *testing.T, insecure bool) {
	t.Helper()
	browser, err := NewBrowser(BrowserOpts{IsHeadless: true, Timeout: 5 * time.Second, Insecure: insecure})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := browser.Close(); err != nil {
			t.Error(err)
		}
	})
	copyBeforeRecovery := *browser
	// A self-signed page proves insecure mode survives the relaunch.
	target := "about:blank"
	if insecure {
		server := httptest.NewTLSServer(http.NotFoundHandler())
		t.Cleanup(server.Close)
		target = server.URL
	}

	for _, stage := range []string{"before first connect", "recent connection", "health ping"} {
		t.Run(stage, func(t *testing.T) {
			oldLauncher := browser.conn.launcher
			oldLauncher.Kill()
			if stage == "health ping" {
				browser.conn.lastOK = time.Time{}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			canceled, stop := context.WithCancel(ctx)
			stop()
			if _, err := browser.Navigate(canceled, "about:blank"); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled navigate: %v", err)
			}
			if browser.conn.launcher != oldLauncher {
				t.Fatal("canceled request relaunched Chrome")
			}

			var workers sync.WaitGroup
			for i := 0; i < 3; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					page, err := copyBeforeRecovery.Navigate(ctx, target)
					if err != nil {
						t.Error(err)
						return
					}
					if err := browser.ClosePage(ctx, page, time.Second); err != nil {
						t.Error(err)
					}
				}()
			}
			workers.Wait()
			if browser.conn.launcher == oldLauncher {
				t.Fatal("dead process was not replaced")
			}
		})
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.conn.exited:
	default:
		t.Fatal("Close returned before profile cleanup finished")
	}
	closedLauncher := browser.conn.launcher
	if _, err := copyBeforeRecovery.Navigate(context.Background(), "about:blank"); err == nil {
		t.Fatal("closed browser was reopened")
	}
	if browser.conn.launcher != closedLauncher {
		t.Fatal("shutdown relaunched Chrome")
	}
}

//go:build integration

package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestGoogleWaitsForHydratedResults(t *testing.T) {
	testutil.RequireIntegration(t)
	engine := New(*ithelper.CreateBrowser(t), core.SearchEngineOptions{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<div data-hveid="shell" data-ved="pending"></div>`)
	}))
	defer server.Close()
	for _, class := range []string{"tF2Cxc", "other-layout"} {
		t.Run(class, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			page, err := engine.Navigate(ctx, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer core.DeferClosePage(ctx, page, &engine.Browser)()
			_, err = page.Eval(`(cls) => {
				if (cls === "other-layout") document.body.innerHTML = '<div class="tF2Cxc"></div>';
				setTimeout(() => {
					document.body.innerHTML += '<div class="' + cls + '" data-hveid="1" data-ved="ready"><a href="https://example.org/"><h3>Loaded result</h3></a></div>';
				}, 300);
			}`, class)
			if err != nil {
				t.Fatal(err)
			}
			results, _, err := engine.waitForResults(ctx, page)
			if err != nil || len(results) != 1 {
				t.Fatalf("hydrated results=%d, error=%v", len(results), err)
			}
			title, err := results[0].Element(Selectors.Title)
			if err != nil || core.ElementText(title) != "Loaded result" {
				t.Fatalf("returned an unfinished wrapper, error=%v", err)
			}
		})
	}
	t.Run("no results", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		page, err := engine.Navigate(ctx, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer core.DeferClosePage(ctx, page, &engine.Browser)()
		_, err = page.Eval(`() => {
			document.body.innerHTML += '<div id="botstuff"><div class="mnr-c">Your search - <em>q</em> - did not match any documents.</div></div>';
		}`)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		results, _, err := engine.waitForResults(ctx, page)
		if err != nil || len(results) != 0 || time.Since(started) > time.Second {
			t.Fatalf("no-results page: results=%d, error=%v, waited %s", len(results), err, time.Since(started))
		}
	})
	t.Run("notice above results", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		page, err := engine.Navigate(ctx, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer core.DeferClosePage(ctx, page, &engine.Browser)()
		_, err = page.Eval(`() => {
			document.body.innerHTML = '<div id="topstuff"><div class="mnr-c">Not many great matches</div></div>';
			setTimeout(() => {
				document.body.innerHTML += '<div class="tF2Cxc" data-hveid="1" data-ved="ready"><a href="https://example.org/"><h3>Loaded result</h3></a></div>';
			}, 300);
		}`)
		if err != nil {
			t.Fatal(err)
		}
		results, _, err := engine.waitForResults(ctx, page)
		if err != nil || len(results) != 1 {
			t.Fatalf("notice page: results=%d, error=%v", len(results), err)
		}
	})
}

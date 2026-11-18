//go:build integration
// +build integration

package bing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchBing(t *testing.T) {
	ithelper.RunEngineTests(t, func(b *core.Browser) core.SearchEngine {
		return New(*b, ithelper.EngineOptions())
	})
}

// TestBingSearchForm runs typeInTab against a local page: the stale text is
// replaced, the session survives the submit, and a form that doesn't navigate
// fails instead of leaving us on the homepage.
func TestBingSearchForm(t *testing.T) {
	testutil.RequireIntegration(t)
	const text = `嫦娥六号 2024年月背采样返回 & "moon" site:example.org filetype:pdf`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/search" {
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "kept" {
				t.Error("form navigation lost the session")
			}
			_, _ = fmt.Fprint(w, "results")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "kept"})
		_, _ = fmt.Fprint(w, `<form action="/search"><textarea id="sb_form_q" name="q">stale query</textarea></form>`)
	}))
	defer server.Close()

	engine := New(*ithelper.CreateBrowser(t), core.SearchEngineOptions{})
	engine.Timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := engine.Navigate(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer core.DeferClosePage(ctx, page, &engine.Browser)()

	if err := engine.typeInTab(page, text); err != nil {
		t.Fatal(err)
	}
	info, err := page.Info()
	if err != nil {
		t.Fatal(err)
	}
	if u, err := url.Parse(info.URL); err != nil || u.Query().Get("q") != text {
		t.Fatalf("submitted URL=%s, error=%v", info.URL, err)
	}
	if err := engine.loadInTab(page, server.URL+"/search?first=11"); err != nil {
		t.Fatalf("same-tab paging: %v", err)
	}

	if _, err := page.Eval(`() => document.body.innerHTML = '<form onsubmit="event.preventDefault()"><textarea id="sb_form_q" name="q"></textarea></form>'`); err != nil {
		t.Fatal(err)
	}
	engine.Timeout = 200 * time.Millisecond
	if err := engine.typeInTab(page, text); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submission without navigation: %v", err)
	}
}

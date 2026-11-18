package google

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

func TestRawSearchDeduplicatesResolvedURLsAndPreservesRanks(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = io.WriteString(w, `<div class="tF2Cxc"><a href="/goto?url=first"><h3>First</h3></a></div>
<div class="tF2Cxc"><a href="/goto?url=second"><h3>Duplicate</h3></a></div>
<div class="tF2Cxc"><a href="https://example.org/other"><h3>Other</h3></a></div>`)
		case "/goto":
			w.Header().Set("Location", "https://example.org/article")
			w.WriteHeader(http.StatusFound)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	upstream.Config.SetKeepAlivesEnabled(false)
	defer upstream.Close()

	// Tunnel Google requests to the local fixture server.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dst, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer dst.Close()
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(dst, buffered) }()
		_, _ = io.Copy(conn, dst)
	}))
	defer proxy.Close()

	for _, start := range []int{0, 10} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			results, err := Search(context.Background(), core.Query{
				Text: "test", Start: start, ProxyURL: proxy.URL, Insecure: true, ResolveURLs: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 2 || results[0].URL != "https://example.org/article" || results[1].URL != "https://example.org/other" {
				t.Fatalf("expected distinct destinations, got %+v", results)
			}
			for i, rank := range []int{start + 1, start + 3} {
				if results[i].Rank != rank || results[i].AbsoluteRank != rank {
					t.Errorf("result %d ranks = %d/%d, want %d", i, results[i].Rank, results[i].AbsoluteRank, rank)
				}
			}
		})
	}

	t.Run("limit", func(t *testing.T) {
		results, err := Search(context.Background(), core.Query{
			Text: "test", Limit: 1, ProxyURL: proxy.URL, Insecure: true, ResolveURLs: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].URL != "https://example.org/article" {
			t.Fatalf("limit=1 should keep only the first organic result, got %+v", results)
		}
	})
}

func TestGoogleParseHTMLFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		fixture        string
		minResultCount int
		maxResultCount int
		wantZero       bool
		wantErr        error
	}{
		{
			name:           "search results",
			fixture:        "search_results.html",
			minResultCount: 1,
			maxResultCount: 20,
		},
		{
			name:     "no results",
			fixture:  "search_no_results.html",
			wantZero: true,
		},
		{
			name:    "captcha page",
			fixture: "search_captcha.html",
			wantErr: core.ErrCaptcha,
		},
		{
			name:    "new captcha page",
			fixture: "search_captcha_new.html",
			wantErr: core.ErrCaptcha,
		},
		{
			name:    "soft block page",
			fixture: "search_soft_block.html",
			wantErr: core.ErrBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := ParseHTML(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v for %s, got %v", tt.wantErr, tt.fixture, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHTML() error = %v", err)
			}

			if tt.wantZero {
				if len(results) != 0 {
					t.Fatalf("expected zero results for %s, got %d", tt.fixture, len(results))
				}
				return
			}

			if len(results) < tt.minResultCount || len(results) > tt.maxResultCount {
				t.Fatalf(
					"unexpected result count for %s: got %d, want range [%d,%d]",
					tt.fixture, len(results), tt.minResultCount, tt.maxResultCount,
				)
			}

			testutil.AssertSequentialRanks(t, results)
			testutil.AssertFirstResultFilled(t, results)
		})
	}
}

func TestGoogleParseHTMLEmptyHTML(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(testutil.ResponseFromString("").Body)
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestGoogleClassifyRawHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    error
	}{
		{name: "no results", fixture: "search_no_results.html", want: core.ErrEmptyResult},
		{name: "captcha page", fixture: "search_captcha.html", want: core.ErrCaptcha},
		{name: "new captcha page", fixture: "search_captcha_new.html", want: core.ErrCaptcha},
		{name: "sorry challenge page", fixture: "search_sorry.html", want: core.ErrCaptcha},
		{name: "soft block page", fixture: "search_soft_block.html", want: core.ErrBlocked},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc, err := goquery.NewDocumentFromReader(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if err != nil {
				t.Fatalf("read fixture body: %v", err)
			}
			err = classifyGoogleDocument(doc)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v for %s, got %v", tt.want, tt.fixture, err)
			}
		})
	}
}

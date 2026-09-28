package core_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/extract"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestRenderExtractFinalURL(t *testing.T) {
	testutil.RequireIntegration(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old/page" {
			http.Redirect(w, r, "/docs/page.html", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><base href="./"></head><body><a href="guide.html">Guide</a></body></html>`))
	}))
	defer target.Close()
	browser := ithelper.CreateBrowser(t)
	extractor := extract.Extractor{
		Cfg: extract.DefaultConfig(),
		RenderedFetch: func(ctx context.Context, req extract.ExtractRequest) (*extract.FetchResponse, error) {
			resp, err := core.RenderExtractHTML(ctx, browser, req)
			if err == nil && resp.FinalURL != target.URL+"/docs/page.html" {
				t.Errorf("final URL = %q", resp.FinalURL)
			}
			return resp, err
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := extractor.Extract(ctx, extract.ExtractRequest{
		URL: target.URL + "/old/page", Mode: extract.ModeRendered, FullPage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != target.URL+"/old/page" {
		t.Errorf("request URL changed: %s", result.URL)
	}
	if len(result.Links) != 1 || result.Links[0].URL != target.URL+"/docs/guide.html" {
		t.Errorf("links = %#v", result.Links)
	}
	if !strings.Contains(result.Markdown, "("+target.URL+"/docs/guide.html)") {
		t.Errorf("markdown = %q", result.Markdown)
	}
}

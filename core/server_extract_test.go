package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	extractpkg "github.com/karust/openserp/extract"
)

func TestRawExtractFetchFinalURL(t *testing.T) {
	const destination = "http://1.1.1.1/docs/page.html"
	// The local proxy serves both public URLs; no external request is made.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old/page" {
			http.Redirect(w, r, destination, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`<html><head><base href="./"><link rel="canonical" href="canonical.html"></head>
<body><a href="guide.html">Guide</a></body></html>`))
	}))
	defer proxy.Close()
	cfg := extractpkg.DefaultConfig()
	for _, target := range []string{"http://8.8.8.8/old/page", destination} {
		t.Run(target, func(t *testing.T) {
			fetch := func(ctx context.Context, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
				resp, err := RawExtractFetch(ctx, req, cfg, false)
				if err == nil && resp.FinalURL != destination {
					t.Errorf("final URL = %q", resp.FinalURL)
				}
				return resp, err
			}
			extractor := extractpkg.Extractor{RawFetch: fetch, Cfg: cfg}
			result, err := extractor.Extract(context.Background(), extractpkg.ExtractRequest{
				URL: target, Mode: extractpkg.ModeFast, ProxyURL: proxy.URL, FullPage: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.URL != target || result.Canonical != "http://1.1.1.1/docs/canonical.html" {
				t.Errorf("URL = %q, canonical = %q", result.URL, result.Canonical)
			}
			if len(result.Links) != 1 || result.Links[0].URL != "http://1.1.1.1/docs/guide.html" {
				t.Errorf("links = %#v", result.Links)
			}
			if !strings.Contains(result.Markdown, "(http://1.1.1.1/docs/guide.html)") {
				t.Errorf("markdown = %q", result.Markdown)
			}
		})
	}
}

func TestEnrichEnvelopeWithExtractionRetriesThinAndFailedCandidates(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/thin":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><head><title>tripadvisor.com</title></head><body>tripadvisor.com</body></html>`))
		case "/blocked":
			w.WriteHeader(http.StatusBadGateway)
		case "/useful":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body><article><h1>Useful page</h1><p>This useful page has enough body text to count as extracted content and should be selected after earlier candidates fail.</p></article></body></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer target.Close()

	opts := DefaultServerOptions()
	opts.Extract = extractpkg.Config{
		Enabled:              true,
		DefaultMode:          string(extractpkg.ModeFast),
		Timeout:              time.Second,
		MaxBytes:             256 * 1024,
		MaxConcurrent:        2,
		AllowPrivateNetworks: true,
	}
	s := &Server{opts: opts}
	env := &Envelope{Results: []Result{
		{URL: target.URL + "/thin"},
		{URL: target.URL + "/blocked"},
		{URL: target.URL + "/useful"},
	}}
	q := Query{Extract: true, ExtractTop: 1, ExtractMode: string(extractpkg.ModeFast)}

	s.enrichEnvelopeWithExtraction(context.Background(), env, q, "json")

	if env.Results[0].Extracted == nil || env.Results[0].Extracted.Error != "empty extracted content" {
		t.Fatalf("first candidate extracted = %+v, want empty-content error", env.Results[0].Extracted)
	}
	if env.Results[1].Extracted == nil || env.Results[1].Extracted.Error == "" {
		t.Fatalf("second candidate extracted = %+v, want failure error", env.Results[1].Extracted)
	}
	if env.Results[2].Extracted == nil || env.Results[2].Extracted.Error != "" {
		t.Fatalf("third candidate extracted = %+v, want successful retry", env.Results[2].Extracted)
	}
	if !strings.Contains(env.Results[2].Extracted.Content, "Useful page") {
		t.Fatalf("third candidate content = %q", env.Results[2].Extracted.Content)
	}
}

func TestExtractRejectsLinkLocalAddressByDefault(t *testing.T) {
	opts := DefaultServerOptions()
	opts.Extract = extractpkg.DefaultConfig()
	s := NewServerWithOptions("127.0.0.1", 0, opts)

	req, err := http.NewRequest(http.MethodPost, "/extract", strings.NewReader(`{"url":"http://169.254.169.254/latest/meta-data/"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestExtractRejectsLocalhostByDefault(t *testing.T) {
	opts := DefaultServerOptions()
	opts.Extract = extractpkg.DefaultConfig()
	s := NewServerWithOptions("127.0.0.1", 0, opts)

	req, err := http.NewRequest(http.MethodGet, "/extract?url=http://localhost/private", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestValidateExtractTargetURLNormalizesBarePublicIP(t *testing.T) {
	if err := validateExtractTargetURL(context.Background(), "1.1.1.1", false); err != nil {
		t.Fatalf("expected bare public IP target to validate after scheme normalization: %v", err)
	}
}

func TestBatchExtractSingleURL(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><article><h1>Test Page</h1><p>This is a test page with enough content to pass the minimum runes threshold for extraction in batch mode.</p></article></body></html>`))
	}))
	defer target.Close()

	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, fmt.Sprintf(`{"urls":["%s"]}`, target.URL))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	items := decodeBatchItems(t, resp)
	if len(items) != 1 {
		t.Fatalf("items count = %d, want 1", len(items))
	}
	if items[0].PageContent == "" {
		t.Fatal("expected non-empty page_content")
	}
	if items[0].Metadata["title"] != "Test Page" {
		t.Fatalf("metadata = %v, want title 'Test Page'", items[0].Metadata)
	}
}

func TestBatchExtractHandlesMultipleURLs(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><article><h1>Multi</h1><p>Page with sufficient content for batch extraction test that verifies concurrent processing works correctly.</p></article></body></html>`))
	}))
	defer target.Close()

	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, fmt.Sprintf(`{"urls":["%s/1","%s/2","%s/3"]}`, target.URL, target.URL, target.URL))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if items := decodeBatchItems(t, resp); len(items) != 3 {
		t.Fatalf("items count = %d, want 3", len(items))
	}
}

func batchExtractTestServer(allowPrivate bool) *Server {
	opts := DefaultServerOptions()
	opts.Extract = extractpkg.Config{
		Enabled:              true,
		DefaultMode:          string(extractpkg.ModeFast),
		Timeout:              time.Second,
		MaxBytes:             256 * 1024,
		MaxConcurrent:        2,
		AllowPrivateNetworks: allowPrivate,
	}
	return NewServerWithOptions("127.0.0.1", 0, opts)
}

func postBatchExtract(t *testing.T, s *Server, body string, header ...[2]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/extract/batch", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, h := range header {
		req.Header.Set(h[0], h[1])
	}
	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeBatchItems(t *testing.T, resp *http.Response) []batchExtractItem {
	t.Helper()
	var items []batchExtractItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return items
}

func TestBatchExtractReturnsPerURLErrors(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><article><h1>Good page</h1><p>Enough body text to extract something meaningful from this page in batch mode.</p></article></body></html>`))
	}))
	defer target.Close()

	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, fmt.Sprintf(`{"urls":["%s","ftp://example.com/x"]}`, target.URL))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	items := decodeBatchItems(t, resp)
	if len(items) != 2 {
		t.Fatalf("items count = %d, want 2", len(items))
	}
	if !strings.Contains(items[0].PageContent, "Good page") || items[0].Metadata["error"] != "" {
		t.Fatalf("first item = %+v, want successful extraction", items[0])
	}
	if items[1].PageContent != "" || items[1].Metadata["error"] == "" {
		t.Fatalf("second item = %+v, want error item", items[1])
	}
	if items[1].Metadata["source"] != "ftp://example.com/x" {
		t.Fatalf("second item source = %q", items[1].Metadata["source"])
	}
}

func TestBatchExtractKeepsPrivateNetworkGuardPerItem(t *testing.T) {
	s := batchExtractTestServer(false)
	resp := postBatchExtract(t, s, `{"urls":["http://169.254.169.254/latest/meta-data/"]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	items := decodeBatchItems(t, resp)
	if len(items) != 1 {
		t.Fatalf("items count = %d, want 1", len(items))
	}
	if items[0].PageContent != "" || !strings.Contains(items[0].Metadata["error"], "not allowed") {
		t.Fatalf("item = %+v, want blocked-target error", items[0])
	}
}

func TestBatchExtractDedupesURLs(t *testing.T) {
	var hits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`<html><body><article><h1>Dedup</h1><p>Some body text long enough for the extractor to produce markdown content here.</p></article></body></html>`))
	}))
	defer target.Close()

	s := batchExtractTestServer(true)
	body := fmt.Sprintf(`{"urls":["%s/a","%s/a","%s/b"]}`, target.URL, target.URL, target.URL)
	resp := postBatchExtract(t, s, body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if items := decodeBatchItems(t, resp); len(items) != 2 {
		t.Fatalf("items count = %d, want 2", len(items))
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("target hits = %d, want 2", got)
	}
}

func TestBatchExtractDisabledReturns404(t *testing.T) {
	opts := DefaultServerOptions()
	opts.Extract = extractpkg.DefaultConfig()
	opts.Extract.Enabled = false
	s := NewServerWithOptions("127.0.0.1", 0, opts)

	resp := postBatchExtract(t, s, `{"urls":["https://example.com"]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestBatchExtractRejectsInvalidMode(t *testing.T) {
	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, `{"urls":["https://example.com"],"mode":"turbo"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBatchExtractRejectsBadProxyHeader(t *testing.T) {
	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, `{"urls":["https://example.com"]}`, [2]string{"X-Proxy-URL", "not-a-proxy"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBatchExtractBodyLangReachesFetch(t *testing.T) {
	var acceptLanguage string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptLanguage = r.Header.Get("Accept-Language")
		_, _ = w.Write([]byte(`<html><body><article><h1>Lang</h1><p>Body text long enough to satisfy extraction thresholds for this test case.</p></article></body></html>`))
	}))
	defer target.Close()

	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, fmt.Sprintf(`{"urls":["%s"],"lang":"de"}`, target.URL))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.HasPrefix(acceptLanguage, "de") {
		t.Fatalf("Accept-Language = %q, want de-prefixed", acceptLanguage)
	}
}

func TestBatchExtractRejectsEmptyURLs(t *testing.T) {
	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, `{"urls":[]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBatchExtractRejectsURLsOverLimit(t *testing.T) {
	// 21 URLs, limit is 20
	urls := make([]string, 21)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://example.com/%d", i)
	}
	body, _ := json.Marshal(map[string][]string{"urls": urls})

	s := batchExtractTestServer(true)
	resp := postBatchExtract(t, s, string(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

package extract

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// staticRaw returns a RawFetcher that always serves the same HTML with HTTP 200.
func staticRaw(html string) RawFetcher {
	return func(context.Context, ExtractRequest) (*FetchResponse, error) {
		return &FetchResponse{StatusCode: 200, Body: []byte(html)}, nil
	}
}

// runExtract builds an Extractor from the given fetchers and runs Extract,
// failing the test on error.
func runExtract(t *testing.T, raw RawFetcher, rendered RenderedFetcher, req ExtractRequest) *ExtractResult {
	t.Helper()
	extractor := Extractor{RawFetch: raw, RenderedFetch: rendered, Cfg: DefaultConfig()}
	result, err := extractor.Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	return result
}

func TestExtractFastStaticArticle(t *testing.T) {
	html := `<!doctype html><html lang="en"><head>
<title>Static Article</title>
<meta name="description" content="A focused article">
</head><body>
<article>
<h1>Static Article</h1>
<p>This article has enough body text to be treated as useful extracted content. It explains how OpenSERP extracts target pages into markdown for grounding workflows, with clear paragraphs and useful links for downstream automation. The text is intentionally longer than the quality threshold.</p>
<a href="/docs">Docs</a>
</article>
</body></html>`
	result := runExtract(t, staticRaw(html), nil, ExtractRequest{URL: "https://example.com/post", Mode: ModeFast})
	if result.Title != "Static Article" {
		t.Fatalf("title = %q", result.Title)
	}
	if !strings.Contains(result.Markdown, "OpenSERP extracts target pages") {
		t.Fatalf("markdown missing article body: %q", result.Markdown)
	}
	if len(result.Links) != 1 || result.Links[0].URL != "https://example.com/docs" {
		t.Fatalf("links = %#v", result.Links)
	}
	if result.Meta.ModeUsed != string(ModeFast) {
		t.Fatalf("mode_used = %q", result.Meta.ModeUsed)
	}
}

func TestExtractAutoEscalatesThinShell(t *testing.T) {
	raw := staticRaw(`<!doctype html><div id="root"></div><script src="/app.js"></script>`)
	rendered := func(context.Context, ExtractRequest) (*FetchResponse, error) {
		return &FetchResponse{StatusCode: 200, Body: []byte(`<!doctype html><article><h1>Rendered</h1><p>This rendered page contains enough meaningful article text after JavaScript execution to pass the extraction threshold and avoid returning an empty shell to callers.</p></article>`)}, nil
	}
	result := runExtract(t, raw, rendered, ExtractRequest{URL: "https://example.com/app", Mode: ModeAuto})
	if result.Meta.ModeUsed != string(ModeRendered) {
		t.Fatalf("mode_used = %q", result.Meta.ModeUsed)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"kamaloff.ru":            "https://kamaloff.ru",
		"example.com/path?q=1":   "https://example.com/path?q=1",
		"http://example.com":     "http://example.com",
		"https://example.com":    "https://example.com",
		"//example.com":          "//example.com",
		"":                       "",
		"socks5h://127.0.0.1:80": "socks5h://127.0.0.1:80",
	}
	for in, want := range cases {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractBareHostGetsScheme(t *testing.T) {
	var fetched string
	raw := func(_ context.Context, req ExtractRequest) (*FetchResponse, error) {
		fetched = req.URL
		return &FetchResponse{StatusCode: 200, Body: []byte(`<!doctype html><html><head><title>Home</title></head><body><article><p>` +
			strings.Repeat("Bare hostnames must resolve to https and extract normally. ", 5) + `</p></article></body></html>`)}, nil
	}
	result := runExtract(t, raw, nil, ExtractRequest{URL: "kamaloff.ru", Mode: ModeFast})
	if fetched != "https://kamaloff.ru" {
		t.Fatalf("fetched %q, want https://kamaloff.ru", fetched)
	}
	if result.Title != "Home" {
		t.Fatalf("title = %q", result.Title)
	}
}

func TestExtractAutoMinRunesForcesEscalation(t *testing.T) {
	// Raw body clears the default floor (kept as-is), but a high MinRunes raises
	// the bar above the raw yield, forcing escalation to the richer rendered pass.
	rawHTML := `<!doctype html><html lang="en"><head><title>Summary</title></head><body>
<article><h1>Summary</h1><p>This raw article body is comfortably over the default two hundred rune floor, so without a higher threshold the auto pass would accept it and never render.</p></article>
</body></html>`
	rendered := func(context.Context, ExtractRequest) (*FetchResponse, error) {
		return &FetchResponse{StatusCode: 200, Body: []byte(`<!doctype html><article><h1>Full</h1><p>` +
			strings.Repeat("The rendered pass returns the complete article with substantially more prose than the server-sent summary, which is exactly what a caller raising the content floor is asking for. ", 4) +
			`</p></article>`)}, nil
	}

	// Without the override, auto keeps the raw summary.
	base := runExtract(t, staticRaw(rawHTML), rendered, ExtractRequest{URL: "https://example.com/x", Mode: ModeAuto})
	if base.Meta.ModeUsed != string(ModeFast) {
		t.Fatalf("baseline mode_used = %q, want fast (raw clears default floor)", base.Meta.ModeUsed)
	}

	// With a floor above the raw yield, auto escalates to rendered.
	raised := runExtract(t, staticRaw(rawHTML), rendered, ExtractRequest{URL: "https://example.com/x", Mode: ModeAuto, MinRunes: 1000})
	if raised.Meta.ModeUsed != string(ModeRendered) {
		t.Fatalf("raised-floor mode_used = %q, want rendered", raised.Meta.ModeUsed)
	}
}

func TestExtractAutoKeepsRawWhenRenderedThinner(t *testing.T) {
	rawHTML := `<!doctype html><html lang="en"><head><title>Raw</title></head><body>
<article><h1>Raw</h1><p>This raw HTML response already carries a substantial article body that sits just under the auto-mode quality threshold, yet still holds far more useful prose than the consent wall the rendered pass returns for this page.</p></article>
</body></html>`
	rendered := func(context.Context, ExtractRequest) (*FetchResponse, error) {
		return &FetchResponse{StatusCode: 200, Body: []byte(`<!doctype html><article><p>Accept cookies to continue.</p></article>`)}, nil
	}
	result := runExtract(t, staticRaw(rawHTML), rendered, ExtractRequest{URL: "https://example.com/wall", Mode: ModeAuto})
	if result.Meta.ModeUsed != string(ModeFast) {
		t.Fatalf("expected raw result to win, mode_used = %q", result.Meta.ModeUsed)
	}
}

func TestExtractCleanFallsBackOnThinArticle(t *testing.T) {
	// A landing page: trafilatura strips the feature/nav chrome down to almost
	// nothing, so the thin-output guard should fall back to full-body extraction
	// and recover the visible text.
	landing := `<!doctype html><html lang="en"><head><title>OpenSERP</title></head><body>
<header><nav><a href="/docs">Docs</a><a href="/pricing">Pricing</a></nav></header>
<main>
<h1>OpenSERP — Free SERP API</h1>
<section class="features">
<div class="card"><h3>Google</h3><p>Scrape Google results with one call.</p></div>
<div class="card"><h3>Bing</h3><p>Bing and Yandex supported out of the box.</p></div>
<div class="card"><h3>Yandex</h3><p>Region targeting and UULE handling built in.</p></div>
</section>
<footer><p>Free and open source SERP scraping for everyone.</p></footer>
</main>
</body></html>`
	result := runExtract(t, staticRaw(landing), nil, ExtractRequest{URL: "https://openserp.org", Mode: ModeFast})
	for _, want := range []string{"Google", "Bing", "Yandex", "Region targeting"} {
		if !strings.Contains(result.Text, want) {
			t.Fatalf("full-body fallback dropped %q; text = %q", want, result.Text)
		}
	}
}

func TestExtractFullPageKeepsChrome(t *testing.T) {
	page := `<!doctype html><html lang="en"><head><title>Dash</title></head><body>
<nav><a href="/a">Alpha</a><a href="/b">Beta</a></nav>
<article><h1>Article</h1><p>A genuine article body that easily clears the extraction quality threshold so the clean path would normally keep only this and discard the navigation links above it.</p></article>
</body></html>`
	// FullPage must retain the nav chrome that clean mode would strip.
	result := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/dash", Mode: ModeFast, FullPage: true})
	if !strings.Contains(result.Text, "Alpha") || !strings.Contains(result.Text, "Beta") {
		t.Fatalf("full-page extraction dropped nav chrome; text = %q", result.Text)
	}
}

func TestExtractLLMSTxtRootHit(t *testing.T) {
	const llmsFull = `# Example Docs

This is the full LLM-optimized corpus for the site. It contains far more useful, structured prose than scraping the rendered HTML landing page would ever surface, which is exactly why agents should prefer it when present at the site root.`
	var fetched []string
	raw := func(_ context.Context, req ExtractRequest) (*FetchResponse, error) {
		fetched = append(fetched, req.URL)
		if strings.HasSuffix(req.URL, "/llms-full.txt") {
			return &FetchResponse{StatusCode: 200, Body: []byte(llmsFull)}, nil
		}
		return &FetchResponse{StatusCode: 404}, nil
	}
	result := runExtract(t, raw, nil, ExtractRequest{URL: "https://example.com", Mode: ModeFast, UseLLMSTxt: true})
	if result.Meta.ModeUsed != "llms_txt" {
		t.Fatalf("mode_used = %q, want llms_txt", result.Meta.ModeUsed)
	}
	if result.Title != "Example Docs" {
		t.Fatalf("title = %q", result.Title)
	}
	if fetched[0] != "https://example.com/llms-full.txt" {
		t.Fatalf("first probe = %q, want .../llms-full.txt", fetched[0])
	}
}

func TestExtractLLMSTxtSkippedForDeepURL(t *testing.T) {
	article := `<!doctype html><html><head><title>Post</title></head><body><article><h1>Post</h1><p>` +
		strings.Repeat("This deep article page has its own substantial body content that must win over any site-level llms index. ", 4) +
		`</p></article></body></html>`
	var probedLLMS bool
	raw := func(_ context.Context, req ExtractRequest) (*FetchResponse, error) {
		if strings.Contains(req.URL, "llms") {
			probedLLMS = true
		}
		return &FetchResponse{StatusCode: 200, Body: []byte(article)}, nil
	}
	result := runExtract(t, raw, nil, ExtractRequest{URL: "https://example.com/blog/post", Mode: ModeFast, UseLLMSTxt: true})
	if probedLLMS {
		t.Fatal("llms.txt was probed for a deep (non-root) URL")
	}
	if result.Meta.ModeUsed == "llms_txt" {
		t.Fatal("deep URL must not resolve via llms.txt")
	}
}

func TestExtractLLMSTxtRejectsHTMLShell(t *testing.T) {
	// A site that answers unknown paths with its SPA index.html (200, but HTML).
	const spaShell = `<!doctype html><html><head><title>App</title></head><body><div id="root"></div></body></html>`
	raw := func(_ context.Context, req ExtractRequest) (*FetchResponse, error) {
		if strings.Contains(req.URL, "llms") {
			return &FetchResponse{StatusCode: 200, Body: []byte(spaShell)}, nil
		}
		return &FetchResponse{StatusCode: 200, Body: []byte(`<!doctype html><html><head><title>Home</title></head><body><article><p>` +
			strings.Repeat("Real homepage article content that should be extracted normally. ", 5) + `</p></article></body></html>`)}, nil
	}
	result := runExtract(t, raw, nil, ExtractRequest{URL: "https://example.com", Mode: ModeFast, UseLLMSTxt: true})
	if result.Meta.ModeUsed == "llms_txt" {
		t.Fatal("HTML shell served at /llms.txt must be rejected")
	}
}

// TestExtractHonorsContextCancellation proves a fetch aborts when the parent
// context is cancelled. The derived batch deadline in the search-enrichment
// path (Config.BatchTimeout) relies on exactly this: once the budget is spent,
// in-flight Extract calls must return promptly with the context error rather
// than running to their own per-fetch timeout.
func TestExtractHonorsContextCancellation(t *testing.T) {
	// A raw fetcher that hangs until its context is cancelled, mimicking a slow
	// or unresponsive target.
	hangingRaw := func(ctx context.Context, _ ExtractRequest) (*FetchResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	extractor := Extractor{RawFetch: hangingRaw, Cfg: DefaultConfig()}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := extractor.Extract(ctx, ExtractRequest{URL: "https://example.com/slow", Mode: ModeFast})
	if err == nil {
		t.Fatal("expected a context error, got nil")
	}
	// Must abort near the parent deadline, not run to the 20s per-fetch timeout.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Extract ignored context cancellation; took %s", elapsed)
	}
}

func TestBatchTimeoutDerivation(t *testing.T) {
	cfg := DefaultConfig() // Timeout=20s, MaxConcurrent=2
	cases := []struct {
		count int
		want  time.Duration
	}{
		{count: 0, want: 20 * time.Second},  // degenerate: one per-URL budget
		{count: 1, want: 40 * time.Second},  // 1 wave  * 2 * 20s
		{count: 2, want: 40 * time.Second},  // 1 wave  * 2 * 20s
		{count: 3, want: 80 * time.Second},  // 2 waves * 2 * 20s
		{count: 5, want: 120 * time.Second}, // 3 waves * 2 * 20s
	}
	for _, tc := range cases {
		if got := cfg.BatchTimeout(tc.count); got != tc.want {
			t.Errorf("BatchTimeout(%d) = %s, want %s", tc.count, got, tc.want)
		}
	}
}

func TestParseMetadataRichTags(t *testing.T) {
	doc, err := documentFromString(`<!doctype html><html lang="en"><head>
<meta property="og:title" content="OG title">
<meta property="og:description" content="OG description">
<meta name="twitter:card" content="summary">
<link rel="canonical" href="/canonical">
<script type="application/ld+json">{"@graph":[{"@type":"Article","headline":"One"},{"@type":"BreadcrumbList"}]}</script>
</head><body><h1>Heading</h1><a href="/a">A link</a></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	meta := parseMetadata(doc, "https://example.com/page")
	if meta.Title != "OG title" || meta.Description != "OG description" {
		t.Fatalf("metadata title/description = %q / %q", meta.Title, meta.Description)
	}
	if meta.Canonical != "https://example.com/canonical" {
		t.Fatalf("canonical = %q", meta.Canonical)
	}
	if len(meta.SchemaOrg) != 2 {
		t.Fatalf("schema_org count = %d", len(meta.SchemaOrg))
	}
	if meta.OGTags["twitter:card"] != "summary" {
		t.Fatalf("og_tags = %#v", meta.OGTags)
	}
}

func documentFromString(raw string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(raw))
}

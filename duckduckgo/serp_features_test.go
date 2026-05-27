package duckduckgo

import (
	"bytes"
	"os"
	"testing"

	"github.com/karust/openserp/core"
)

func TestParseHTMLFixtureExtractsRealFeatures(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/search_results.html")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	results, err := ParseHTML(f)
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertFeatureType(t, results, core.ResultTypeRelatedSearches)
	// wikinlp/about instant-answer modules are surfaced as features, not results.
	assertFeatureType(t, results, core.ResultTypeAISummary)

	// Organic ranks must be a contiguous 1..N sequence. The combined result
	// selector previously matched both the li[data-layout] wrapper and the inner
	// article[data-testid], counting each result twice and leaving rank gaps.
	rank := 0
	for i, r := range results {
		if r.Ad {
			continue
		}
		rank++
		if r.Rank != rank {
			t.Fatalf("organic rank gap at index %d: got %d, want %d", i, r.Rank, rank)
		}
	}
}

func TestParseHTMLExtractsSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<div class="zci zci--answer" id="zero_click_wrapper">
  <h2>DuckDuckGo answer</h2>
  <div class="zci__result">DuckDuckGo instant answer text.</div>
  <a class="zci__more-at" href="https://example.com/source">Source</a>
</div>
<div data-testid="related-searches">
  <a href="https://duckduckgo.com/?q=related+one">related one</a>
  <a href="https://duckduckgo.com/?q=related+two">related two</a>
</div>
<article data-testid="result">
  <h2><a data-testid="result-title-a" href="https://example.com/result">Organic result</a></h2>
  <div data-result="snippet">Snippet</div>
</article>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertFeatureType(t, results, core.ResultTypeAnswerBox)
	assertFeatureType(t, results, core.ResultTypeRelatedSearches)
}

func TestParseHTMLOrganicOnlyHasNoSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<article data-testid="result">
  <h2><a data-testid="result-title-a" href="https://example.com/result">Organic result</a></h2>
  <div data-result="snippet">Snippet</div>
</article>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertNoFeatures(t, results)
}

func assertFeatureType(t *testing.T, results []core.SearchResult, want core.ResultType) {
	t.Helper()
	for _, result := range results {
		for _, feature := range result.Features {
			if feature.Type == want {
				return
			}
		}
	}
	t.Fatalf("expected feature type %q in %#v", want, results)
}

func assertNoFeatures(t *testing.T, results []core.SearchResult) {
	t.Helper()
	for _, result := range results {
		if len(result.Features) > 0 {
			t.Fatalf("expected no features, got %#v", result.Features)
		}
	}
}

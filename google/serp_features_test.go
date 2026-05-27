package google

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

// TestParseHTMLFixtureExtractsRealFeatures guards against selectors drifting
// away from the real-SERP fixture (a JavaScript "fetch API" query). The fixture
// renders Google's AI Overview into the main-col streaming container, so the
// raw parser must emit exactly one ai_summary carrying the full multi-paragraph
// answer (not just the heading) plus its cited source links. The data-mcpr
// fallback container must not also fire and leak inline CSS as a second summary.
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
	assertFeatureType(t, results, core.ResultTypeAISummary)

	summaries := 0
	for _, r := range results {
		for _, ft := range r.Features {
			if ft.Type != core.ResultTypeAISummary {
				continue
			}
			summaries++
			// The full answer body must be captured, not just the heading.
			if len(ft.Text) < 200 {
				t.Fatalf("ai_summary text looks truncated (%d chars): %q", len(ft.Text), ft.Text)
			}
			// A fallback container that swept a <style> block would surface CSS
			// rule syntax rather than prose.
			if strings.Contains(ft.Text, "@keyframes") || strings.Contains(ft.Text, "} .") {
				t.Fatalf("ai_summary text contains CSS, not prose: %q", ft.Text)
			}
			if len(ft.Links) == 0 {
				t.Fatal("expected the ai_summary to carry cited source links")
			}
		}
	}
	if summaries != 1 {
		t.Fatalf("expected exactly 1 ai_summary feature, got %d", summaries)
	}
}

func TestParseHTMLExtractsSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<div data-mcpr="1" aria-label="AI Overview">
  <div role="heading">AI Overview</div>
  <div data-sncf="1">Google AI summary text.</div>
  <a href="https://example.com/source">Source</a>
</div>
<div data-initq="dubai schokolade">
  <div class="wQiwMc related-question-pair" data-q="What is OpenSERP?">
    <a href="https://example.com/paa">What is OpenSERP?</a>
    <div>OpenSERP is a search parser.</div>
  </div>
</div>
<div data-hveid="1" data-ved="2">
  <a href="https://example.com/result"><h3>Organic result</h3></a>
  <div data-sncf="1"><div>Snippet</div></div>
</div>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertFeatureType(t, results, core.ResultTypeAISummary)
	assertFeatureType(t, results, core.ResultTypePeopleAlsoAsk)
}

func TestParseHTMLOrganicOnlyHasNoSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<div data-hveid="1" data-ved="2">
  <a href="https://example.com/result"><h3>Organic result</h3></a>
  <div data-sncf="1"><div>Snippet</div></div>
</div>`

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

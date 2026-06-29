package google

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

// TestParseHTMLFixtureExtractsRealFeatures guards the live AI Overview fixture.
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

	var summaries int
	for _, r := range results {
		for _, ft := range r.Features {
			if ft.Type != core.ResultTypeAISummary {
				continue
			}
			summaries++
			if len(ft.Text) < 200 {
				t.Fatalf("ai_summary text looks truncated (%d chars): %q", len(ft.Text), ft.Text)
			}
			if strings.Contains(strings.ToLower(ft.Text), "недоступен") {
				t.Fatalf("ai_summary captured the 'not available' placeholder: %q", ft.Text)
			}
			if strings.Contains(ft.Text, "@keyframes") || strings.Contains(ft.Text, "} .") {
				t.Fatalf("ai_summary text contains CSS, not prose: %q", ft.Text)
			}
			if len(ft.Links) == 0 {
				t.Fatal("expected ai_summary to carry cited source links")
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

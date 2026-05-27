package baidu

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
}

func TestParseHTMLExtractsSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<div class="result-op c-container" tpl="ai_chat">
  <div class="c-title">AI智能回答</div>
  <div class="c-abstract">Baidu AI summary text.</div>
  <a href="https://example.com/source">Source</a>
</div>
<div id="rs">
  <table>
    <tr>
      <th><a href="https://example.com/related">baidu related search</a></th>
    </tr>
  </table>
</div>
<div id="content_left">
  <div class="result c-container">
    <h3><a href="https://example.com/result">Organic result</a></h3>
    <div class="c-abstract">Snippet</div>
  </div>
</div>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertFeatureType(t, results, core.ResultTypeAISummary)
	assertFeatureType(t, results, core.ResultTypeRelatedSearches)
}

func TestParseHTMLOrganicOnlyHasNoSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<div id="content_left">
  <div class="result c-container">
    <h3><a href="https://example.com/result">Organic result</a></h3>
    <div class="c-abstract">Snippet</div>
  </div>
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

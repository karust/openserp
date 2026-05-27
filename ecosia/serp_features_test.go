package ecosia

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
<section data-test-id="instant-answer">
  <h2>Answer</h2>
  <p data-test-id="instant-answer-description">Ecosia answer text.</p>
</section>
<section data-test-id="related-searches">
  <a href="https://example.com/search?q=trees">trees</a>
</section>
<main data-test-id="mainline">
  <article data-test-id="mainline-result-web">
    <a data-test-id="result-link" href="https://example.com/result">
      <h2 data-test-id="result-title">Organic result</h2>
    </a>
    <p data-test-id="result-description">Snippet</p>
  </article>
</main>`

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
<main data-test-id="mainline">
  <article data-test-id="mainline-result-web">
    <a data-test-id="result-link" href="https://example.com/result">
      <h2 data-test-id="result-title">Organic result</h2>
    </a>
    <p data-test-id="result-description">Snippet</p>
  </article>
</main>`

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

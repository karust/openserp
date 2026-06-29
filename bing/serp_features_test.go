package bing

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

// TestParseHTMLFixtureExtractsRealFeatures guards the live SERP fixture.
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

	related := findFeature(results, core.ResultTypeRelatedSearches)
	if related == nil {
		t.Fatalf("expected related_searches feature in fixture")
	}
	if len(related.Items) == 0 {
		t.Fatalf("related_searches feature has no items: %#v", related)
	}
	for _, r := range results {
		for _, feature := range r.Features {
			if feature.Type == core.ResultTypeAnswerBox && strings.Contains(feature.Title, "searches you might like") {
				t.Fatalf("related-search module leaked into answer_box: %#v", feature)
			}
		}
	}

	for i, r := range results {
		if strings.ContainsAny(r.Description, "\n\t") {
			t.Fatalf("result %d description has raw whitespace: %q", i, r.Description)
		}
	}
}

func TestParseHTMLExtractsSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<ol id="b_results">
  <li class="b_ans">
    <h2>Bing answer</h2>
    <div class="b_focusTextLarge">Bing answer text.</div>
    <div class="b_caption"><p>Source snippet</p></div>
    <a href="https://example.com/source">Source</a>
  </li>
  <li class="b_rrsr">
    <h2>People also ask</h2>
    <ul>
      <li><a href="https://example.com/question">What is OpenSERP?</a></li>
    </ul>
  </li>
  <li class="b_algo">
    <h2><a href="https://example.com/result">Organic result</a></h2>
    <div class="b_caption"><p>Snippet</p></div>
  </li>
</ol>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertFeatureType(t, results, core.ResultTypeAnswerBox)
	assertFeatureType(t, results, core.ResultTypeRelatedQuestions)
}

// TestParseHTMLExtractsRelatedSearchesFromBrsContainer guards the classic footer.
func TestParseHTMLExtractsRelatedSearchesFromBrsContainer(t *testing.T) {
	t.Parallel()

	html := `
<ol id="b_results">
  <li class="b_algo">
    <h2><a href="https://example.com/result">Organic result</a></h2>
    <div class="b_caption"><p>Snippet</p></div>
  </li>
</ol>
<div id="brs">
  <ul>
    <li><a href="https://www.bing.com/search?q=best+languages+2026">best languages 2026</a></li>
    <li><a href="https://www.bing.com/search?q=easiest+language+to+learn">easiest language to learn</a></li>
  </ul>
</div>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertFeatureType(t, results, core.ResultTypeRelatedSearches)
}

// TestParseHTMLExtractsSimilarSearchesFromInlineRail guards the inline rail.
func TestParseHTMLExtractsSimilarSearchesFromInlineRail(t *testing.T) {
	t.Parallel()

	html := `
<ol id="b_results">
  <li class="b_algo">
    <h2><a href="https://example.com/result">Organic result</a></h2>
    <div class="b_caption"><p>Snippet</p></div>
  </li>
  <li class="b_ans">
    <div id="inline_rs" class="b_hide">
      <div id="rs_root" class="rsExplr">
        <h2><a>Users also search for</a><a>Close</a></h2>
        <ul>
          <li class="rslist"><a href="https://www.bing.com/ck/a?u=a1aHR0cA"><span class="b_suggestionText">learn coding free</span></a></li>
          <li class="rslist"><a href="https://www.bing.com/ck/a?u=a1aHR0cB"><span class="b_suggestionText">where to start programming</span></a></li>
        </ul>
      </div>
    </div>
  </li>
</ol>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	related := findFeature(results, core.ResultTypeRelatedSearches)
	if related == nil {
		t.Fatalf("expected related_searches from inline rail")
	}
	if len(related.Items) != 2 {
		t.Fatalf("expected 2 rail items (header chrome excluded), got %d: %#v", len(related.Items), related.Items)
	}
	for _, it := range related.Items {
		if strings.Contains(it.Text, "Users also search") || it.Text == "Close" {
			t.Fatalf("rail header chrome leaked as item: %q", it.Text)
		}
	}
}

func TestParseHTMLOrganicOnlyHasNoSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<ol id="b_results">
  <li class="b_algo">
    <h2><a href="https://example.com/result">Organic result</a></h2>
    <div class="b_caption"><p>Snippet</p></div>
  </li>
</ol>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	assertNoFeatures(t, results)
}

func findFeature(results []core.SearchResult, want core.ResultType) *core.SerpFeature {
	for _, result := range results {
		for i := range result.Features {
			if result.Features[i].Type == want {
				return &result.Features[i]
			}
		}
	}
	return nil
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

package yandex

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

// TestParseHTMLFixtureSeparatesNeuroAnswer locks in the fix where the neuro/AI
// answer card (li data-fast-name='neuro_answer') is excluded from the rankable
// organic stream and surfaced as an ai_summary feature instead.
func TestParseHTMLFixtureSeparatesNeuroAnswer(t *testing.T) {
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

	rank := 0
	for i, r := range results {
		if r.Ad {
			continue
		}
		rank++
		if r.Rank != rank {
			t.Fatalf("organic rank gap at index %d: got %d, want %d", i, r.Rank, rank)
		}
		// The neuro answer's teaser ("Базовый синтаксис"/"Содержимое ответа")
		// must not leak into the organic stream as the first result.
		if strings.Contains(r.Title, "Содержимое ответа") {
			t.Fatalf("neuro answer leaked into organic results at index %d: %q", i, r.Title)
		}
	}
}

func TestParseHTMLExtractsSerpFeatures(t *testing.T) {
	t.Parallel()

	html := `
<div class="FactAnswer">
  <div class="FactAnswer-Title">Yandex answer</div>
  <div class="FactAnswer-Text">Yandex answer text.</div>
</div>
<div class="RelatedSearches">
  <a href="https://yandex.example/search?text=one">yandex related one</a>
  <a href="https://yandex.example/search?text=two">yandex related two</a>
</div>
<ul>
  <li data-fast="1">
    <a class="OrganicTitle-Link" href="https://example.com/result"><h2>Organic result</h2></a>
    <span class="OrganicTextContentSpan">Snippet</span>
  </li>
</ul>`

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
<ul>
  <li data-fast="1">
    <a class="OrganicTitle-Link" href="https://example.com/result"><h2>Organic result</h2></a>
    <span class="OrganicTextContentSpan">Snippet</span>
  </li>
</ul>`

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

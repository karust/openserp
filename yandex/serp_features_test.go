package yandex

import (
	"bytes"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

// TestParseHTMLSeparatesNeuroAnswer locks in the fix where the neuro/AI answer
// card (li data-fast-name='neuro_answer') is excluded from the rankable organic
// stream and surfaced as an ai_summary feature instead. It uses inline HTML
// rather than a saved SERP fixture so the assertion does not break whenever the
// fixture is refreshed with a SERP that happens to lack a neuro card.
func TestParseHTMLSeparatesNeuroAnswer(t *testing.T) {
	t.Parallel()

	html := `
<ul>
  <li data-fast="1" data-fast-name="neuro_answer" class="serp-item">
    <div class="FuturisSearch">
      <div class="FuturisInlineHeader-Text">Нейро</div>
      <div class="FuturisGPTMessage-GroupContent">Содержимое ответа: краткий пересказ.</div>
      <a class="FuturisSource" href="https://source.example/cited">Источник</a>
    </div>
  </li>
  <li data-fast="1" class="serp-item">
    <a class="OrganicTitle-Link" href="https://example.com/first"><h2>First organic</h2></a>
    <span class="OrganicTextContentSpan">First snippet</span>
  </li>
  <li data-fast="1" class="serp-item">
    <a class="OrganicTitle-Link" href="https://example.com/second"><h2>Second organic</h2></a>
    <span class="OrganicTextContentSpan">Second snippet</span>
  </li>
</ul>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
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
		// The neuro answer's teaser must not leak into the organic stream.
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

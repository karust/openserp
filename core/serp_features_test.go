package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestEnvelopeAlwaysIncludesSerpFeatures(t *testing.T) {
	env := NewEnvelope(Query{Text: "golang"}, "req-1", time.Unix(0, 0), []string{"google"})

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if !strings.Contains(string(data), `"serp_features":[]`) {
		t.Fatalf("expected empty serp_features array in JSON, got %s", data)
	}
}

func TestAppendEnrichedSearchResultAddsFeatureSurface(t *testing.T) {
	env := NewEnvelope(Query{Text: "weather"}, "req-1", time.Unix(0, 0), []string{"google"})
	raw := SearchResult{
		Rank:        1,
		URL:         "https://example.com/weather",
		Title:       "Weather result",
		Description: "Organic snippet",
		Features: []SerpFeature{{
			Type: ResultTypeAnswerBox,
			Text: "72 F and sunny",
			Links: []FeatureLink{{
				Title: "Weather source",
				URL:   "https://example.com/weather",
			}},
			Position:   &Position{Absolute: 1},
			Confidence: 0.9,
		}},
	}

	AppendEnrichedSearchResult(env, raw, EnrichContext{Engine: "google", Query: Query{}}, time.Unix(0, 0))

	if len(env.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(env.Results))
	}
	if len(env.SerpFeatures) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(env.SerpFeatures))
	}
	feature := env.SerpFeatures[0]
	if feature.ID == "" || !strings.HasPrefix(feature.ID, "f_") {
		t.Fatalf("expected stable feature ID, got %q", feature.ID)
	}
	if feature.Engine != "google" {
		t.Fatalf("feature engine = %q, want google", feature.Engine)
	}
	if feature.ExtractedAt != "1970-01-01T00:00:00Z" {
		t.Fatalf("feature extracted_at = %q", feature.ExtractedAt)
	}
	if len(feature.SourceResultIDs) != 1 || feature.SourceResultIDs[0] != env.Results[0].ID {
		t.Fatalf("expected feature to reference result ID %q, got %#v", env.Results[0].ID, feature.SourceResultIDs)
	}
}

func TestAppendEnrichedSearchResultMirrorsExistingAnswerResultAsFeature(t *testing.T) {
	env := NewEnvelope(Query{Text: "weather"}, "req-1", time.Unix(0, 0), []string{"google"})
	raw := SearchResult{
		Rank:        -1,
		Type:        ResultTypeAnswerBox,
		URL:         "https://example.com/weather",
		Title:       "Weather",
		Description: "72 F and sunny",
	}

	AppendEnrichedSearchResult(env, raw, EnrichContext{Engine: "google", Query: Query{}}, time.Unix(0, 0))

	if len(env.Results) != 1 {
		t.Fatalf("expected existing result to be preserved, got %d results", len(env.Results))
	}
	if len(env.SerpFeatures) != 1 {
		t.Fatalf("expected mirrored feature, got %d", len(env.SerpFeatures))
	}
	if env.SerpFeatures[0].Type != ResultTypeAnswerBox {
		t.Fatalf("mirrored feature type = %q", env.SerpFeatures[0].Type)
	}
	if env.SerpFeatures[0].Text != "72 F and sunny" {
		t.Fatalf("mirrored feature text = %q", env.SerpFeatures[0].Text)
	}
	if len(env.SerpFeatures[0].SourceResultIDs) != 1 || env.SerpFeatures[0].SourceResultIDs[0] != env.Results[0].ID {
		t.Fatalf("mirrored feature did not reference result: %#v", env.SerpFeatures[0].SourceResultIDs)
	}
}

func TestStripResultFeatures(t *testing.T) {
	results := []SearchResult{
		{
			Rank:  1,
			URL:   "https://example.com",
			Title: "Example",
			Features: []SerpFeature{{
				Type: ResultTypeRelatedSearches,
				Items: []FeatureItem{
					{Text: "example search"},
				},
			}},
		},
		{
			Rank:  2,
			URL:   "https://example.org",
			Title: "Example Org",
		},
	}

	if kept := StripResultFeatures(results, true); len(kept[0].Features) != 1 {
		t.Fatalf("keep=true must preserve features, got %#v", kept[0].Features)
	}

	stripped := StripResultFeatures(results, false)

	if len(stripped) != 2 {
		t.Fatalf("expected result count to be preserved, got %d", len(stripped))
	}
	for i, result := range stripped {
		if len(result.Features) != 0 {
			t.Fatalf("result %d kept features: %#v", i, result.Features)
		}
	}
	if stripped[0].URL != "https://example.com" || stripped[1].Rank != 2 {
		t.Fatalf("non-feature fields changed: %#v", stripped)
	}
}

func TestFeatureItemsUseTextAsTitleAndStripInvisibleCharacters(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
<div class="related">
  <a href="?q=python+coding">python coding&#8203;</a>
</div>`))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	features := ExtractSerpFeaturesBySelectors(doc, []SerpFeatureSelector{{
		Type:         ResultTypeRelatedSearches,
		Title:        "Related searches",
		Container:    []string{".related"},
		ItemSelector: []string{"a"},
		LinkSelector: []string{"a"},
	}})

	if len(features) != 1 || len(features[0].Items) != 1 {
		t.Fatalf("expected one feature item, got %#v", features)
	}
	item := features[0].Items[0]
	if item.Title != "python coding" || item.Text != "python coding" {
		t.Fatalf("unexpected item text/title: %#v", item)
	}
}

func TestEnrichSerpFeatureNormalizesRelativeLinks(t *testing.T) {
	feature := EnrichSerpFeature(SerpFeature{
		Type: ResultTypeRelatedSearches,
		Items: []FeatureItem{{
			Text: "python coding\u200b",
			Link: "?q=python+coding&t=h",
		}},
		Links: []FeatureLink{{
			Title: "baidu related",
			URL:   "/s?wd=python",
		}},
	}, "duckduckgo", "", time.Unix(0, 0))

	if feature.Items[0].Title != "python coding" || feature.Items[0].Text != "python coding" {
		t.Fatalf("unexpected enriched item: %#v", feature.Items[0])
	}
	if got, want := feature.Items[0].Link, "https://duckduckgo.com/?q=python+coding&t=h"; got != want {
		t.Fatalf("item link = %q, want %q", got, want)
	}
	if got, want := feature.Links[0].URL, "https://duckduckgo.com/s?wd=python"; got != want {
		t.Fatalf("feature link = %q, want %q", got, want)
	}
}

func TestRenderersIncludeSerpFeatures(t *testing.T) {
	env := NewEnvelope(Query{Text: "openserp"}, "req-1", time.Unix(0, 0), []string{"google"})
	AppendEnrichedSearchResult(env, SearchResult{
		Rank:        1,
		URL:         "https://example.com/result",
		Title:       "Organic result",
		Description: "Snippet",
		Features: []SerpFeature{
			{
				Type: ResultTypeAISummary,
				Text: "OpenSERP is a search API.",
				Links: []FeatureLink{{
					Title: "Example",
					URL:   "https://example.com/source",
				}},
				Position:   &Position{Absolute: 1},
				Confidence: 0.95,
			},
			{
				Type: ResultTypeRelatedSearches,
				Items: []FeatureItem{
					{Text: "openserp cloud"},
					{Text: "serp api"},
				},
			},
		},
	}, EnrichContext{Engine: "google", Query: Query{}}, time.Unix(0, 0))
	env.Finalize(time.Unix(0, 0), Query{Text: "openserp", Limit: 10})

	markdown := string(RenderMarkdown(env))
	if !strings.Contains(markdown, "## AI summary") || !strings.Contains(markdown, "Sources:") {
		t.Fatalf("markdown missing AI summary feature section:\n%s", markdown)
	}
	if !strings.Contains(markdown, "## Results") {
		t.Fatalf("markdown missing Results section:\n%s", markdown)
	}
	if !strings.Contains(markdown, "## Related searches") {
		t.Fatalf("markdown missing related searches section:\n%s", markdown)
	}
	// Spec order: AI summary -> ... -> Results -> Related searches.
	if aiIdx, resIdx, relIdx := strings.Index(markdown, "## AI summary"), strings.Index(markdown, "## Results"), strings.Index(markdown, "## Related searches"); !(aiIdx < resIdx && resIdx < relIdx) {
		t.Fatalf("markdown section order wrong: ai=%d results=%d related=%d\n%s", aiIdx, resIdx, relIdx, markdown)
	}

	text := string(RenderText(env))
	if !strings.Contains(text, "AI summary\n") || !strings.Contains(text, "Related searches\n") {
		t.Fatalf("text missing feature sections:\n%s", text)
	}

	lines := strings.Split(strings.TrimSpace(string(RenderNDJSON(env))), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 ndjson lines, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], `"kind":"result"`) {
		t.Fatalf("first ndjson line should be a result, got %s", lines[0])
	}
	if !strings.Contains(lines[1], `"kind":"feature"`) || !strings.Contains(lines[2], `"kind":"feature"`) {
		t.Fatalf("feature ndjson lines missing kind tag: %v", lines)
	}
}

// TestRenderFeaturesUnorderedTypeStillRenders guards against silently dropping a
// feature whose Type is in neither render-order section (e.g. a newly added
// enum value not yet placed). It must still appear in text and markdown output.
func TestRenderFeaturesUnorderedTypeStillRenders(t *testing.T) {
	const unplaced ResultType = "experimental_module"
	env := NewEnvelope(Query{Text: "openserp"}, "req-1", time.Unix(0, 0), []string{"google"})
	env.SerpFeatures = append(env.SerpFeatures, SerpFeature{
		Type: unplaced,
		Text: "experimental feature body",
	})

	text := string(RenderText(env))
	if !strings.Contains(text, "experimental feature body") {
		t.Fatalf("text output dropped an unordered feature type:\n%s", text)
	}

	markdown := string(RenderMarkdown(env))
	if !strings.Contains(markdown, "experimental feature body") {
		t.Fatalf("markdown output dropped an unordered feature type:\n%s", markdown)
	}
}

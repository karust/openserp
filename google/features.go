package google

import (
	"context"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractGoogleFeatures(doc *goquery.Document) []core.SerpFeature {
	features := core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			// aimc carries the real AI Overview; mfc can be a placeholder.
			Type:          core.ResultTypeAISummary,
			Title:         "AI Overview",
			Container:     []string{"div[data-mcpr]:has(div[data-subtree='aimc'])", "div[data-container-id='main-col'][data-sfc-root='c']", "div[data-subtree='aifb']", "div[data-mcpr]", "div[aria-label*='AI Overview']", "div[jsname][data-rl]", "div[data-rsoextract]"},
			TitleSelector: []string{"[role='heading']", "h2", "h3"},
			TextSelector:  []string{"div[data-subtree='aimc']", "div[data-streaming-container]", "div[data-sncf='1']", "[data-attrid*='description']", "div[data-subtree='aifb']"},
			LinkSelector:  []string{"div[data-subtree='aimc'] a[href^='http']", "a[href^='http']"},
			Position:      1,
			Confidence:    0.75,
			SingleMatch:   true,
		},
		{
			Type:         core.ResultTypePeopleAlsoAsk,
			Title:        "People also ask",
			Container:    []string{"div[data-initq]", "div[jsname='yEVEwb']"},
			ItemSelector: []string{"div.related-question-pair[data-q]", "div[data-q]"},
			LinkSelector: []string{"a[href^='http']"},
			Position:     1,
			Confidence:   0.8,
			SingleMatch:  true,
		},
		{
			Type:         core.ResultTypeRelatedSearches,
			Title:        "Related searches",
			Container:    []string{"div[jsname='yEVEwb'][role='navigation']", "div[data-abe='1']"},
			ItemSelector: []string{"a[href*='/search?']"},
			LinkSelector: []string{"a[href*='/search?']"},
			Confidence:   0.6,
		},
	})
	return filterGooglePlaceholders(features)
}

// googlePlaceholderText catches empty AI Overview shells.
var googlePlaceholderText = []string{
	"ai overview is not available",
	"an ai overview is not available for this search",
	// Localized "An AI Overview is not available for this query" (ru).
	"обзор от ии недоступен",
}

func filterGooglePlaceholders(features []core.SerpFeature) []core.SerpFeature {
	kept := features[:0]
	for _, feature := range features {
		if feature.Type == core.ResultTypeAISummary && isGooglePlaceholder(feature) {
			continue
		}
		kept = append(kept, feature)
	}
	return kept
}

func isGooglePlaceholder(feature core.SerpFeature) bool {
	text := strings.ToLower(strings.TrimSpace(feature.Text))
	if text == "" || text == "show more" || text == "show less" {
		return true
	}
	if looksLikeCSS(text) {
		return true
	}
	for _, marker := range googlePlaceholderText {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// looksLikeCSS catches fallback containers that swept inline styles.
func looksLikeCSS(text string) bool {
	if strings.Contains(text, "@keyframes") || strings.Contains(text, "@media") {
		return true
	}
	if strings.Contains(text, "} .") || strings.Contains(text, "} #") {
		return true
	}
	return strings.Contains(text, "{ ") && strings.Count(text, ": ") > 20 && strings.Count(text, ";") > 20
}

func extractGoogleFeaturesFromPage(ctx context.Context, page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPageWithWait(ctx, page, extractGoogleFeatures)
}

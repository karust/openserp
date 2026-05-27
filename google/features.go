package google

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractGoogleFeatures(doc *goquery.Document) []core.SerpFeature {
	features := core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			// Google's AI Overview prose renders into the main-col streaming
			// container; the text is fragmented across span[data-subtree]/<strong>/
			// <code> nodes, so the text selector takes the container's whole
			// collapsed text to reconstruct the answer. The older data-mcpr/
			// data-rsoextract containers are kept as fallbacks for other layouts.
			Type:          core.ResultTypeAISummary,
			Title:         "AI Overview",
			Container:     []string{"div[data-container-id='main-col'][data-sfc-root='c']", "div[data-mcpr]", "div[aria-label*='AI Overview']", "div[data-rsoextract]"},
			TitleSelector: []string{"[role='heading']", "h2", "h3"},
			TextSelector:  []string{"div[data-streaming-container]", "div[data-sncf='1']", "[data-attrid*='description']"},
			LinkSelector:  []string{"a[href^='http']"},
			Position:      1,
			Confidence:    0.75,
			// Emit a single AI Overview: the main-col container yields the prose;
			// the data-mcpr fallback otherwise also matches and sweeps embedded CSS.
			SingleMatch: true,
		},
		{
			Type:  core.ResultTypePeopleAlsoAsk,
			Title: "People also ask",
			// div[data-initq] is the single outer PAA module. jsname='yEVEwb'
			// also matches inner expandable sub-panels (one per question), so
			// using it as a container fragments the module into N features;
			// keep it only as a fallback when data-initq is absent.
			Container:    []string{"div[data-initq]", "div[jsname='yEVEwb']"},
			ItemSelector: []string{"div.related-question-pair[data-q]", "div[data-q]"},
			LinkSelector: []string{"a[href^='http']"},
			Position:     1,
			Confidence:   0.8,
			SingleMatch:  true,
		},
		{
			Type:  core.ResultTypeRelatedSearches,
			Title: "Related searches",
			// Scope to the dedicated related-search footer modules only. The
			// main #rso results container also carries data-async-context, so a
			// query:-prefix match there yields navigation chips, not searches.
			Container:    []string{"div[jsname='yEVEwb'][role='navigation']", "div[data-abe='1']"},
			ItemSelector: []string{"a[href*='/search?']"},
			LinkSelector: []string{"a[href*='/search?']"},
			Confidence:   0.6,
		},
	})
	features = filterGooglePlaceholders(features)
	return core.DeduplicateSerpFeatures(features)
}

// googlePlaceholderText flags AI-overview text that Google renders when no
// summary exists ("An AI Overview is not available...") and bare expander
// labels ("Show more") so we don't emit empty/false-positive features.
var googlePlaceholderText = []string{
	"ai overview is not available",
	"an ai overview is not available for this search",
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
	// A fallback container can wrap an inline <style> block whose collapsed text
	// is CSS, not prose. Reject text that is clearly a stylesheet.
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

// looksLikeCSS reports whether collapsed text is dominated by stylesheet syntax.
// It keys off CSS-specific tokens (@keyframes/@media and "selector {" rule
// blocks) rather than a raw brace count, so prose containing code samples with
// braces is not misclassified.
func looksLikeCSS(text string) bool {
	if strings.Contains(text, "@keyframes") || strings.Contains(text, "@media") {
		return true
	}
	// CSS rule blocks look like "} .cls {" / "} #id {"; prose almost never does.
	return strings.Contains(text, "} .") || strings.Contains(text, "} #") || strings.Contains(text, "{ ") && strings.Count(text, ": ") > 20 && strings.Count(text, ";") > 20
}

func extractGoogleFeaturesFromPage(page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPage(page, extractGoogleFeatures)
}

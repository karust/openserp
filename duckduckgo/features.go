package duckduckgo

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractDDGFeatures(doc *goquery.Document) []core.SerpFeature {
	features := core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			// wikinlp is DDG's AI-assisted "DuckAssist" summary
			// (li[data-layout='wikinlp']). The full multi-section answer lives in
			// duckassist-expanded-answer-content; duckassist-answer-content is only
			// the collapsed teaser. Prefer the expanded wrapper and take its whole
			// collapsed text so the body isn't truncated to the teaser.
			Type:          core.ResultTypeAISummary,
			Title:         "Instant Answer",
			Container:     []string{"li[data-layout='wikinlp'] div.react-module", "[data-react-module-id='wikinlp']"},
			TitleSelector: []string{"h2", "h3", ".module__title"},
			TextSelector:  []string{"[data-testid='duckassist-expanded-answer-content']", "[data-testid='duckassist-answer-content']", ".module__text", "p"},
			LinkSelector:  []string{"[data-testid='duckassist-expanded-answer-content'] a[href^='http']", "[data-testid='duckassist-answer-content'] a[href^='http']", "a[href^='http']"},
			Position:      1,
			Confidence:    0.7,
		},
		{
			Type:          core.ResultTypeAnswerBox,
			Title:         "Answer",
			Container:     []string{"#zero_click_wrapper", ".zci", ".zci--answer", ".result--answer", "li[data-layout='about'] .module--about", ".module--about"},
			TitleSelector: []string{".module__title__sub", "h1", "h2", ".zci__title"},
			TextSelector:  []string{".js-about-item-abstr", ".module__text", ".zci__result", ".zci__body", ".result__snippet"},
			LinkSelector:  []string{"a.module__more-at[href^='http']", "a[href^='http']"},
			Position:      1,
			Confidence:    0.8,
		},
		{
			Type:         core.ResultTypeRelatedQuestions,
			Title:        "Related questions",
			Container:    []string{"[data-testid='related-questions']", ".related-questions", ".module--questions"},
			ItemSelector: []string{"a", "button"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.7,
		},
		{
			Type:         core.ResultTypeRelatedSearches,
			Title:        "Related searches",
			Container:    []string{"[data-testid='related-searches']", ".related-searches", ".result__related"},
			ItemSelector: []string{"a"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.75,
		},
	})
	return core.DeduplicateSerpFeatures(features)
}

func extractDDGFeaturesFromPage(page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPage(page, extractDDGFeatures)
}

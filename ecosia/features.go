package ecosia

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractEcosiaFeatures(doc *goquery.Document) []core.SerpFeature {
	features := core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			Type:          core.ResultTypeAnswerBox,
			Title:         "Answer",
			Container:     []string{"[data-test-id='instant-answer']", "[data-test-id='answer-box']", ".instant-answer"},
			TitleSelector: []string{"h2", "[data-test-id='instant-answer-title']"},
			TextSelector:  []string{"[data-test-id='instant-answer-description']", "[data-test-id='answer-box-description']", ".instant-answer__description", "p"},
			LinkSelector:  []string{"a[href^='http']"},
			Position:      1,
			Confidence:    0.8,
		},
		{
			Type:         core.ResultTypeRelatedSearches,
			Title:        "Related searches",
			Container:    []string{"[data-test-id='web-related-queries']", ".related-queries__bottom", "[data-test-id='related-searches']"},
			ItemSelector: []string{"a"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.8,
		},
	})
	return core.DeduplicateSerpFeatures(features)
}

func extractEcosiaFeaturesFromPage(page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPage(page, extractEcosiaFeatures)
}

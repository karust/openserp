package yandex

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractYandexFeatures(doc *goquery.Document) []core.SerpFeature {
	features := core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			// Neuro/AI answer card (data-fast-name='neuro_answer'). Most of the
			// answer body renders client-side from a data-state JSON blob, so the
			// static snapshot exposes the visible teaser text + source links.
			Type:          core.ResultTypeAISummary,
			Title:         "Нейро",
			Container:     []string{"li[data-fast-name='neuro_answer']", ".FuturisSearch", ".FuturisSearchCard"},
			TitleSelector: []string{".FuturisInlineHeader-Text", ".FuturisSearchCard-Title"},
			TextSelector:  []string{".FuturisGPTMessage-GroupContent", ".FuturisSearchCard-Content", ".FuturisSnippetText"},
			// Restrict to cited sources; the block also contains reasoning-plan
			// chips and follow-up suggestion links we don't want as citations.
			LinkSelector: []string{"a.FuturisSource[href^='http']", ".FuturisSourceDetails a[href^='http']"},
			Position:     1,
			Confidence:   0.6,
		},
		{
			Type:          core.ResultTypeAnswerBox,
			Title:         "Answer",
			Container:     []string{".FactAnswer", ".fact-answer", "[data-fast-name='fact']", "[data-fast-name='calculator']", "[data-fast-name='entity_search']", ".Calculator", ".AdaptiveCalc"},
			TitleSelector: []string{".FactAnswer-Title", ".fact-answer__title", "h2"},
			TextSelector:  []string{".FactAnswer-Text", ".fact-answer__text", ".calculator__result", ".AdaptiveCalc-Result", ".ConverterText", ".fact__answer"},
			LinkSelector:  []string{"a[href^='http']"},
			Position:      1,
			Confidence:    0.75,
		},
		{
			Type:         core.ResultTypeRelatedSearches,
			Title:        "Related searches",
			Container:    []string{".RelatedSearches", ".related", ".serp-footer__related", "[data-fast-name='related']", ".RelatedBottom", ".AppndQuestions"},
			ItemSelector: []string{"a"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.7,
		},
	})
	return core.DeduplicateSerpFeatures(features)
}

func extractYandexFeaturesFromPage(page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPage(page, extractYandexFeatures)
}

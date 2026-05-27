package bing

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractBingFeatures(doc *goquery.Document) []core.SerpFeature {
	features := core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			Type: core.ResultTypeAnswerBox,
			// Only treat a b_ans block as an answer box when it carries an
			// actual answer/entity payload. A bare li.b_ans also wraps related
			// modules ("Searches you might like", "Get a detailed look at ..."),
			// so require a focus/fact/xl text node to be present.
			Container:     []string{"li.b_ans:has(.b_focusTextLarge)", "li.b_ans:has(.b_focusLabel)", "li.b_ans:has(.b_xlText)", "li.b_ans:has(.b_factrow)"},
			TitleSelector: []string{".b_focusLabel", "h2"},
			TextSelector:  []string{".b_focusTextLarge", ".b_xlText", ".b_vPanel .b_factrow", ".b_caption p"},
			LinkSelector:  []string{"a[href^='http']"},
			Position:      1,
			Confidence:    0.8,
		},
		{
			Type:         core.ResultTypeRelatedQuestions,
			Title:        "People also ask",
			Container:    []string{".b_rrsr", ".rqnaacfacc", "li.b_ans:has(.df_alaskcr)"},
			ItemSelector: []string{".df_qntext", ".rqnaacfacc a", "li a"},
			LinkSelector: []string{"a[href^='http']"},
			Confidence:   0.7,
		},
		{
			Type:         core.ResultTypeRelatedSearches,
			Title:        "Related searches",
			Container:    []string{"#brsv3", "li.b_rs", "ol#b_rs"},
			ItemSelector: []string{"li a", "a"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.75,
		},
		{
			// Bing's "developer answer" / rich answer card is AI-generated
			// ("This summary was generated using AI based on multiple online
			// sources"). Title sits in h2.b_topTitle; cited sources are the
			// numbered superscript anchors. Copilot chat (#b_sydConvCont) is kept
			// as a fallback for SERPs that render the chat answer inline instead.
			Type:          core.ResultTypeAISummary,
			Title:         "AI answer",
			Container:     []string{".developer_answercard_wrapper", "#b_sydConvCont", ".b_sydConvCont", "[data-testid='bing-chat-answer']"},
			TitleSelector: []string{"h2.b_topTitle", ".b_sydAns"},
			// The full generated answer lives in .devmag_card_content, split across
			// many <p>/<li> inside span.devmag_cntnt_snip; take the wrapper's whole
			// collapsed text so the body isn't truncated to the first paragraph.
			TextSelector: []string{".devmag_card_content", ".rd_def_list", ".b_sydAns", "[data-testid='answer']", "p"},
			LinkSelector: []string{".rd_cnt_srcs a[href^='http']", ".rd_gencon_attr a[href^='http']", "h2.b_topTitle a[href^='http']", "a[href^='http']"},
			Position:     1,
			Confidence:   0.6,
		},
	})
	return core.DeduplicateSerpFeatures(features)
}

func extractBingFeaturesFromPage(page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPage(page, extractBingFeatures)
}

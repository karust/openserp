package bing

import (
	"context"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/karust/openserp/core"
)

func extractBingFeatures(doc *goquery.Document) []core.SerpFeature {
	return core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			Type: core.ResultTypeAnswerBox,
			// li.b_ans also wraps related modules; require answer payload.
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
			Container:    []string{"#brsv3", "#rs_root", "#inline_rs", "#brs", "#b_rs", "ol#b_rs", "li.b_rs"},
			ItemSelector: []string{"li.rslist a", "li a", "a"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.75,
			SingleMatch:  true,
		},
		{
			Type:          core.ResultTypeAISummary,
			Title:         "AI answer",
			Container:     []string{".developer_answercard_wrapper", "#ca_main", ".ca_container", "#b_sydConvCont", ".b_sydConvCont", "[data-testid='bing-chat-answer']"},
			TitleSelector: []string{"h2.b_topTitle", ".b_sydAns"},
			TextSelector:  []string{".devmag_card_content", ".rd_def_list", ".b_sydAns", "[data-testid='answer']", ".ca_div", "p"},
			LinkSelector:  []string{".rd_cnt_srcs a[href^='http']", ".rd_gencon_attr a[href^='http']", "h2.b_topTitle a[href^='http']", "a[href^='http']"},
			Position:      1,
			Confidence:    0.6,
		},
	})
}

func extractBingFeaturesFromPage(ctx context.Context, page *rod.Page) []core.SerpFeature {
	return core.FeaturesFromPageWithWait(ctx, page, extractBingFeatures)
}

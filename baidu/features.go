package baidu

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

func extractBaiduFeatures(doc *goquery.Document) []core.SerpFeature {
	return core.ExtractSerpFeaturesBySelectors(doc, []core.SerpFeatureSelector{
		{
			Type:          core.ResultTypeAISummary,
			Title:         "AI summary",
			Container:     []string{"div[tpl='app/chat-input']", "div[tpl='ai_chat']", "div[tpl*='ai']", ".op-ai-answer", ".cosc-result", ".ai-answer"},
			TitleSelector: []string{".c-title", "h2", "h3"},
			TextSelector:  []string{".cosc-answer", ".op_ai_answer_content", ".ai-answer-content", ".c-abstract"},
			LinkSelector:  []string{"a[href^='http']"},
			Position:      1,
			Confidence:    0.7,
		},
		{
			Type:          core.ResultTypeAnswerBox,
			Title:         "Answer",
			Container:     []string{".op_exactqa_s_answer", ".op_dict_content", ".op_weather4_twoicon", "div[tpl='calculator']", "div[tpl='app/calc']"},
			TitleSelector: []string{".c-title", "h2", "h3"},
			TextSelector:  []string{".op_exactqa_s_answer", ".op_dict_content", ".op_weather4_twoicon", ".op_new_val_screen_result", ".c-abstract"},
			LinkSelector:  []string{"a[href^='http']"},
			Position:      1,
			Confidence:    0.75,
		},
		{
			Type:         core.ResultTypeRelatedSearches,
			Title:        "Related searches",
			Container:    []string{"div[tpl='app/rs']", "#rs_new", "#rs", ".opr-recommends-merge-content", ".c-recommend"},
			ItemSelector: []string{"a"},
			LinkSelector: []string{"a[href^='http']", "a"},
			Confidence:   0.75,
		},
	})
}

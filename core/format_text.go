package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderText formats an Envelope as a minimal plain-text block optimised for
// LLM context windows (~25-30% fewer tokens than JSON for the same data).
func RenderText(env *Envelope) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "Search: %s\n", env.Query.Text)
	enginesStr := strings.Join(env.Query.EnginesRequested, ", ")
	if enginesStr != "" {
		fmt.Fprintf(&b, "Engines: %s\n", enginesStr)
	}
	if len(env.Meta.EnginesFailed) > 0 {
		fmt.Fprintf(&b, "Failed: %s\n", strings.Join(env.Meta.EnginesFailed, ", "))
	}
	b.WriteString("\n")

	renderTextFeatures(&b, env.SerpFeatures, featureRenderOrderBeforeResults())

	if len(env.Results) > 0 {
		b.WriteString("Results\n\n")
	}
	for i, r := range env.Results {
		fmt.Fprintf(&b, "[%d] %s (%s)\n", i+1, r.Title, r.Domain)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "%s\n", r.Snippet)
		}
		fmt.Fprintf(&b, "URL: %s\n\n", r.URL)
	}

	renderTextFeatures(&b, env.SerpFeatures, featureRenderOrderAfterResults())

	return []byte(b.String())
}

// RenderTextImage formats an ImageEnvelope as plain text.
func RenderTextImage(env *ImageEnvelope) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "Image search: %s\n\n", env.Query.Text)

	for i, r := range env.Results {
		fmt.Fprintf(&b, "[%d] %s (%s)\n", i+1, r.Title, r.Source.Domain)
		fmt.Fprintf(&b, "Image: %s\n", r.Image.URL)
		fmt.Fprintf(&b, "Page: %s\n\n", r.Source.PageURL)
	}

	return []byte(b.String())
}

// RenderNDJSON formats an Envelope as newline-delimited JSON.
func RenderNDJSON(env *Envelope) []byte {
	var b strings.Builder
	for _, r := range env.Results {
		writeNDJSONLine(&b, "result", r)
	}
	for _, feature := range env.SerpFeatures {
		writeNDJSONLine(&b, "feature", feature)
	}
	return []byte(b.String())
}

// RenderNDJSONImage formats an ImageEnvelope as newline-delimited JSON.
func RenderNDJSONImage(env *ImageEnvelope) []byte {
	var b strings.Builder
	for _, r := range env.Results {
		writeNDJSONLine(&b, "result", r)
	}
	return []byte(b.String())
}

func renderTextFeatures(b *strings.Builder, features []SerpFeature, order []ResultType) {
	for _, featureType := range order {
		for _, feature := range features {
			if feature.Type != featureType {
				continue
			}
			renderTextFeature(b, feature)
		}
	}
}

func renderTextFeature(b *strings.Builder, feature SerpFeature) {
	heading := featureHeading(feature)
	if feature.Type == ResultTypeKnowledgePanel && feature.Title != "" {
		heading += " - " + feature.Title
	}
	fmt.Fprintf(b, "%s\n", heading)
	if feature.Text != "" {
		fmt.Fprintf(b, "%s", feature.Text)
		if len(feature.Links) == 1 {
			fmt.Fprintf(b, " (source: %s)", feature.Links[0].URL)
		}
		b.WriteString("\n")
	}
	for _, item := range feature.Items {
		switch {
		case item.Title != "" && item.Text != "":
			fmt.Fprintf(b, "- %s - %s\n", item.Title, item.Text)
		case item.Text != "":
			fmt.Fprintf(b, "- %s\n", item.Text)
		case item.Title != "":
			fmt.Fprintf(b, "- %s\n", item.Title)
		}
	}
	if len(feature.Links) > 1 {
		b.WriteString("Sources:\n")
		for _, link := range feature.Links {
			fmt.Fprintf(b, "- %s\n", link.URL)
		}
	}
	b.WriteString("\n")
}

func writeNDJSONLine(b *strings.Builder, kind string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return
	}
	object["kind"] = kind
	data, err = json.Marshal(object)
	if err != nil {
		return
	}
	b.Write(data)
	b.WriteByte('\n')
}

// featureRenderOrderBeforeResults lists the feature sections rendered above the
// results list, in fixed order (spec: AI summary -> answer box -> featured
// snippet -> PAA -> related questions -> knowledge panel -> results -> ...).
func featureRenderOrderBeforeResults() []ResultType {
	return []ResultType{
		ResultTypeAISummary,
		ResultTypeAnswerBox,
		ResultTypeFeaturedSnippet,
		ResultTypePeopleAlsoAsk,
		ResultTypeRelatedQuestions,
		ResultTypeKnowledgePanel,
	}
}

// featureRenderOrderAfterResults lists the feature sections rendered below the
// results list (related searches and the module gallery), in fixed order.
func featureRenderOrderAfterResults() []ResultType {
	return []ResultType{
		ResultTypeRelatedSearches,
		ResultTypeNews,
		ResultTypeVideo,
		ResultTypeVideos,
		ResultTypeShopping,
		ResultTypeImagesInline,
		ResultTypeLocal,
		ResultTypeSitelinks,
		ResultTypeCalculator,
		ResultTypeWeather,
		ResultTypeDictionary,
	}
}

func featureHeading(feature SerpFeature) string {
	switch feature.Type {
	case ResultTypeAISummary:
		return "AI summary"
	case ResultTypeAnswerBox:
		return "Answer box"
	case ResultTypeFeaturedSnippet:
		return "Featured snippet"
	case ResultTypePeopleAlsoAsk:
		return "People also ask"
	case ResultTypeRelatedQuestions:
		return "Related questions"
	case ResultTypeKnowledgePanel:
		return "Knowledge panel"
	case ResultTypeRelatedSearches:
		return "Related searches"
	case ResultTypeNews:
		return "News"
	case ResultTypeVideo, ResultTypeVideos:
		return "Videos"
	case ResultTypeShopping:
		return "Shopping"
	case ResultTypeImagesInline:
		return "Images"
	case ResultTypeLocal:
		return "Local pack"
	case ResultTypeSitelinks:
		return "Sitelinks"
	case ResultTypeCalculator:
		return "Calculator"
	case ResultTypeWeather:
		return "Weather"
	case ResultTypeDictionary:
		return "Dictionary"
	default:
		return strings.ReplaceAll(string(feature.Type), "_", " ")
	}
}

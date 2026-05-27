package core

import (
	"fmt"
	"strings"
)

// RenderMarkdown formats an Envelope as a Markdown document suitable for
// Slack/Discord/email nodes in n8n workflows.
func RenderMarkdown(env *Envelope) []byte {
	var b strings.Builder

	enginesStr := strings.Join(env.Query.EnginesRequested, ", ")
	fmt.Fprintf(&b, "# Search results for %q\n\n", env.Query.Text)
	fmt.Fprintf(&b, "**Query:** %s - **Engines:** %s - **Took:** %dms\n\n",
		env.Query.Text, enginesStr, env.Meta.TookMs)

	if len(env.Meta.EnginesFailed) > 0 {
		fmt.Fprintf(&b, "> Engines that failed: %s\n\n", strings.Join(env.Meta.EnginesFailed, ", "))
	}

	renderMarkdownFeatures(&b, env.SerpFeatures, featureRenderOrderBeforeResults())

	if len(env.Results) > 0 {
		b.WriteString("## Results\n\n")
	}
	for i, r := range env.Results {
		fmt.Fprintf(&b, "### %d. %s\n\n", i+1, escapeMarkdown(r.Title))
		typeLabel := string(r.Type)
		fmt.Fprintf(&b, "**%s** - %s\n\n", r.DisplayURL, typeLabel)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "%s\n\n", r.Snippet)
		}
		fmt.Fprintf(&b, "-> %s\n\n", r.URL)
	}

	renderMarkdownFeatures(&b, env.SerpFeatures, featureRenderOrderAfterResults())

	return []byte(b.String())
}

// RenderMarkdownImage formats an ImageEnvelope as Markdown.
func RenderMarkdownImage(env *ImageEnvelope) []byte {
	var b strings.Builder

	enginesStr := strings.Join(env.Query.EnginesRequested, ", ")
	fmt.Fprintf(&b, "# Image results for %q\n\n", env.Query.Text)
	fmt.Fprintf(&b, "**Query:** %s - **Engines:** %s - **Took:** %dms\n\n",
		env.Query.Text, enginesStr, env.Meta.TookMs)

	for i, r := range env.Results {
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, escapeMarkdown(r.Title))
		fmt.Fprintf(&b, "**Source:** %s\n\n", r.Source.Domain)
		fmt.Fprintf(&b, "-> Image: %s\n", r.Image.URL)
		fmt.Fprintf(&b, "-> Page: %s\n\n", r.Source.PageURL)
	}

	return []byte(b.String())
}

func renderMarkdownFeatures(b *strings.Builder, features []SerpFeature, order []ResultType) {
	for _, featureType := range order {
		for _, feature := range features {
			if feature.Type != featureType {
				continue
			}
			renderMarkdownFeature(b, feature)
		}
	}
}

func renderMarkdownFeature(b *strings.Builder, feature SerpFeature) {
	heading := featureHeading(feature)
	if feature.Type == ResultTypeKnowledgePanel && feature.Title != "" {
		heading += " - " + feature.Title
	}
	fmt.Fprintf(b, "## %s\n\n", heading)
	if feature.Type == ResultTypeFeaturedSnippet && feature.Text != "" {
		fmt.Fprintf(b, "> %s\n", feature.Text)
		if len(feature.Links) > 0 {
			fmt.Fprintf(b, "> - [%s](%s)\n", escapeMarkdown(feature.Links[0].Title), feature.Links[0].URL)
		}
		b.WriteString("\n")
		return
	}
	if feature.Text != "" {
		fmt.Fprintf(b, "%s\n\n", feature.Text)
	}
	if len(feature.Items) > 0 {
		for _, item := range feature.Items {
			switch {
			case item.Title != "" && item.Text != "":
				fmt.Fprintf(b, "- **%s** - %s\n", escapeMarkdown(item.Title), item.Text)
			case item.Text != "":
				fmt.Fprintf(b, "- %s\n", item.Text)
			case item.Title != "":
				fmt.Fprintf(b, "- %s\n", escapeMarkdown(item.Title))
			}
		}
		b.WriteString("\n")
	}
	if len(feature.Links) > 0 {
		b.WriteString("Sources:\n")
		for _, link := range feature.Links {
			title := link.Title
			if title == "" {
				title = link.URL
			}
			fmt.Fprintf(b, "- [%s](%s)\n", escapeMarkdown(title), link.URL)
		}
		b.WriteString("\n")
	}
}

func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"*", `\*`,
		"_", `\_`,
		"`", "\\`",
		"[", `\[`,
		"]", `\]`,
	)
	return replacer.Replace(s)
}

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
		if r.Extracted != nil && r.Extracted.Content != "" {
			b.WriteString("#### Extracted content\n\n")
			b.WriteString(shiftMarkdownHeadings(r.Extracted.Content, 4))
			b.WriteString("\n\n")
		}
	}

	renderMarkdownFeatures(&b, env.SerpFeatures, featureRenderOrderAfterResults(env.SerpFeatures))

	return []byte(b.String())
}

func shiftMarkdownHeadings(markdown string, minLevel int) string {
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		indent := line[:len(line)-len(trimmed)]
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		count := 0
		for count < len(trimmed) && trimmed[count] == '#' {
			count++
		}
		if count == 0 || count >= len(trimmed) || trimmed[count] != ' ' {
			continue
		}
		target := count + minLevel
		if target > 6 {
			target = 6
		}
		lines[i] = indent + strings.Repeat("#", target) + trimmed[count:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
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
	forEachFeatureInOrder(features, order, func(feature SerpFeature) {
		renderMarkdownFeature(b, feature)
	})
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

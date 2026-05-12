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
	fmt.Fprintf(&b, "**Query:** %s · **Engines:** %s · **Took:** %dms\n\n",
		env.Query.Text, enginesStr, env.Meta.TookMs)

	if len(env.Meta.EnginesFailed) > 0 {
		fmt.Fprintf(&b, "> ⚠️ Engines that failed: %s\n\n", strings.Join(env.Meta.EnginesFailed, ", "))
	}

	for i, r := range env.Results {
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, escapeMarkdown(r.Title))
		typeLabel := string(r.Type)
		fmt.Fprintf(&b, "**%s** · %s\n\n", r.DisplayURL, typeLabel)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "%s\n\n", r.Snippet)
		}
		fmt.Fprintf(&b, "→ %s\n\n", r.URL)
	}

	return []byte(b.String())
}

// RenderMarkdownImage formats an ImageEnvelope as Markdown.
func RenderMarkdownImage(env *ImageEnvelope) []byte {
	var b strings.Builder

	enginesStr := strings.Join(env.Query.EnginesRequested, ", ")
	fmt.Fprintf(&b, "# Image results for %q\n\n", env.Query.Text)
	fmt.Fprintf(&b, "**Query:** %s · **Engines:** %s · **Took:** %dms\n\n",
		env.Query.Text, enginesStr, env.Meta.TookMs)

	for i, r := range env.Results {
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, escapeMarkdown(r.Title))
		fmt.Fprintf(&b, "**Source:** %s\n\n", r.Source.Domain)
		fmt.Fprintf(&b, "→ Image: %s\n", r.Image.URL)
		fmt.Fprintf(&b, "→ Page: %s\n\n", r.Source.PageURL)
	}

	return []byte(b.String())
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

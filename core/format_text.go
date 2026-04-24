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
	enginesStr := strings.Join(env.Meta.EnginesResponded, ", ")
	if enginesStr != "" {
		fmt.Fprintf(&b, "Engines: %s\n", enginesStr)
	}
	if len(env.Meta.EnginesFailed) > 0 {
		fmt.Fprintf(&b, "Failed: %s\n", strings.Join(env.Meta.EnginesFailed, ", "))
	}
	b.WriteString("\n")

	for i, r := range env.Results {
		fmt.Fprintf(&b, "[%d] %s (%s)\n", i+1, r.Title, r.Domain)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "%s\n", r.Snippet)
		}
		fmt.Fprintf(&b, "URL: %s\n\n", r.URL)
	}

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

// RenderNDJSON formats an Envelope as newline-delimited JSON (one Result per line).
// The envelope meta is omitted from the body; clients should read response headers.
func RenderNDJSON(env *Envelope) []byte {
	var b strings.Builder
	for _, r := range env.Results {
		data, err := json.Marshal(r)
		if err != nil {
			continue
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// RenderNDJSONImage formats an ImageEnvelope as newline-delimited JSON.
func RenderNDJSONImage(env *ImageEnvelope) []byte {
	var b strings.Builder
	for _, r := range env.Results {
		data, err := json.Marshal(r)
		if err != nil {
			continue
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

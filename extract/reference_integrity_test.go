package extract

import (
	"strings"
	"testing"
)

func TestFunctionReferenceKeepsParameters(t *testing.T) {
	html := `<html><body><div class="refentry"><div class="refnamediv"><h1>decode_packet</h1></div><div class="refsect1"><h2>Description</h2><div class="methodsynopsis">decode_packet(input, mode)</div><p>The input must use a supported character encoding. Invalid input must not be silently replaced with a successful empty packet.</p></div><div class="refsect1"><h2>Parameters</h2><dl><dt>mode</dt><dd>STRICT_MODE rejects malformed records rather than silently skipping invalid fields. This parameter applies to the entire input packet.</dd></dl></div></div><aside>EXTERNAL COMMENTS</aside></body></html>`
	got := runExtract(t, staticRaw(html), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, PreserveStructure: true})
	for _, want := range []string{`decode\_packet`, `STRICT\_MODE`, "entire input packet"} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("lost %s: %s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "EXTERNAL COMMENTS") {
		t.Fatal("included unrelated comments")
	}
}

func TestManualDefinitionOptionsKeepNames(t *testing.T) {
	html := `<html><body><div class="section-level-extent" id="runner"><div class="nav-panel">UNRELATED NAVIGATION</div><h3>Runner</h3><pre>runner [options] command</pre><dl><dt><samp>--keep-status</samp></dt><dd><p>Return the managed command's exit status rather than replacing it when the deadline expires. This is useful when the command performs its own cleanup and has a meaningful exit status.</p></dd><dt><samp>--stop-after</samp></dt><dd><p>Send a termination signal after the additional duration. The duration starts when the first signal is sent, not when the process is initially started.</p></dd></dl></div></body></html>`
	got := runExtract(t, staticRaw(html), nil, ExtractRequest{URL: "https://example.com/manual", Mode: ModeFast, PreserveStructure: true})
	for _, want := range []string{"###### `--keep-status`", "###### `--stop-after`", "duration starts when"} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("lost %s: %s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "UNRELATED NAVIGATION") {
		t.Fatal("included navigation")
	}
}

func TestListedReferenceSectionsRemainSeparate(t *testing.T) {
	html := `<html><main><h1>Reference</h1><ul><li><section><h3>requireValue</h3><pre>requireValue(value)</pre><p>Throws an error when value is absent. The caller must supply an initialized value before calling this method; missing input cannot be converted automatically.</p></section></li><li><section><h3>optionalValue</h3><pre>optionalValue(value)</pre><p>Returns an empty result when value is absent. This method accepts missing input and does not throw merely because the caller omitted a value.</p></section></li></ul></main></html>`
	got := runExtract(t, staticRaw(html), nil, ExtractRequest{URL: "https://example.com/api", Mode: ModeFast, PreserveStructure: true})
	if !strings.Contains(got.Markdown, "\n\n### optionalValue\n") || strings.Contains(got.Markdown, "- ###") {
		t.Fatalf("method heading attached to previous list item: %s", got.Markdown)
	}
}

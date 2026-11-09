package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestStructuredDefinitionsKeepScopeAndExamples(t *testing.T) {
	page := `<html><head><title>Task reference</title></head><body><nav>OUTSIDE NAV</nav><main>
<aside role="complementary">SIDEBAR LINKS</aside><h1>Tasks</h1>
<dl><dt><h2>wait(tasks)</h2></dt><dd><p>Returns completed and pending tasks. A timeout does not cancel pending tasks; callers retain responsibility for cancelling them when no longer needed.</p></dd>
<dt class="sig sig-object" id="wait-for">wait_for(task)</dt><dd><p>Waits for one task. On timeout, cancels the task and waits for cancellation to finish before raising an exception, so elapsed time may exceed the requested timeout.</p><pre><code>await wait_for(task, timeout=5)</code></pre></dd></dl>
<aside role="note">A task must belong to the current event loop.</aside><footer>FOOTER LINKS</footer></main></body></html>`
	legacy := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast})
	if strings.Contains(legacy.Markdown, "######") {
		t.Fatal("structure preservation enabled by default")
	}
	got := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, PreserveStructure: true})
	for _, want := range []string{"## wait(tasks)", `###### wait\_for(task)`, "does not cancel", "cancels the task", "```", "await wait_for(task, timeout=5)", "must belong to the current event loop"} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("missing %q: %s", want, got.Markdown)
		}
	}
	if strings.Index(got.Markdown, "does not cancel") > strings.Index(got.Markdown, `###### wait\_for(task)`) {
		t.Fatalf("definition moved across section: %s", got.Markdown)
	}
	for _, junk := range []string{"OUTSIDE NAV", "SIDEBAR LINKS", "FOOTER LINKS"} {
		if strings.Contains(got.Markdown, junk) {
			t.Fatalf("chrome retained: %s", got.Markdown)
		}
	}
	full := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, FullPage: true, PreserveStructure: true})
	if !strings.Contains(full.Markdown, "OUTSIDE NAV") {
		t.Fatal("full-page behavior changed")
	}
}

func TestStructuredExtractionRequiresUnambiguousSubstantiveLandmark(t *testing.T) {
	for _, page := range []string{
		`<main><h1>One</h1><h2>Two</h2><pre>short shell</pre></main>`,
		`<main><h1>One</h1></main><main><h2>Two</h2><pre>ambiguous</pre></main>`,
		`<article><h1>One</h1><h2>Two</h2><pre>no landmark</pre></article>`,
	} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := extractStructuredContent(doc, "https://example.com"); ok || err != nil {
			t.Fatalf("unexpected structural override: %v %v", ok, err)
		}
	}
}

func TestSingleHeadingReferenceKeepsDefinitionSections(t *testing.T) {
	page := `<html><body><nav>OUTSIDE INDEX</nav><main><h1>Object cloning</h1>
<p>Cloning compound objects requires a decision about whether references to nested mutable values should be shared or replaced with independent copies.</p>
<dl><dt class="sig" id="object.clone">object.clone(value)</dt><dd><p>A shallow clone keeps references to nested objects.</p></dd>
<dt class="sig" id="object.clone_tree">object.clone_tree(value, memo)</dt><dd><p>A recursive clone copies nested objects and uses a memo to avoid copying an object repeatedly.</p>
<pre><code>def clone_tree(value):
    return visit(value)
</code></pre></dd></dl></main></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := extractStructuredContent(doc, "https://example.com/reference"); !ok || err != nil {
		t.Fatalf("reference not recognized: %v %v", ok, err)
	}
	legacy := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast})
	if strings.Contains(legacy.Markdown, "######") {
		t.Fatal("structure preservation enabled by default")
	}
	got := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, PreserveStructure: true})
	for _, want := range []string{"###### object.clone(value)", `###### object.clone\_tree(value, memo)`, "keeps references to nested objects", "uses a memo", "def clone_tree(value):\n    return visit(value)"} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("missing %q: %s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "OUTSIDE INDEX") {
		t.Fatal("navigation retained")
	}
}

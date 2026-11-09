package extract

import (
	"strings"
	"testing"
)

func TestManualSignatureWithAliasAnchors(t *testing.T) {
	page := `<html><body><h1>Tool manual</h1>`
	for _, name := range []string{"strict", "output", "repeat"} {
		page += `<p class="level0"><a name="short-` + name + `"></a><a name="long-` + name + `"></a><span class="nroffip">-x, --` + name + `</span></p><p class="level1">This option controls the request. Errors produce a nonzero exit status. Authentication failures may require additional handling, so the caller must inspect the documented exceptions.</p><pre class="level1">tool --` + name + `</pre>`
	}
	got := runExtract(t, staticRaw(page+`</body></html>`), nil, ExtractRequest{URL: "https://example.com/manual", Mode: ModeFast, PreserveStructure: true})
	if !strings.Contains(got.Markdown, "###### -x, --strict") || !strings.Contains(got.Markdown, "Authentication failures") {
		t.Fatalf("lost aliased option scope: %s", got.Markdown)
	}
}

func TestMethodSignaturePrecedesDescriptionAndImplementation(t *testing.T) {
	page := `<html><main><h1>Container</h1><div class="method-detail" id="method-i-empty"><div class="method-heading"><a href="#method-i-empty"><span class="method-callseq">empty → self</span></a></div><div class="method-controls">Toggle implementation</div><div class="method-source-code"><pre>INTERNAL_IMPLEMENTATION_BODY</pre></div><div class="method-description"><p>Removes every entry from the container and returns the same instance. The operation does not return a missing value, and callers may continue using the returned container after the operation completes.</p><pre>container.empty</pre></div></div><h2>Related methods</h2></main></html>`
	got := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, PreserveStructure: true})
	if !strings.Contains(got.Markdown, "##### [empty → self]") || strings.Index(got.Markdown, "Removes every entry") > strings.Index(got.Markdown, "INTERNAL_IMPLEMENTATION_BODY") || strings.Contains(got.Markdown, "Toggle implementation") {
		t.Fatalf("method structure lost: %s", got.Markdown)
	}
}

func TestDirectiveManualKeepsStatusDefinitions(t *testing.T) {
	page := `<html><body><nav>EXTERNAL NAVIGATION</nav><div id="page-content"><div id="quickview">QUICK LINKS</div><h1>Routing reference</h1><div class="directive-section"><h2>Route</h2><pre>Route status path</pre><p>The status determines which response is returned to the caller. The following named values have distinct behavior and must be interpreted according to their definitions.</p><dl><dt>removed</dt><dd>Returns status 410 and requires the destination argument to be omitted.</dd><dt>temporary</dt><dd>Returns status 302 and requires a destination argument.</dd></dl></div><div class="directive-section"><h2>Alias</h2><p>Maps a URL path to the configured local resource. Trailing separators are significant when comparing the configured path with the request.</p></div></div></body></html>`
	got := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, PreserveStructure: true})
	for _, text := range []string{"removed", "410", "destination argument to be omitted"} {
		if !strings.Contains(got.Markdown, text) {
			t.Fatalf("lost definition %s: %s", text, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "QUICK LINKS") || strings.Contains(got.Markdown, "EXTERNAL NAVIGATION") {
		t.Fatal("navigation leaked into reference")
	}
}

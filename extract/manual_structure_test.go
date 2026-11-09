package extract

import (
	"strings"
	"testing"
)

func TestManualOptionsPreserveTheirWarnings(t *testing.T) {
	page := `<html><body><nav>INDEX CHROME</nav><h1>Tool manual</h1>`
	for _, name := range []string{"--repeat-all", "--resume", "--output"} {
		page += `<p class="level0"><a name="` + name + `"></a><span class="nroffip">` + name + `</span></p>` +
			`<p class="level1">This option controls the operation. Repeating a request can duplicate data. Redirected output cannot be reset before retrying, so callers must handle duplicated output.</p>` +
			`<pre class="level1">tool ` + name + ` example</pre>`
	}
	page += `</body></html>`
	got := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/manual", Mode: ModeFast, PreserveStructure: true})
	for _, name := range []string{"--repeat-all", "--resume", "--output"} {
		if !strings.Contains(got.Markdown, "###### "+name) {
			t.Fatalf("option heading lost: %s", got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "INDEX CHROME") || !strings.Contains(got.Markdown, "Redirected output cannot be reset") {
		t.Fatal("warning or navigation handling changed")
	}
	legacy := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/manual", Mode: ModeFast})
	if strings.Contains(legacy.Markdown, "######") {
		t.Fatal("manual preservation enabled without opt-in")
	}
}

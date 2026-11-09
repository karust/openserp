package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestReferenceEntryKeepsQualifiedRules(t *testing.T) {
	entry := `<div class="refentry"><div class="refnamediv"><h1>READ</h1></div>
<div class="refsynopsisdiv"><h2>Synopsis</h2><pre>READ items IGNORE BUSY</pre></div>
<div class="refsect1"><h2>Locking</h2><p>Using <code>IGNORE BUSY</code> omits rows that cannot be locked immediately. The resulting view is inconsistent, so this mode is unsuitable for general reporting but can be used by multiple consumers of a work queue. Other locks are still acquired normally; this option only changes the handling of the requested row locks.</p></div></div>`
	page := `<html><body><nav>OUTSIDE NAVIGATION</nav>` + entry + `<footer>OUTSIDE FOOTER</footer></body></html>`
	got := runExtract(t, staticRaw(page), nil, ExtractRequest{URL: "https://example.com/reference", Mode: ModeFast, PreserveStructure: true})
	for _, want := range []string{"IGNORE BUSY", "view is inconsistent", "unsuitable for general reporting", "Other locks are still acquired"} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("lost qualification %q: %s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "OUTSIDE") {
		t.Fatal("reference body included surrounding navigation")
	}
	for _, invalid := range []string{entry + entry, strings.ReplaceAll(entry, `class="refsynopsisdiv"`, `class="unknown"`)} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(invalid))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := extractStructuredContent(doc, "https://example.com"); ok || err != nil {
			t.Fatalf("ambiguous or incomplete reference accepted: %v %v", ok, err)
		}
	}
}

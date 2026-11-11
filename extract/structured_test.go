package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// padding lifts a fixture over the thin-content floor.
var padding = "<p>" + strings.Repeat("Background prose that keeps this fixture above the thin-content floor. ", 4) + "</p>"

// Fixtures follow each generator's real markup, trimmed to one or two entries.
var sphinxPage = `<html><body><nav>SITE NAV</nav><div role="main">
<h1>copy<a class="headerlink" href="#module-copy" title="Link to this heading">¶</a></h1>` + padding + `
<dl class="py function"><dt class="sig sig-object py" id="copy.copy">copy.copy(obj)<a class="headerlink" href="#copy.copy">¶</a></dt>
<dd><p>Return a shallow copy of obj.</p></dd></dl>
<dl class="py function"><dt class="sig sig-object py" id="copy.deepcopy">copy.deepcopy(obj)<a class="headerlink" href="#copy.deepcopy">¶</a></dt>
<dd><p>Return a deep copy of obj.</p><pre>y = copy.deepcopy(x)</pre></dd></dl>
<div role="contentinfo">SITE FOOTER</div></div></body></html>`

func TestStructuredLayouts(t *testing.T) {
	for _, tc := range []struct {
		name string
		page string
		want []string // in document order
		drop []string
	}{{
		name: "sphinx",
		page: sphinxPage,
		want: []string{"# copy\n", "###### copy.copy(obj)\n", "shallow copy", "###### copy.deepcopy(obj)\n", "deep copy", "```"},
		drop: []string{"SITE NAV", "SITE FOOTER", "¶"},
	}, {
		name: "roffit man page",
		page: `<html><body><h1>curl.1 the man page</h1>` + padding + `
<p class="level0"><a name="--retry"></a><span class="nroffip">--retry</span></p>
<p class="level1">If a transient error is returned when curl tries to perform a transfer, it retries.</p>
<p class="level0"><a name="--retry-all-errors"></a><span class="nroffip">--retry-all-errors</span></p>
<p class="level1">Retry on any error. There may be unintended consequences such as sending or receiving duplicate data.</p>
<pre class="level1">curl --retry 5 --retry-all-errors https://example.com</pre></body></html>`,
		want: []string{"###### --retry\n", "transient error", "###### --retry-all-errors\n", "duplicate data", "```"},
		drop: []string{"[]("},
	}, {
		name: "docbook refentry",
		page: `<html><body><div class="navheader">PREV NEXT</div><div class="refentry" id="SQL-SELECT">
<div class="refnamediv"><h2>SELECT</h2><p>SELECT - retrieve rows from a table or view</p></div>
<div class="refsynopsisdiv"><h2>Synopsis</h2><pre class="synopsis">SELECT * FROM t FOR UPDATE [ NOWAIT | SKIP LOCKED ]</pre></div>
<div class="refsect1"><h2>The Locking Clause</h2><p>With SKIP LOCKED, any selected rows that cannot be immediately locked are skipped.
Skipping locked rows provides an inconsistent view of the data, so this is not suitable for general purpose work,
but can be used to avoid lock contention with multiple consumers accessing a queue-like table.<sup><a href="#ftn.1">1</a></sup> See also<a href="#ftn.2">†</a>.</p></div>
</div><div class="navfooter">PREV NEXT</div></body></html>`,
		want: []string{"## SELECT", "## Synopsis", "SKIP LOCKED", "## The Locking Clause", "inconsistent view", "[1](", "[†]("},
		drop: []string{"PREV NEXT"},
	}, {
		name: "texinfo options",
		page: `<html><body><div class="section-level-extent" id="timeout-invocation">
<div class="nav-panel"><p>Next: <a href="kill-invocation.html">kill</a></p></div>
<h3 class="section" id="timeout">23.6 timeout: Run a command with a time limit<a class="copiable-link" href="#timeout"> ¶</a></h3>
<a class="index-entry-id" id="index-timeout"></a>
<pre class="example">timeout [option] duration command [arg]...</pre>` + padding + `
<dl class="table"><dt><a id="index-p"></a><span>‘<samp class="samp">-p</samp>’<a class="copiable-link" href="#index-p"> ¶</a></span></dt>
<dt>‘<samp class="samp">--preserve-status</samp>’</dt>
<dd><p>Return the exit status of the managed command on timeout, rather than a specific exit status.</p></dd>
<dt>‘<samp class="samp">--kill-after=duration</samp>’</dt>
<dd><p>The duration starts from the point in time when timeout sends the initial signal.</p></dd></dl></div></body></html>`,
		want: []string{"### 23.6 timeout", "###### ‘`-p`’", "###### ‘`--preserve-status`’", "managed command", "###### ‘`--kill-after=duration`’", "initial signal"},
		drop: []string{"Next:", "¶", "[]("},
	}, {
		name: "rdoc methods",
		page: `<html><body><main><h1>class Hash</h1>` + padding + `
<div id="method-i-clear" class="method-detail"><div class="method-header">
<div class="method-heading"><a href="#method-i-clear" title="Link to this method"><span class="method-callseq">clear → self</span></a><span class="method-click-advice">click to toggle source</span></div>
<div class="method-controls"><details class="method-source-toggle"><summary>Source</summary></details></div></div>
<div class="method-source-code" id="clear-source"><pre class="c">VALUE rb_hash_clear(VALUE hash)</pre></div>
<div class="method-description"><p>Removes all entries from self; returns emptied self.</p><pre>h.clear # => {}</pre></div></div>
<div id="method-i-length" class="method-detail">
<div class="method-heading"><a href="#method-i-length"><span class="method-callseq">length → integer</span></a></div>
<div class="method-description"><p>Returns the count of entries in self.</p></div></div></main></body></html>`,
		want: []string{"###### clear → self\n", "Removes all entries", "h.clear", "###### length → integer\n", "count of entries"},
		drop: []string{"rb_hash_clear", "Source", "toggle source", "[clear"},
	}, {
		name: "javadoc member list",
		page: `<html><body><main role="main"><h1>Class Objects</h1>` + padding + `
<section class="method-details"><h2>Method Details</h2><ul class="member-list">
<li><section class="detail" id="requireNonNull(T)"><h3>requireNonNull</h3>
<div class="member-signature">public static T requireNonNull(T obj)</div>
<div class="block">Checks that the specified object reference is not null.</div></section></li>
<li><section class="detail" id="isNull(java.lang.Object)"><h3>isNull</h3>
<div class="member-signature">public static boolean isNull(Object obj)</div>
<div class="block">Returns true if the provided reference is null.</div><pre>Objects.isNull(value)</pre></section></li>
</ul></section></main></body></html>`,
		want: []string{"### requireNonNull\n", "is not null", "\n\n### isNull\n", "reference is null"},
		drop: []string{"- ###"},
	}, {
		name: "apache directives",
		page: `<html><body><div id="page-header">SITE HEADER</div><div id="page-content">
<div id="preamble"><h1>Apache Module mod_alias</h1>` + padding + `</div>
<div id="quickview"><h3>Directives</h3><ul><li><a href="#redirect">Redirect</a></li></ul></div>
<div class="top"><a href="#page-header">Top</a></div>
<div class="directive-section"><h2 id="redirect">Redirect Directive <a class="permalink" href="#redirect" title="Permanent link">¶</a></h2>
<pre class="prettyprint">Redirect [status] [URL-path] URL</pre>
<dl><dt>gone</dt><dd>Returns a "gone" status (410). When this status is used the URL argument should be omitted.</dd>
<dt>temp</dt><dd>Returns a temporary redirect status (302). This is the default.</dd></dl></div></div></body></html>`,
		want: []string{"## Redirect Directive", "gone", "(410)", "should be omitted"},
		drop: []string{"SITE HEADER", "Directives", "Top", "¶"},
	}, {
		name: "tabs and icon links",
		page: `<html><body><main><h1>Install</h1>` + padding + `
<h2 id="pm">Package managers<a href="#pm" aria-label="Link for this heading" title="Link for this heading"><svg viewBox="0 0 13 13"></svg></a></h2>
<p>Packages <span aria-hidden="true">ICON</span>are on <a href="https://github.com/x/y"><svg><title>GitHub</title></svg></a>
the registry, <a href="https://example.com/guide.pdf" aria-label="Download PDF"><svg></svg></a>.</p>
<div role="tabpanel"><pre>npm install openserp</pre></div>
<div role="tabpanel" hidden><pre>pnpm add openserp</pre></div>
<div hidden>COOKIE DIALOG</div></main></body></html>`,
		want: []string{"## Package managers\n", "[Download PDF](https://example.com/guide.pdf)", "npm install openserp", "pnpm add openserp"},
		drop: []string{"COOKIE DIALOG", "Link for this heading", "ICON", "GitHub", "[]("},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := runExtract(t, staticRaw(tc.page), nil, ExtractRequest{URL: "https://example.com/ref", Mode: ModeFast, PreserveStructure: true}).Markdown
			rest := got
			for _, want := range tc.want {
				i := strings.Index(rest, want)
				if i < 0 {
					t.Fatalf("missing or out of order %q in:\n%s", want, got)
				}
				rest = rest[i+len(want):]
			}
			for _, drop := range tc.drop {
				if strings.Contains(got, drop) {
					t.Fatalf("kept %q in:\n%s", drop, got)
				}
			}
		})
	}
}

func TestStructuredIsOptIn(t *testing.T) {
	req := ExtractRequest{URL: "https://example.com/ref", Mode: ModeFast}
	if got := runExtract(t, staticRaw(sphinxPage), nil, req).Markdown; strings.Contains(got, "######") {
		t.Fatalf("structured output without opt-in:\n%s", got)
	}
	req.PreserveStructure, req.FullPage = true, true
	if got := runExtract(t, staticRaw(sphinxPage), nil, req).Markdown; !strings.Contains(got, "SITE NAV") {
		t.Fatalf("full-page extraction changed:\n%s", got)
	}
}

func TestStructuredRejects(t *testing.T) {
	for name, page := range map[string]string{
		"thin main":      `<main><nav>NAV</nav><h1>One<a href="#one">¶</a></h1><ul><li><h2>Two</h2></li></ul><pre>short shell</pre></main>`,
		"two mains":      `<main><h1>One</h1>` + padding + `</main><main><h2>Two</h2><pre>code</pre></main>`,
		"no container":   `<article><h1>One</h1><h2>Two</h2>` + padding + `<pre>code</pre></article>`,
		"no code or dl":  `<main><h1>One</h1><h2>Two</h2>` + padding + `</main>`,
		"single heading": `<main><h1>One</h1>` + padding + `<pre>code</pre></main>`,
	} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
		if err != nil {
			t.Fatal(err)
		}
		before, _ := doc.Html()
		if _, ok, err := extractStructured(doc, "https://example.com"); ok || err != nil {
			t.Errorf("%s: accepted (err %v)", name, err)
		}
		// Article detection runs on the same document next.
		if after, _ := doc.Html(); after != before {
			t.Errorf("%s: document modified", name)
		}
	}
}

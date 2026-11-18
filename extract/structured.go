package extract

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/atom"
)

// referenceLayouts are tried in order; the first container that matches
// exactly one element is used. chrome lists layout-specific UI to drop.
var referenceLayouts = []struct{ container, chrome string }{
	{"main, [role='main']", ""},
	// DocBook: PostgreSQL, PHP.
	{"div.refentry", ""},
	// Texinfo: GNU manuals.
	{"div.section-level-extent[id]", ".nav-panel"},
	// Apache httpd module docs.
	{"div#page-content:has(div.directive-section)", "#quickview, .toplang, div.top"},
	// roffit man pages, e.g. curl.
	{"body:has(p.level0 > span.nroffip)", ""},
}

// pageChrome is UI found inside content containers. Tab panels are hidden
// until selected but hold real content, e.g. per-language examples.
const pageChrome = "script, style, noscript, template, svg, iframe, nav, footer, " +
	"[role='complementary'], [role='navigation'], [role='banner'], [role='contentinfo'], " +
	"[hidden]:not([role='tabpanel']), [aria-hidden='true']:not([role='tabpanel'])"

// permalinkMarks are the texts of in-page links that are UI: permalinks
// (Docusaurus uses a zero-width space) and footnote back-links.
var permalinkMarks = []string{"¶", "#", "§", "🔗", "​", "↑", "^", "↩", "↩︎"}

const headingTags = "h1, h2, h3, h4, h5, h6"

// extractStructured converts the page's content container as-is instead of
// running article detection, which tends to drop headings, code blocks and
// definition terms on documentation pages and so detaches option names and
// signatures from their descriptions. ok is false when there is no single
// container or it lacks reference structure.
func extractStructured(doc *goquery.Document, baseURL string) (contentResult, bool, error) {
	root := referenceRoot(doc)
	if root == nil {
		return contentResult{}, false, nil
	}
	markReferenceSections(root)
	if !hasReferenceStructure(root) {
		return contentResult{}, false, nil
	}
	out, err := renderContent(doc, root, baseURL)
	return out, true, err
}

// referenceRoot returns a cleaned copy of the content container, or nil.
func referenceRoot(doc *goquery.Document) *goquery.Selection {
	for _, layout := range referenceLayouts {
		found := doc.Find(layout.container)
		if found.Length() != 1 {
			continue
		}
		root := found.Clone()
		root.Find(pageChrome).Remove()
		if layout.chrome != "" {
			root.Find(layout.chrome).Remove()
		}
		return root
	}
	return nil
}

// markReferenceSections turns API entries into headings so each description
// stays under its signature after Markdown conversion.
func markReferenceSections(root *goquery.Selection) {
	// RDoc: the call sequence is the entry title. Implementation source is
	// collapsed on the page and is noise in extracted text.
	root.Find(".method-detail .method-source-code, .method-detail .method-controls, .method-click-advice").Remove()
	retag(root.Find(".method-detail .method-heading"), atom.H6)

	// roffit: an option name is a paragraph holding only the name.
	root.Find("p.level0").Each(func(_ int, p *goquery.Selection) {
		name := p.ChildrenFiltered("span.nroffip")
		if name.Length() == 1 && strings.TrimSpace(p.Text()) == strings.TrimSpace(name.Text()) {
			retag(p, atom.H6)
		}
	})

	// Sphinx signatures and command-line options are definition terms.
	root.Find("dt").Each(func(_ int, dt *goquery.Selection) {
		if dt.Find(headingTags).Length() > 0 {
			return
		}
		if dt.Is(".sig[id]") || isOption(dt.Text()) {
			dt.WrapInnerHtml("<h6></h6>")
		}
	})

	// Javadoc wraps every member section in a list item. As Markdown list
	// items, the next heading would attach to the previous member.
	retag(root.Find("li:has("+headingTags+")"), atom.Div)
	retag(root.Find("ul, ol").FilterFunction(func(_ int, list *goquery.Selection) bool {
		return list.ChildrenFiltered("li").Length() == 0
	}), atom.Div)

	// Permalinks, named anchors and unlabelled icon links render as "[¶](url)"
	// or "[](url)". A labelled icon link gets its label as text.
	root.Find("a").Each(func(_ int, a *goquery.Selection) {
		if a.Find("img").Length() > 0 {
			return
		}
		href := a.AttrOr("href", "")
		self := href == "" || strings.HasPrefix(href, "#")
		text := strings.TrimSpace(a.Text())
		label := strings.TrimSpace(firstNonEmpty(a.AttrOr("aria-label", ""), a.AttrOr("title", "")))
		switch {
		case text == "" && !self && label != "":
			a.SetText(label)
		case text == "" || self && slices.Contains(permalinkMarks, text):
			a.Remove()
		}
	})
	// Self-links that carry a heading's text, as in RDoc, are unwrapped to keep
	// it plain.
	root.Find(headingTags).Find("a[href^='#']").Contents().Unwrap()
}

// hasReferenceStructure requires sections plus code or definitions, and
// enough text to rule out an empty shell.
func hasReferenceStructure(root *goquery.Selection) bool {
	headings := root.Find(headingTags).Length()
	sections := headings >= 2 || headings == 1 && root.Find("dl dt").Length() >= 2
	return sections && root.Find("pre, dl").Length() > 0 &&
		utf8.RuneCountInString(strings.TrimSpace(root.Text())) >= minCleanTextRunes
}

// isOption matches command-line option terms: "--name", "-x", optionally quoted.
func isOption(term string) bool {
	term = strings.TrimLeft(strings.TrimSpace(term), "‘\"'")
	return strings.HasPrefix(term, "--") || len(term) > 1 && term[0] == '-' && unicode.IsLetter(rune(term[1]))
}

func retag(sel *goquery.Selection, tag atom.Atom) {
	for _, node := range sel.Nodes {
		node.Data, node.DataAtom = tag.String(), tag
	}
}

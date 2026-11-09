package extract

import (
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/atom"
)

// Preserve API definitions, headings and examples together when the page has
// an explicit content landmark. Article extraction can flatten definition
// lists and drop headings even when the remaining prose is not thin.
func extractStructuredContent(doc *goquery.Document, baseURL string) (contentResult, bool, error) {
	var out contentResult
	roots := doc.Find("main, [role='main']")
	// DocBook reference entries delimit the manual body even without an
	// HTML main landmark. Keep the synopsis and its qualifying paragraphs.
	if roots.Length() == 0 {
		entries := doc.Find("div.refentry")
		if entries.Length() == 1 && entries.Find(".refnamediv").Length() == 1 &&
			(entries.Find(".refsynopsisdiv").Length() == 1 || entries.Find(".methodsynopsis").Length() == 1) && entries.Find(".refsect1").Length() > 0 {
			roots = entries
		}
	}
	if roots.Length() == 0 {
		sections := doc.Find("div.section-level-extent[id]")
		if sections.Length() == 1 && sections.Find("dl dt").Length() >= 2 && sections.Find("pre").Length() > 0 {
			roots = sections
		}
	}
	// Directive manuals delimit their reference body without a main landmark.
	if roots.Length() == 0 {
		pages := doc.Find("div#page-content")
		if pages.Length() == 1 && pages.Find("div.directive-section").Length() >= 2 && pages.Find("dl dt").Length() >= 2 && pages.Find("pre").Length() > 0 {
			roots = pages
		}
	}
	// HTML manual generators often use anchored nroff option paragraphs
	// instead of a main landmark and semantic headings.
	if roots.Length() == 0 && doc.Find("body").Length() == 1 && manualSignatures(doc.Selection).Length() >= 3 {
		roots = doc.Find("body")
	}
	if roots.Length() != 1 {
		return out, false, nil
	}
	root := roots.First().Clone()
	// An aside can contain a substantive warning or prerequisite; only remove
	// explicitly identified navigation/complementary regions.
	root.Find("script,style,noscript,template,svg,iframe,nav,footer,.nav-panel,[role='complementary'],[role='navigation'],[role='banner'],[role='contentinfo'],[hidden],[aria-hidden='true']").Remove()
	if root.Is("#page-content") {
		root.Find("#quickview,.toplang,div.top").Remove()
	}
	// RDoc signatures are divs. Keep descriptions next to their signatures;
	// implementation source remains available after the explanation.
	root.Find(".method-detail[id]").Each(func(_ int, method *goquery.Selection) {
		if method.Find(".method-description").Length() != 1 {
			return
		}
		method.Find(".method-heading").Each(func(_ int, heading *goquery.Selection) {
			if heading.Find(".method-callseq").Length() == 1 {
				for _, node := range heading.Nodes {
					node.Data, node.DataAtom = "h5", atom.H5
				}
			}
		})
		method.Find(".method-controls").Remove()
		source := method.ChildrenFiltered(".method-source-code").Remove()
		method.AppendSelection(source)
	})
	manualSignatures(root).Each(func(_ int, term *goquery.Selection) {
		if content, err := term.Find("span.nroffip").Html(); err == nil {
			term.SetHtml("<h6>" + content + "</h6>")
		}
	})
	headingCount := root.Find("h1,h2,h3,h4,h5,h6").Length()
	// Reference pages can have a single page title and expose all API entries
	// through definition terms. Those signatures provide the section structure.
	hasSections := headingCount >= 2 || headingCount >= 1 && root.Find("dl dt").Length() >= 2
	if !hasSections ||
		root.Find("pre,dl").Length() == 0 ||
		utf8.RuneCountInString(strings.TrimSpace(root.Text())) < minCleanTextRunes {
		return out, false, nil
	}
	// Sphinx-style signatures are definition terms rather than HTML headings.
	// Give each definition a leaf heading so its following paragraphs retain
	// their API scope after conversion to Markdown. Existing headings win.
	root.Find("dt.sig[id]").Each(func(_ int, term *goquery.Selection) {
		if term.Find("h1,h2,h3,h4,h5,h6").Length() != 0 {
			return
		}
		if content, err := term.Html(); err == nil {
			term.SetHtml("<h6>" + content + "</h6>")
		}
	})
	// Option definitions need explicit section boundaries as well as their
	// descriptions. Preserve other definition lists (parameters, return values).
	root.Find("dt").Each(func(_ int, term *goquery.Selection) {
		text := strings.Trim(strings.TrimSpace(term.Text()), "‘’\"'")
		if strings.HasPrefix(text, "--") && term.Find("h1,h2,h3,h4,h5,h6").Length() == 0 {
			if content, err := term.Html(); err == nil {
				term.SetHtml("<h6>" + content + "</h6>")
			}
		}
	})
	// Some reference generators wrap complete API sections in list items.
	// Emitting them as Markdown lists attaches the next heading to the previous
	// method's Returns/Throws block. Only unwrap items containing section headings.
	root.Find("ul,ol").Each(func(_ int, list *goquery.Selection) {
		items := list.ChildrenFiltered("li")
		if items.Length() == 0 || items.FilterFunction(func(_ int, item *goquery.Selection) bool {
			return item.Find("h1,h2,h3,h4,h5,h6").Length() > 0
		}).Length() != items.Length() {
			return
		}
		for _, node := range list.Nodes {
			node.Data, node.DataAtom = "div", atom.Div
		}
	})
	root.Find("li").Each(func(_ int, item *goquery.Selection) {
		if item.Find("h1,h2,h3,h4,h5,h6").Length() == 0 {
			return
		}
		for _, node := range item.Nodes {
			node.Data, node.DataAtom = "div", atom.Div
		}
	})
	body, err := root.Html()
	if err != nil {
		return out, true, err
	}
	markdown, err := htmlToMarkdown(body, baseURL)
	if err != nil {
		return out, true, err
	}
	out.HTML = strings.TrimSpace(body)
	out.Text = collapseBlankLines(strings.TrimSpace(root.Text()))
	out.Markdown = normalizeMarkdown(markdown)
	out.Title = strings.TrimSpace(doc.Find("title").First().Text())
	out.Description, _ = doc.Find("meta[name='description']").First().Attr("content")
	out.Lang, _ = doc.Find("html").First().Attr("lang")
	return out, true, nil
}

func manualSignatures(root *goquery.Selection) *goquery.Selection {
	return root.Find("p.level0").FilterFunction(func(_ int, p *goquery.Selection) bool {
		return p.Find("a[name],a[id]").Length() >= 1 &&
			p.Find("span.nroffip").Length() == 1 &&
			strings.TrimSpace(p.Text()) == strings.TrimSpace(p.Find("span.nroffip").Text()) &&
			p.Next().Is("p.level1,pre.level1")
	})
}

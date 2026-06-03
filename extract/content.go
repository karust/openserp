package extract

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

var blankLineRun = regexp.MustCompile(`\n{3,}`)

type contentResult struct {
	HTML        string
	Text        string
	Markdown    string
	Title       string
	Description string
	Lang        string
}

// minCleanTextRunes is the trimmed-text floor below which a clean (article-only)
// extraction is treated as too thin. trafilatura strips everything it classifies
// as boilerplate, which guts landing pages, doc indexes, and dashboards where the
// "chrome" is the actual information. When the cleaned text falls under this and
// the page itself carried real content, we fall back to whole-readable-body
// extraction so callers (and LLM agents) get the full page instead of a husk.
const minCleanTextRunes = 250

// extractContent turns a page into markdown/text. When clean is true (the
// default) it uses trafilatura to keep only the main article body; if that pass
// comes back too thin for a page that clearly had content, it transparently
// falls back to full-body extraction. When clean is false it skips article
// detection entirely and converts the whole readable <body>.
func extractContent(htmlBytes []byte, baseURL string, clean bool) (contentResult, error) {
	if !clean {
		return extractFullBody(htmlBytes, baseURL)
	}

	var out contentResult
	opts := trafilatura.Options{
		EnableFallback:  true,
		Focus:           trafilatura.Balanced,
		ExcludeComments: true,
		IncludeImages:   true,
		IncludeLinks:    true,
		Deduplicate:     true,
	}
	if parsed, err := url.Parse(baseURL); err == nil {
		opts.OriginalURL = parsed
	}
	extracted, err := trafilatura.Extract(bytes.NewReader(htmlBytes), opts)
	if err != nil {
		return out, err
	}
	if extracted == nil || extracted.ContentNode == nil {
		return extractFullBody(htmlBytes, baseURL)
	}

	var htmlBuf bytes.Buffer
	if err := html.Render(&htmlBuf, extracted.ContentNode); err != nil {
		return out, err
	}
	out.HTML = strings.TrimSpace(htmlBuf.String())
	out.Text = strings.TrimSpace(extracted.ContentText)
	out.Title = strings.TrimSpace(extracted.Metadata.Title)
	out.Description = strings.TrimSpace(extracted.Metadata.Description)
	out.Lang = strings.TrimSpace(extracted.Metadata.Language)

	markdown, err := htmlToMarkdown(out.HTML, baseURL)
	if err != nil {
		return out, err
	}
	out.Markdown = normalizeMarkdown(markdown)

	// trafilatura was too aggressive: the cleaned article is near-empty but the
	// raw page had real visible text. Prefer the fuller readable-body pass.
	if len([]rune(out.Text)) < minCleanTextRunes {
		if full, ferr := extractFullBody(htmlBytes, baseURL); ferr == nil &&
			len([]rune(full.Text)) > len([]rune(out.Text)) {
			// Keep trafilatura's metadata (title/description/lang) when present;
			// it is usually cleaner than what we derive from the full body.
			full.Title = firstNonEmpty(out.Title, full.Title)
			full.Description = firstNonEmpty(out.Description, full.Description)
			full.Lang = firstNonEmpty(out.Lang, full.Lang)
			return full, nil
		}
	}
	return out, nil
}

// extractFullBody converts the whole readable <body> to markdown, stripping only
// non-content elements (scripts, styles, nav/header/footer chrome is kept since
// for landing pages and indexes that "chrome" is the information). This is the
// raw-er extraction used for clean=false and as the thin-output fallback.
func extractFullBody(htmlBytes []byte, baseURL string) (contentResult, error) {
	var out contentResult
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return out, err
	}
	doc.Find("script,style,noscript,template,svg,iframe").Remove()

	body := doc.Find("body").First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	bodyHTML, err := body.Html()
	if err != nil {
		return out, err
	}
	out.HTML = strings.TrimSpace(bodyHTML)
	out.Text = collapseBlankLines(strings.TrimSpace(body.Text()))
	out.Title = strings.TrimSpace(doc.Find("title").First().Text())
	if lang, ok := doc.Find("html").First().Attr("lang"); ok {
		out.Lang = strings.TrimSpace(lang)
	}

	markdown, err := htmlToMarkdown(out.HTML, baseURL)
	if err != nil {
		return out, err
	}
	out.Markdown = normalizeMarkdown(markdown)
	return out, nil
}

func htmlToMarkdown(htmlStr, baseURL string) (string, error) {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
		),
	)
	conv.Register.Plugin(table.NewTablePlugin(table.WithSkipEmptyRows(true), table.WithHeaderPromotion(true)))
	conv.Register.Plugin(strikethrough.NewStrikethroughPlugin())
	return conv.ConvertString(htmlStr, converter.WithDomain(baseURL))
}

var whitespaceRun = regexp.MustCompile(`[ \t]+`)

// collapseBlankLines tidies the raw .Text() of a full body: each line is trimmed,
// intra-line whitespace runs collapse to a single space, and runs of blank lines
// collapse to one (via normalizeMarkdown).
func collapseBlankLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(whitespaceRun.ReplaceAllString(line, " "))
	}
	return normalizeMarkdown(strings.Join(lines, "\n"))
}

func normalizeMarkdown(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = blankLineRun.ReplaceAllString(markdown, "\n\n")
	return strings.TrimSpace(markdown)
}

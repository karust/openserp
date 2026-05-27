package core

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// blockLevelTags are HTML elements whose boundaries should become line breaks
// when flattening a feature container to text, so block content (headings,
// paragraphs, list items, code blocks) does not fuse into the neighbouring text.
// div/section are deliberately excluded: some engines (e.g. Google's streaming
// AI Overview) wrap every word in its own <div>, which would otherwise put each
// word on its own line. Structure there comes from p/h*/li/br instead.
var blockLevelTags = map[atom.Atom]bool{
	atom.P: true, atom.Br: true, atom.Li: true,
	atom.Tr: true, atom.Pre: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Blockquote: true,
}

// blockAwareText flattens a selection to text while inserting line breaks at
// block-element boundaries, then collapses horizontal whitespace per line and
// drops blank lines. The result keeps logical structure (one line per heading/
// paragraph/list item) instead of fusing words across element edges, which is
// what goquery's raw .Text() does.
func blockAwareText(sel *goquery.Selection) string {
	var sb strings.Builder
	for _, node := range sel.Nodes {
		writeNodeText(&sb, node)
	}
	lines := strings.Split(sb.String(), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = cleanFeatureText(line); line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func writeNodeText(sb *strings.Builder, node *html.Node) {
	switch node.Type {
	case html.TextNode:
		// Collapse whitespace inside the text node (including source-formatting
		// newlines) to single spaces, so only the block-boundary breaks inserted
		// below survive. Preserve a single leading/trailing space so adjacent
		// inline fragments ("global " + "fetch()") keep their word gap.
		text := node.Data
		collapsed := strings.Join(strings.Fields(text), " ")
		if collapsed == "" {
			return
		}
		if len(text) > 0 && isASCIISpace(text[0]) {
			sb.WriteByte(' ')
		}
		sb.WriteString(collapsed)
		if len(text) > 0 && isASCIISpace(text[len(text)-1]) {
			sb.WriteByte(' ')
		}
		return
	case html.ElementNode:
		if node.DataAtom == atom.Script || node.DataAtom == atom.Style {
			return
		}
		block := blockLevelTags[node.DataAtom]
		if block {
			sb.WriteByte('\n')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			writeNodeText(sb, child)
		}
		if block {
			sb.WriteByte('\n')
		}
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			writeNodeText(sb, child)
		}
	}
}

// SerpFeatureSelector describes one engine-native SERP module shape.
type SerpFeatureSelector struct {
	Type          ResultType
	Title         string
	Container     []string
	TitleSelector []string
	TextSelector  []string
	ItemSelector  []string
	LinkSelector  []string
	Position      int
	Confidence    float64
	// SingleMatch emits at most one feature for this spec: the first container
	// node (across Container selectors, in order) that yields content. Use it
	// for modules whose container selector also matches nested sub-panels, which
	// would otherwise fragment one logical module into many features.
	SingleMatch bool
}

// ExtractSerpFeaturesBySelectors converts engine-native SERP module markup into
// normalized features. It is intentionally conservative: a matched container is
// emitted only when it yields text, grouped items, or source links.
func ExtractSerpFeaturesBySelectors(doc *goquery.Document, specs []SerpFeatureSelector) []SerpFeature {
	var features []SerpFeature
	for _, spec := range specs {
		matched := false
		for _, selector := range spec.Container {
			if spec.SingleMatch && matched {
				break
			}
			doc.Find(selector).EachWithBreak(func(_ int, container *goquery.Selection) bool {
				feature := SerpFeature{
					Type:       spec.Type,
					Title:      firstNonEmpty(spec.Title, firstSelectedText(container, spec.TitleSelector)),
					Text:       firstSelectedText(container, spec.TextSelector),
					Items:      selectedFeatureItems(container, spec.ItemSelector),
					Links:      selectedFeatureLinks(container, spec.LinkSelector),
					Confidence: spec.Confidence,
				}
				if spec.Position > 0 {
					feature.Position = &Position{Absolute: spec.Position}
				}
				if feature.Text == "" && len(feature.Items) == 0 && len(feature.Links) == 0 {
					return true
				}
				features = append(features, feature)
				matched = true
				// Stop after the first content-bearing container when SingleMatch.
				return !spec.SingleMatch
			})
		}
	}
	return DeduplicateSerpFeatures(features)
}

// AttachFeaturesToFirstResult keeps ParseHTML signatures unchanged while
// letting server response building split features onto the new top-level field.
func AttachFeaturesToFirstResult(results []SearchResult, features []SerpFeature) []SearchResult {
	if len(features) == 0 {
		return results
	}
	if len(results) == 0 {
		return []SearchResult{{Features: features}}
	}
	results[0].Features = append(results[0].Features, features...)
	return results
}

// DeduplicateSerpFeatures removes duplicate modules emitted by overlapping
// selectors while preserving original order.
func DeduplicateSerpFeatures(features []SerpFeature) []SerpFeature {
	seen := map[string]struct{}{}
	unique := make([]SerpFeature, 0, len(features))
	for _, feature := range features {
		key := serpFeatureKey(feature)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, feature)
	}
	return unique
}

func firstSelectedText(container *goquery.Selection, selectors []string) string {
	for _, selector := range selectors {
		var text string
		container.Find(selector).EachWithBreak(func(_ int, item *goquery.Selection) bool {
			text = blockAwareText(item)
			return text == ""
		})
		if text != "" {
			return text
		}
	}
	return ""
}

func selectedFeatureItems(container *goquery.Selection, selectors []string) []FeatureItem {
	var items []FeatureItem
	for _, selector := range selectors {
		container.Find(selector).Each(func(_ int, item *goquery.Selection) {
			text := cleanFeatureText(item.Text())
			title := firstAttr(item, "data-q", "data-title", "aria-label", "title")
			// Some modules (e.g. Google PAA) carry the question in an attribute
			// and render the answer lazily, so the element text can be empty.
			if text == "" {
				text = cleanFeatureText(title)
			}
			if text == "" {
				return
			}
			link := firstAttr(item, "href", "data-url", "data-link")
			items = append(items, FeatureItem{
				Title: strings.TrimSpace(title),
				Text:  text,
				Link:  strings.TrimSpace(link),
			})
		})
		if len(items) > 0 {
			break
		}
	}
	return items
}

func selectedFeatureLinks(container *goquery.Selection, selectors []string) []FeatureLink {
	var links []FeatureLink
	for _, selector := range selectors {
		container.Find(selector).Each(func(_ int, item *goquery.Selection) {
			href := strings.TrimSpace(firstAttr(item, "href", "data-url", "data-link"))
			if href == "" {
				return
			}
			title := cleanFeatureText(firstAttr(item, "data-title", "aria-label", "title"))
			if title == "" {
				title = cleanFeatureText(item.Text())
			}
			links = append(links, FeatureLink{Title: title, URL: href})
		})
		if len(links) > 0 {
			break
		}
	}
	return links
}

func firstAttr(item *goquery.Selection, names ...string) string {
	for _, name := range names {
		value, ok := item.Attr(name)
		if ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanFeatureText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func serpFeatureKey(feature SerpFeature) string {
	firstLink := ""
	if len(feature.Links) > 0 {
		firstLink = feature.Links[0].URL
	}
	firstItem := ""
	if len(feature.Items) > 0 {
		firstItem = feature.Items[0].Text + "|" + feature.Items[0].Link
	}
	return strings.Join([]string{
		string(feature.Type),
		strings.ToLower(cleanFeatureText(feature.Title)),
		strings.ToLower(cleanFeatureText(feature.Text)),
		strings.ToLower(firstItem),
		strings.ToLower(firstLink),
	}, "|")
}

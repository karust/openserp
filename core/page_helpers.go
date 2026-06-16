package core

import (
	"context"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
)

// pollInterval is how often WaitForElements re-probes selectors while the
// page hydrates. Short enough to feel snappy, long enough not to hammer CDP.
const pollInterval = 120 * time.Millisecond

// WaitForElements probes the supplied CSS selectors until one returns at least
// one matching element or timeout elapses. It exists because rod's
// page.Search/Elements and the surrounding WaitLoad/WaitStable do not wait for
// a *specific* selector to hydrate — modern SPA SERPs (DDG, Bing, Google)
// regularly fire `load` and even reach DOM-stable before result rows render,
// causing parsers to see an empty page on the first probe and forcing the
// caller's retry layer to reload.
//
// The probe loop returns as soon as a selector matches, returning the matched
// elements and the selector that hit. On timeout it returns ErrSearchTimeout
// so callers can disambiguate between "no results" / "captcha" by inspecting
// the page directly.
func WaitForElements(ctx context.Context, page *rod.Page, selectors []string, timeout time.Duration) (rod.Elements, string, error) {
	if page == nil {
		return nil, "", ErrSearchTimeout
	}
	ctx = EnsureContext(ctx)
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	probe := func() (rod.Elements, string) {
		for _, selector := range selectors {
			elements, err := page.Elements(selector)
			if err != nil || len(elements) == 0 {
				continue
			}
			return elements, selector
		}
		return nil, ""
	}

	if elements, selector := probe(); len(elements) > 0 {
		return elements, selector, nil
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if elements, selector := probe(); len(elements) > 0 {
			return elements, selector, nil
		}
		if err := SleepContext(ctx, pollInterval); err != nil {
			return nil, "", err
		}
	}
	return nil, "", ErrSearchTimeout
}

// HasAnySelector returns true if at least one of the supplied selectors
// currently matches in the page DOM. It does not wait — pair with
// WaitForElements when hydration may be in flight.
func HasAnySelector(page *rod.Page, selectors []string) bool {
	if page == nil {
		return false
	}
	for _, selector := range selectors {
		has, _, err := page.Has(selector)
		if err == nil && has {
			return true
		}
	}
	return false
}

// DeferClosePage returns a cleanup function that closes page unless the browser
// is configured to leave pages open for debugging.
func DeferClosePage(ctx context.Context, page *rod.Page, browser *Browser) func() {
	return func() {
		if browser != nil && browser.LeavePageOpen {
			return
		}
		if err := ClosePageWithTimeout(ctx, page, time.Second); err != nil {
			WithRequest(ctx).WithError(err).Debug("Page close error")
		}
	}
}

// HasAttribute reports whether el carries attr (regardless of value).
func HasAttribute(el *rod.Element, attr string) bool {
	if el == nil {
		return false
	}
	v, err := el.Attribute(attr)
	return err == nil && v != nil
}

// NormalizeWhitespace collapses runs of whitespace (newlines, source
// indentation) into single spaces and trims the result.
func NormalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ElementText returns el's visible text, falling back to textContent (for nodes
// rod's Text() leaves empty), normalized. Empty string if el is nil or blank.
func ElementText(el *rod.Element) string {
	if el == nil {
		return ""
	}
	if text, err := el.Text(); err == nil {
		if normalized := NormalizeWhitespace(text); normalized != "" {
			return normalized
		}
	}
	if value, err := el.Property("textContent"); err == nil {
		return NormalizeWhitespace(value.String())
	}
	return ""
}

// ElementAttribute returns the first non-empty value among attrs on el,
// normalized. Empty string if el is nil or none are set.
func ElementAttribute(el *rod.Element, attrs ...string) string {
	if el == nil {
		return ""
	}
	for _, attr := range attrs {
		value, err := el.Attribute(attr)
		if err != nil || value == nil {
			continue
		}
		if normalized := NormalizeWhitespace(*value); normalized != "" {
			return normalized
		}
	}
	return ""
}

// FirstNonEmptyText returns the text (see ElementText) of the first selector
// under root that yields non-empty content. Empty string if none match.
func FirstNonEmptyText(root *rod.Element, selectors ...string) string {
	if root == nil {
		return ""
	}
	for _, selector := range selectors {
		el, err := root.Element(selector)
		if err != nil {
			continue
		}
		if text := ElementText(el); text != "" {
			return text
		}
	}
	return ""
}

// ClosestMatching walks up the ancestor chain (including el itself) and returns
// the first element matching selector, or nil if none is found within maxHops.
// rod has no native Closest helper, so this is a bounded walk used by parsers
// that need to recover a wrapping <a> from a nested title node.
func ClosestMatching(el *rod.Element, selector string, maxHops int) *rod.Element {
	if el == nil || selector == "" {
		return nil
	}
	current := el
	for hop := 0; hop <= maxHops; hop++ {
		if matches, err := current.Matches(selector); err == nil && matches {
			return current
		}
		parent, err := current.Parent()
		if err != nil || parent == nil {
			return nil
		}
		current = parent
	}
	return nil
}

// FirstNonEmptyAttribute returns the trimmed value of attr from the first
// selector under root whose attribute is non-empty.
func FirstNonEmptyAttribute(root *rod.Element, attr string, selectors ...string) string {
	if root == nil {
		return ""
	}
	for _, selector := range selectors {
		el, err := root.Element(selector)
		if err != nil {
			continue
		}
		value, err := el.Attribute(attr)
		if err != nil || value == nil {
			continue
		}
		if trimmed := strings.TrimSpace(*value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// FeaturesFromPage renders a live rod page to HTML and runs a document-level
// feature extractor over it. Every engine's browser path shares this boilerplate
// (page.HTML -> goquery doc -> extract), so it lives here rather than being
// copied per engine. Returns nil on any rendering/parse error.
func FeaturesFromPage(page *rod.Page, extract func(*goquery.Document) []SerpFeature) []SerpFeature {
	html, err := page.HTML()
	if err != nil {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	return extract(doc)
}

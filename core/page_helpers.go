package core

import (
	"context"
	"strings"
	"time"

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

// HasAttribute reports whether el carries attr (regardless of value).
func HasAttribute(el *rod.Element, attr string) bool {
	if el == nil {
		return false
	}
	v, err := el.Attribute(attr)
	return err == nil && v != nil
}

// FirstNonEmptyText returns the trimmed text of the first selector under root
// that yields non-empty content. Empty string if none match.
func FirstNonEmptyText(root *rod.Element, selectors ...string) string {
	if root == nil {
		return ""
	}
	for _, selector := range selectors {
		el, err := root.Element(selector)
		if err != nil {
			continue
		}
		text, err := el.Text()
		if err != nil {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

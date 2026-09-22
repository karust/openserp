package google

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

// search_results_wrapped_links.html is a real Google SERP captured through a
// residential exit, trimmed to the results container. Every organic href on it
// is an encrypted /goto wrapper — the state Google serves to clients it has
// classified as automated — which is exactly the page the parser used to turn
// into ten unusable rows.
func loadWrappedFixture(t *testing.T) []core.SearchResult {
	t.Helper()
	data, err := os.ReadFile("testdata/search_results_wrapped_links.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("fixture parsed to zero results")
	}
	return results
}

// The parser must still find the results — the page is a valid SERP, only its
// hrefs are opaque. Silently returning nothing would look like a blocked page.
func TestWrappedSerpStillParsesResults(t *testing.T) {
	results := loadWrappedFixture(t)
	if len(results) < 9 {
		t.Errorf("parsed %d results, want at least 9", len(results))
	}
	for _, r := range results {
		if strings.TrimSpace(r.Title) == "" {
			t.Errorf("rank %d has no title", r.Rank)
		}
	}
}

// Every href on this page is a wrapper, so the fixture is only meaningful if
// the detector agrees. If this fails, the fixture was captured from a clean
// SERP and the tests below prove nothing.
func TestWrappedFixtureHrefsAreAllWrapped(t *testing.T) {
	results := loadWrappedFixture(t)
	for _, r := range results {
		if core.IsUsableResultURL(r.URL) {
			t.Errorf("rank %d unexpectedly has a usable URL: %.60s", r.Rank, r.URL)
		}
		if !core.IsWrappedGoogleURL(r.URL) {
			t.Errorf("rank %d is unusable but not recognised as wrapped: %.60s", r.Rank, r.URL)
		}
	}
}

// The point of the change: attribution survives even when the link does not.
func TestWrappedSerpStillYieldsDomains(t *testing.T) {
	results := loadWrappedFixture(t)

	missing := []string{}
	for _, r := range results {
		enriched := core.EnrichResult(r, core.EnrichContext{Engine: "google"})
		if enriched.Domain == "" {
			missing = append(missing, r.Title)
			continue
		}
		if strings.Contains(enriched.Domain, "google.") {
			t.Errorf("rank %d attributed to Google itself: %q", r.Rank, enriched.Domain)
		}
		if enriched.Favicon == "" {
			t.Errorf("rank %d has a domain (%s) but no favicon", r.Rank, enriched.Domain)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d/%d results lost their domain: %v", len(missing), len(results), missing)
	}
}

// Google mints a fresh token per impression, so an id derived from the URL
// changes on every request for the same result. Identity must not move.
func TestWrappedResultIDsAreStableAcrossImpressions(t *testing.T) {
	results := loadWrappedFixture(t)

	first := results[0]
	idBefore := core.EnrichResult(first, core.EnrichContext{Engine: "google"}).ID

	// Same result, next impression: same attribution, different token.
	second := first
	second.URL = "/goto?url=CAESZQHrOzAVYfA_5ztYOZSIYzfg5an_NhDU1AaDifferentTokenEntirelyXXXXXXXXXXXXXXXXXXXXXX"
	idAfter := core.EnrichResult(second, core.EnrichContext{Engine: "google"}).ID

	if idBefore != idAfter {
		t.Errorf("id changed with the token: %s -> %s", idBefore, idAfter)
	}
	if idBefore == "" || !strings.HasPrefix(idBefore, "s_") {
		t.Errorf("unexpected id format: %q", idBefore)
	}
}

// Two different results must not collapse onto one id just because neither has
// a usable URL.
func TestWrappedResultIDsStayDistinct(t *testing.T) {
	results := loadWrappedFixture(t)
	seen := map[string]string{}
	for _, r := range results {
		id := core.EnrichResult(r, core.EnrichContext{Engine: "google"}).ID
		if prev, dup := seen[id]; dup {
			t.Errorf("id %s collides: %q and %q", id, prev, r.Title)
		}
		seen[id] = r.Title
	}
}

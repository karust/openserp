package google

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/core"
)

func TestParseHTML(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	for i, r := range results {
		if r.Rank != i+1 {
			t.Fatalf("rank sequence broken at index %d: got %d, want %d", i, r.Rank, i+1)
		}
		if r.URL == "" {
			t.Fatalf("result %d: empty URL", i)
		}
		if r.Title == "" {
			t.Fatalf("result %d: empty Title", i)
		}
		if !strings.HasPrefix(r.URL, "http") {
			t.Fatalf("result %d: URL not absolute: %s", i, r.URL)
		}
	}
}

// TestParseHTMLFixtureResultQuality locks in the real-SERP parser fixes:
// the parser keys on canonical div.tF2Cxc result blocks (no broad data-hveid
// sweep), reaches every organic block in the fixture, and never concatenates
// sibling titles into one. The fixture's organic URLs are sanitized to a single
// placeholder, so DeduplicateResults collapses the public ParseHTML output to
// one row; to prove the selector still walks past the first result we read the
// distinct titles at the document level (pre-dedup), since titles aren't
// sanitized and so still distinguish the separate results.
func TestParseHTMLFixtureResultQuality(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	titles := map[string]struct{}{}
	doc.Find(Selectors.Results).Each(func(_ int, item *goquery.Selection) {
		title := strings.TrimSpace(item.Find(Selectors.Title).First().Text())
		if title == "" {
			return
		}
		// A concatenated title (two h3s swept from a broad container) shows up as
		// an implausibly long title; real organic titles stay well under 200 runes.
		if len([]rune(title)) > 200 {
			t.Fatalf("title looks concatenated: %q", title)
		}
		titles[title] = struct{}{}
	})
	// More than one distinct title proves the selector matched separate results
	// and walked past the first, rather than locking onto one over-broad container.
	if len(titles) < 2 {
		t.Fatalf("expected the canonical selector to reach multiple distinct organic results, got %d", len(titles))
	}

	// The public parser must still return at least one well-formed result.
	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	for i, r := range results {
		if r.Rank != i+1 {
			t.Fatalf("rank sequence broken at index %d: got %d, want %d", i, r.Rank, i+1)
		}
	}
}

func TestParseHTMLEmpty(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestParseHTMLNoResults(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_no_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results, got %d", len(results))
	}
}

func TestParseHTMLCaptcha(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"search_captcha.html", "search_captcha_new.html"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile("testdata/" + fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			results, err := ParseHTML(bytes.NewReader(data))
			if !errors.Is(err, core.ErrCaptcha) {
				t.Fatalf("expected ErrCaptcha, got results=%d err=%v", len(results), err)
			}
		})
	}
}

func TestParseHTMLSoftBlock(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_soft_block.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	results, err := ParseHTML(bytes.NewReader(data))
	if !errors.Is(err, core.ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got results=%d err=%v", len(results), err)
	}
}

func TestParseHTMLAdsDoNotConsumeOrganicRank(t *testing.T) {
	t.Parallel()

	html := `
<div>
  <div data-hveid="1" data-ved="ad" data-text-ad="1">
    <a href="https://ads.example.com"><h3>Sponsored Result</h3></a>
    <div data-sncf="1"><div>Paid snippet</div></div>
  </div>
  <div data-hveid="2" data-ved="organic1">
    <a href="https://organic.example.com/one"><h3>Organic One</h3></a>
    <div data-sncf="1"><div>Organic snippet one</div></div>
  </div>
  <div data-hveid="3" data-ved="organic2">
    <a href="https://organic.example.com/two"><h3>Organic Two</h3></a>
    <div data-sncf="1"><div>Organic snippet two</div></div>
  </div>
</div>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	organicRank := 0
	adRank := 0
	for _, r := range results {
		if r.Ad {
			adRank++
			if r.Rank != adRank {
				t.Fatalf("ad rank = %d, want %d", r.Rank, adRank)
			}
			continue
		}
		organicRank++
		if r.Rank != organicRank {
			t.Fatalf("organic rank = %d, want %d", r.Rank, organicRank)
		}
	}
	if organicRank != 2 {
		t.Fatalf("organic count = %d, want 2", organicRank)
	}
	if results[0].AbsoluteRank != 1 || results[1].AbsoluteRank != 2 || results[2].AbsoluteRank != 3 {
		t.Fatalf("unexpected absolute ranks: %d, %d, %d", results[0].AbsoluteRank, results[1].AbsoluteRank, results[2].AbsoluteRank)
	}
}

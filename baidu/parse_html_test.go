package baidu

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseBaiduHTML(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}

	rank := 0
	for i, r := range results {
		if r.Ad {
			continue
		}
		rank++
		if r.Rank != rank {
			t.Fatalf("rank sequence broken at index %d: got %d, want %d", i, r.Rank, rank)
		}
		if r.URL == "" {
			t.Fatalf("result %d: empty URL", i)
		}
		if r.Title == "" {
			t.Fatalf("result %d: empty Title", i)
		}
	}
	if rank == 0 {
		t.Fatal("expected at least one organic result")
	}
}

// TestParseBaiduHTMLParsesBaike locks in two fixes: baike/encyclopedia cards
// (div.result-op.c-container, tpl=bk_polysemy) are parsed alongside organic
// www_index cards instead of being dropped by first-selector-wins, and op
// "People also search" cards (relative /s? links) are excluded as non-organic.
func TestParseBaiduHTMLParsesBaike(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}

	foundBaike := false
	for i, r := range results {
		if strings.Contains(strings.ToLower(r.Title), "baike") ||
			strings.Contains(strings.ToLower(r.Title), "encyclopedia") {
			foundBaike = true
			if strings.TrimSpace(r.Description) == "" {
				t.Fatalf("baike result %d has empty description", i)
			}
		}
		// Op cards link to relative on-site search; organic results must not.
		if strings.HasPrefix(r.URL, "/") {
			t.Fatalf("result %d has a relative (non-organic) URL: %s", i, r.URL)
		}
	}
	if !foundBaike {
		t.Fatal("expected a baidu baike/encyclopedia result to be parsed")
	}
}

func TestParseBaiduHTMLEmpty(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestParseBaiduHTMLFallbackSelectors(t *testing.T) {
	t.Parallel()

	html := `
<div id="content_left">
  <div class="result-op c-container">
    <h3><a href="https://example.com/result">Fallback Title</a></h3>
    <div class="summary-gap_3Jb4I">Fallback description</div>
  </div>
</div>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].URL != "https://example.com/result" {
		t.Fatalf("unexpected URL: %s", results[0].URL)
	}
	if results[0].Title != "Fallback Title" {
		t.Fatalf("unexpected title: %s", results[0].Title)
	}
	if results[0].Description != "Fallback description" {
		t.Fatalf("unexpected description: %s", results[0].Description)
	}
}

func TestParseBaiduHTMLFallsBackWhenEarlierSelectorHasNoResult(t *testing.T) {
	t.Parallel()

	html := `
<div id="content_left">
  <div class="result c-container"></div>
  <div class="result-op c-container">
    <h3><a href="https://example.com/parseable">Parseable Title</a></h3>
    <div class="summary-gap_3Jb4I">Parseable description</div>
  </div>
</div>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].URL != "https://example.com/parseable" {
		t.Fatalf("unexpected URL: %s", results[0].URL)
	}
}

func TestParseBaiduHTMLAdsDoNotConsumeOrganicRank(t *testing.T) {
	t.Parallel()

	html := `
<div id="content_left">
  <div class="result c-container" data-tuiguang="1">
    <h3><a href="https://ads.example.com">Sponsored Result</a></h3>
    <div class="c-abstract">Paid snippet</div>
  </div>
  <div class="result c-container">
    <h3><a href="https://organic.example.com/one">Organic One</a></h3>
    <div class="c-abstract">Organic snippet one</div>
  </div>
  <div class="result c-container">
    <h3><a href="https://organic.example.com/two">Organic Two</a></h3>
    <div class="c-abstract">Organic snippet two</div>
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

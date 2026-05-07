package baidu

import (
	"bytes"
	"os"
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

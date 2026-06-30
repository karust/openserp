package duckduckgo

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

func TestParseDDGHTML(t *testing.T) {
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
		if !strings.HasPrefix(r.URL, "http") {
			t.Fatalf("result %d: URL not absolute: %s", i, r.URL)
		}
	}
	if rank == 0 {
		t.Fatal("expected at least one organic result")
	}
}

func TestParseDDGHTMLEmpty(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestParseDDGHTMLCaptcha(t *testing.T) {
	t.Parallel()

	html := `<html><body><form action="/anomaly.js"><input name="challenge"></form></body></html>`
	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if !errors.Is(err, core.ErrCaptcha) {
		t.Fatalf("expected ErrCaptcha, got results=%d err=%v", len(results), err)
	}
}

func TestParseDDGHTMLNoResults(t *testing.T) {
	t.Parallel()

	html := `<html><body><div data-testid="no-results">No results found.</div></body></html>`
	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results, got %d", len(results))
	}
}

func TestParseDDGHTMLAdsDoNotConsumeOrganicRank(t *testing.T) {
	t.Parallel()

	html := `
<article data-testid="ad">
  <h2><a data-testid="result-title-a" href="https://ads.example.com">Sponsored Result</a></h2>
  <div data-result="snippet">Paid snippet</div>
</article>
<article data-testid="result">
  <h2><a data-testid="result-title-a" href="https://organic.example.com/one">Organic One</a></h2>
  <div data-result="snippet">Organic snippet one</div>
</article>
<article data-testid="result">
  <h2><a data-testid="result-title-a" href="https://organic.example.com/two">Organic Two</a></h2>
  <div data-result="snippet">Organic snippet two</div>
</article>`

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

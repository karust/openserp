package yandex

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseYandexHTML(t *testing.T) {
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

func TestParseYandexHTMLEmpty(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestParseYandexHTMLFallbackSelectors(t *testing.T) {
	t.Parallel()

	html := `
<ul>
  <li class="serp-item">
    <h2><a href="https://example.com/result">Fallback Title</a></h2>
    <div class="OrganicText">Fallback description</div>
  </li>
</ul>`

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

func TestParseYandexHTMLAdsDoNotConsumeOrganicRank(t *testing.T) {
	t.Parallel()

	html := `
<ul>
  <li class="serp-item" data-fast-name="direct">
    <h2><a href="https://ads.example.com">Sponsored Result</a></h2>
    <div class="OrganicText">Paid snippet</div>
  </li>
  <li class="serp-item">
    <h2><a href="https://organic.example.com/one">Organic One</a></h2>
    <div class="OrganicText">Organic snippet one</div>
  </li>
  <li class="serp-item">
    <h2><a href="https://organic.example.com/two">Organic Two</a></h2>
    <div class="OrganicText">Organic snippet two</div>
  </li>
</ul>`

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

func TestParseYandexHTMLYabsURLIsAd(t *testing.T) {
	t.Parallel()

	html := `
<ul>
  <li class="serp-item">
    <h2><a href="https://yabs.yandex.ru/count/WuGejI_zOoVX2">WB Travel</a></h2>
    <div class="OrganicText">Paid snippet</div>
  </li>
  <li class="serp-item">
    <h2><a href="https://organic.example.com/one">Organic One</a></h2>
    <div class="OrganicText">Organic snippet one</div>
  </li>
</ul>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Ad || results[0].Rank != 1 {
		t.Fatalf("first result should be ad rank 1: %+v", results[0])
	}
	if results[1].Ad || results[1].Rank != 1 {
		t.Fatalf("second result should be organic rank 1: %+v", results[1])
	}
}

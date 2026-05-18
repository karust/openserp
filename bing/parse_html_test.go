package bing

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseBingHTML(t *testing.T) {
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

func TestParseBingHTMLEmpty(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestParseBingHTMLAds(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}

	var ads []struct{ url, title string }
	for _, r := range results {
		if r.Ad {
			ads = append(ads, struct{ url, title string }{r.URL, r.Title})
		}
	}

	for i, ad := range ads {
		if ad.url == "" {
			t.Fatalf("ad result %d: empty URL", i)
		}
		if ad.title == "" {
			t.Fatalf("ad result %d: empty Title", i)
		}
	}
}

func TestParseBingHTMLMixedAdsKeepAbsoluteOrder(t *testing.T) {
	t.Parallel()

	html := `
<ol id="b_results">
  <li class="b_algo">
    <h2><a href="https://organic.example.com/one">Organic One</a></h2>
    <div class="b_caption"><p>Organic snippet one</p></div>
  </li>
  <li class="b_ad">
    <h2><a href="https://ads.example.com">Sponsored Result</a></h2>
    <p>Paid snippet</p>
  </li>
  <li class="b_algo">
    <h2><a href="https://organic.example.com/two">Organic Two</a></h2>
    <div class="b_caption"><p>Organic snippet two</p></div>
  </li>
</ol>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Ad || !results[1].Ad || results[2].Ad {
		t.Fatalf("unexpected ad ordering: %+v", results)
	}
	if results[0].AbsoluteRank != 1 || results[1].AbsoluteRank != 2 || results[2].AbsoluteRank != 3 {
		t.Fatalf("unexpected absolute ranks: %d, %d, %d", results[0].AbsoluteRank, results[1].AbsoluteRank, results[2].AbsoluteRank)
	}
	if results[2].Rank != 2 {
		t.Fatalf("second organic rank = %d, want 2", results[2].Rank)
	}
}

func TestParseBingHTMLTitleFallback(t *testing.T) {
	t.Parallel()

	html := `
<ol id="b_results">
  <li class="b_algo">
    <h2><a aria-label="Fallback Title" href="https://example.com/fallback"></a></h2>
    <div class="b_caption"><p>Snippet</p></div>
  </li>
</ol>`

	results, err := ParseHTML(bytes.NewReader([]byte(html)))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Fallback Title" {
		t.Fatalf("title = %q, want fallback", results[0].Title)
	}
}

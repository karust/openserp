package core

import (
	"testing"
	"time"
)

func TestDeduplicateResultsOrdersByAbsoluteRank(t *testing.T) {
	t.Parallel()

	results := DeduplicateResults([]SearchResult{
		{Rank: 1, AbsoluteRank: 3, URL: "https://organic.example.com/one"},
		{Rank: 1, AbsoluteRank: 1, URL: "https://ads.example.com/one", Ad: true},
		{Rank: 2, AbsoluteRank: 2, URL: "https://ads.example.com/two", Ad: true},
		{Rank: 2, AbsoluteRank: 4, URL: "https://organic.example.com/two"},
	})

	wantRanks := []int{1, 2, 1, 2}
	wantAbsoluteRanks := []int{1, 2, 3, 4}
	if len(results) != len(wantRanks) {
		t.Fatalf("len(results) = %d, want %d", len(results), len(wantRanks))
	}
	for i, want := range wantRanks {
		if results[i].Rank != want {
			t.Fatalf("result %d rank = %d, want %d", i, results[i].Rank, want)
		}
		if results[i].AbsoluteRank != wantAbsoluteRanks[i] {
			t.Fatalf("result %d absolute rank = %d, want %d", i, results[i].AbsoluteRank, wantAbsoluteRanks[i])
		}
	}
}

func TestDeduplicateResultsKeepsAdAndOrganicForSameURL(t *testing.T) {
	t.Parallel()

	results := DeduplicateResults([]SearchResult{
		{Rank: 1, AbsoluteRank: 1, URL: "https://example.com/page", Ad: true},
		{Rank: 1, AbsoluteRank: 2, URL: "https://example.com/page"},
	})

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if !results[0].Ad || results[1].Ad {
		t.Fatalf("expected ad and organic rows to be preserved separately: %+v", results)
	}
}

func TestLimitOrganicResultsDoesNotCountAds(t *testing.T) {
	t.Parallel()

	results := LimitOrganicResults([]SearchResult{
		{Rank: 1, URL: "https://ads.example.com/one", Ad: true},
		{Rank: 1, URL: "https://organic.example.com/one"},
		{Rank: 2, URL: "https://ads.example.com/two", Ad: true},
		{Rank: 2, URL: "https://organic.example.com/two"},
	}, 1)

	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if CountOrganicResults(results) != 1 {
		t.Fatalf("organic count = %d, want 1", CountOrganicResults(results))
	}
}

func TestEnrichResultUsesAdRankAndAbsolutePosition(t *testing.T) {
	t.Parallel()

	result := EnrichResult(SearchResult{
		Rank:         1,
		AbsoluteRank: 2,
		URL:          "https://ads.example.com/",
		Title:        "Ad",
		Ad:           true,
	}, EnrichContext{Engine: "google", Query: Query{Limit: 10}})

	if result.Rank != 1 {
		t.Fatalf("rank = %d, want 1", result.Rank)
	}
	if result.Position == nil || result.Position.Absolute != 2 {
		t.Fatalf("unexpected position: %+v", result.Position)
	}
	if result.Type != ResultTypeAd {
		t.Fatalf("unexpected result type: %q", result.Type)
	}
}

func TestEnvelopePaginationCountsOrganicResults(t *testing.T) {
	t.Parallel()

	env := NewEnvelope(Query{Text: "ads", Limit: 2}, "req-1", time.Now(), []string{"bing"})
	env.Results = []Result{
		{Rank: 1, Type: ResultTypeAd},
		{Rank: 2, Type: ResultTypeAd},
		{Rank: 1, Type: ResultTypeOrganic},
	}

	env.Finalize(time.Now(), Query{Text: "ads", Limit: 2})

	if env.Pagination.HasMore {
		t.Fatalf("has_more = true, want false when organic count is below limit: %+v", env.Pagination)
	}
}

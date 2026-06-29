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

func TestEnrichResultUsesExplicitResultType(t *testing.T) {
	t.Parallel()

	result := EnrichResult(SearchResult{
		Rank:  1,
		Type:  ResultTypePeopleAlsoAsk,
		URL:   "https://example.com/question",
		Title: "Question",
	}, EnrichContext{Engine: "google", Query: Query{Limit: 10}})

	if result.Type != ResultTypePeopleAlsoAsk {
		t.Fatalf("unexpected result type: %q", result.Type)
	}
}

func TestValidateResultTypeAcceptsFullTaxonomy(t *testing.T) {
	t.Parallel()

	types := []ResultType{
		ResultTypeOrganic,
		ResultTypeAd,
		ResultTypeFeaturedSnippet,
		ResultTypeKnowledgePanel,
		ResultTypePeopleAlsoAsk,
		ResultTypeVideo,
		ResultTypeImage,
		ResultTypeNews,
		ResultTypeShopping,
		ResultTypeLocal,
		ResultTypeAnswerBox,
		ResultTypeAISummary,
		ResultTypeRelatedQuestions,
		ResultTypeRelatedSearches,
		ResultTypeSitelinks,
		ResultTypeVideos,
		ResultTypeImagesInline,
		ResultTypeCalculator,
		ResultTypeWeather,
		ResultTypeDictionary,
	}

	for _, typ := range types {
		got, warning := ValidateResultType(typ)
		if got != typ || warning != "" {
			t.Fatalf("ValidateResultType(%q) = (%q, %q), want (%q, empty)", typ, got, warning, typ)
		}
	}
}

func TestRankStateInterleavesAdsAndSeedsPages(t *testing.T) {
	t.Parallel()

	// Page 1 (0-based): organic ranks continue at 11, ad ranks always restart at
	// 1, and the absolute rank counts every emitted row regardless of kind.
	rank := NewRankState(1)

	steps := []struct {
		isAd           bool
		rank, absolute int
	}{
		{true, 1, 11},   // ad
		{false, 11, 12}, // organic (seeded from page*10)
		{false, 12, 13}, // organic
		{true, 2, 14},   // ad interleaved after organics
		{false, 13, 15}, // organic
	}
	for i, s := range steps {
		gotRank, gotAbs := rank.Next(s.isAd)
		if gotRank != s.rank || gotAbs != s.absolute {
			t.Fatalf("step %d (ad=%v): got rank=%d absolute=%d, want rank=%d absolute=%d",
				i, s.isAd, gotRank, gotAbs, s.rank, s.absolute)
		}
	}
}

func TestSetSeparatedAdAbsoluteRanks(t *testing.T) {
	t.Parallel()

	// Ecosia collects ads and organics in separate passes (each rank-1-based),
	// then this assigns one mixed absolute order: ads first, then organics.
	results := []SearchResult{
		{Rank: 1, URL: "https://organic.example.com/one"},
		{Rank: 2, URL: "https://organic.example.com/two"},
		{Rank: 1, Ad: true, URL: "https://ads.example.com/one"},
		{Rank: 2, Ad: true, URL: "https://ads.example.com/two"},
	}

	SetSeparatedAdAbsoluteRanks(results, 0)

	want := []int{3, 4, 1, 2} // ads (passes 1,2) precede organics (3,4) in absolute order
	for i, r := range results {
		if r.AbsoluteRank != want[i] {
			t.Fatalf("%s absolute rank = %d, want %d", r.URL, r.AbsoluteRank, want[i])
		}
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

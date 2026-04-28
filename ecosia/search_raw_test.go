package ecosia

import (
	"testing"

	"github.com/karust/openserp/testutil"
)

func TestEcosiaResultParser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		wantCount  int
	}{
		{name: "search results", fixture: "search_results.html", wantCount: 10},
		{name: "no results", fixture: "search_no_results.html", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := resultParser(testutil.ResponseFromFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("resultParser() error = %v", err)
			}
			if len(results) != tt.wantCount {
				t.Fatalf("expected %d results for %s, got %d", tt.wantCount, tt.fixture, len(results))
			}
			if tt.wantCount == 0 {
				return
			}

			testutil.AssertSequentialRanks(t, results)
			testutil.AssertFirstResultFilled(t, results)
		})
	}
}

func TestEcosiaImageResultParser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   string
		wantCount int
	}{
		{name: "image results", fixture: "images_results.html", wantCount: 24},
		{name: "no results", fixture: "images_no_results.html", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := imageResultParser(testutil.ResponseFromFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("imageResultParser() error = %v", err)
			}
			if len(results) != tt.wantCount {
				t.Fatalf("expected %d results for %s, got %d", tt.wantCount, tt.fixture, len(results))
			}
			if tt.wantCount == 0 {
				return
			}
			testutil.AssertSequentialRanks(t, results)
			testutil.AssertFirstResultFilled(t, results)
		})
	}
}

func TestEcosiaResultParserEmptyHTML(t *testing.T) {
	t.Parallel()

	results, err := resultParser(testutil.ResponseFromString(""))
	if err != nil {
		t.Fatalf("resultParser() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

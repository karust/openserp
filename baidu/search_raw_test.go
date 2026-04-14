package baidu

import (
	"testing"

	"github.com/karust/openserp/testutil"
)

func TestBaiduResultParserSnapshots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		fixture        string
		minResultCount int
		maxResultCount int
		wantZero       bool
	}{
		{
			name:           "search results",
			fixture:        "search_results.html",
			minResultCount: 5,
			maxResultCount: 30,
		},
		{
			name:     "no results",
			fixture:  "search_no_results.html",
			wantZero: true,
		},
		{
			name:     "captcha page",
			fixture:  "search_captcha.html",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := baiduResultParser(testutil.ResponseFromFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("baiduResultParser() error = %v", err)
			}

			if tt.wantZero {
				if len(results) != 0 {
					t.Fatalf("expected zero results for %s, got %d", tt.fixture, len(results))
				}
				return
			}

			if len(results) < tt.minResultCount || len(results) > tt.maxResultCount {
				t.Fatalf(
					"unexpected result count for %s: got %d, want range [%d,%d]",
					tt.fixture, len(results), tt.minResultCount, tt.maxResultCount,
				)
			}

			testutil.AssertSequentialRanks(t, results)
			testutil.AssertFirstResultFilled(t, results)
		})
	}
}

func TestBaiduResultParserEmptyHTML(t *testing.T) {
	t.Parallel()

	results, err := baiduResultParser(testutil.ResponseFromString(""))
	if err != nil {
		t.Fatalf("baiduResultParser() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

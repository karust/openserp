package google

import (
	"errors"
	"io"
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

func TestGoogleParseHTMLFixtures(t *testing.T) {
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
			minResultCount: 1,
			maxResultCount: 20,
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

			results, err := ParseHTML(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if err != nil {
				t.Fatalf("ParseHTML() error = %v", err)
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

func TestGoogleParseHTMLEmptyHTML(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(testutil.ResponseFromString("").Body)
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

func TestGoogleClassifyRawHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    error
	}{
		{name: "no results", fixture: "search_no_results.html", want: core.ErrEmptyResult},
		{name: "captcha page", fixture: "search_captcha.html", want: core.ErrCaptcha},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, err := io.ReadAll(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if err != nil {
				t.Fatalf("read fixture body: %v", err)
			}
			err = classifyGoogleRawHTML(body)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v for %s, got %v", tt.want, tt.fixture, err)
			}
		})
	}
}

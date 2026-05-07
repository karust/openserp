package ecosia

import (
	"errors"
	"io"
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

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

func TestEcosiaClassifyRawHTML(t *testing.T) {
	t.Parallel()

	body, err := io.ReadAll(testutil.ResponseFromFixture(t, "search_no_results.html").Body)
	if err != nil {
		t.Fatalf("read fixture body: %v", err)
	}

	err = classifyEcosiaRawHTML(body)
	if !errors.Is(err, core.ErrEmptyResult) {
		t.Fatalf("expected %v for search_no_results.html, got %v", core.ErrEmptyResult, err)
	}
}

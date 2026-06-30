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

	tests := []struct {
		fixture string
		want    error
	}{
		{"search_no_results.html", core.ErrEmptyResult},
		{"search_captcha.html", core.ErrCaptcha},
		{"search_results.html", nil},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()

			body, err := io.ReadAll(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if err != nil {
				t.Fatalf("read fixture body: %v", err)
			}

			got := classifyEcosiaRawHTML(body)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil for %s, got %v", tt.fixture, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("expected %v for %s, got %v", tt.want, tt.fixture, got)
			}
		})
	}
}

func TestEcosiaParseHTMLClassifiesCaptchaAndNoResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture string
		wantErr error
	}{
		{"search_captcha.html", core.ErrCaptcha},
		{"search_no_results.html", nil},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()

			results, err := ParseHTML(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v for %s, got %v", tt.wantErr, tt.fixture, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHTML() error = %v", err)
			}
			if len(results) != 0 {
				t.Fatalf("expected zero results for %s, got %d", tt.fixture, len(results))
			}
		})
	}
}

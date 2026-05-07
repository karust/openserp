package baidu

import (
	"errors"
	"io"
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

// TestBaiduParseHTMLFixtures covers the no-results and captcha fixtures.
// The happy path is covered in TestParseBaiduHTML; this file ensures the
// shared parser (used by both raw mode and the /baidu/parse endpoint) does
// not over-extract on captcha or empty SERPs.
func TestBaiduParseHTMLFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantZero bool
	}{
		{name: "no results", fixture: "search_no_results.html", wantZero: true},
		{name: "captcha page", fixture: "search_captcha.html", wantZero: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := ParseHTML(testutil.ResponseFromFixture(t, tt.fixture).Body)
			if err != nil {
				t.Fatalf("ParseHTML() error = %v", err)
			}
			if tt.wantZero && len(results) != 0 {
				t.Fatalf("expected zero results for %s, got %d", tt.fixture, len(results))
			}
		})
	}
}

func TestBaiduClassifyRawHTML(t *testing.T) {
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
			err = classifyBaiduRawHTML(body)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v for %s, got %v", tt.want, tt.fixture, err)
			}
		})
	}
}

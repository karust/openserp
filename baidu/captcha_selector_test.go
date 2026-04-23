package baidu

import (
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/testutil"
)

// TestBaiduPageTypeSelectors verifies that the selectors defined in selectors.go
// match (or don't match) real fixture HTML without needing a browser.
func TestBaiduPageTypeSelectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture  string
		selector string
		wantHit  bool
	}{
		{"search_captcha.html", sel.Captcha, true},
		{"search_captcha.html", sel.Timeout, true},
		{"search_captcha.html", sel.Results, false},

		{"search_results.html", sel.Results, true},
		{"search_results.html", sel.Captcha, false},
		{"search_results.html", sel.Timeout, false},

		{"search_no_results.html", sel.Captcha, false},
		{"search_no_results.html", sel.Timeout, false},
		{"search_no_results.html", sel.Results, false},
	}

	for _, tt := range tests {
		t.Run(tt.fixture+"/"+tt.selector, func(t *testing.T) {
			t.Parallel()
			assertSelector(t, tt.fixture, tt.selector, tt.wantHit)
		})
	}
}

func assertSelector(t *testing.T, fixture, selector string, wantHit bool) {
	t.Helper()

	resp := testutil.ResponseFromFixture(t, fixture)
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	got := doc.Find(selector).Length() > 0
	if got != wantHit {
		if wantHit {
			t.Fatalf("selector %q not found in %s — update selectors.go", selector, fixture)
		} else {
			t.Fatalf("selector %q unexpectedly present in %s", selector, fixture)
		}
	}
}

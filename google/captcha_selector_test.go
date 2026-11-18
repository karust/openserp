package google

import (
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/testutil"
)

// TestGooglePageTypeSelectors verifies that the selectors defined in selectors.go
// match (or don't match) real fixture HTML without needing a browser.
func TestGooglePageTypeSelectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture  string
		selector string
		wantHit  bool
	}{
		{"search_captcha.html", Selectors.Captcha, true},
		{"search_captcha.html", Selectors.CaptchaPage, true},
		{"search_captcha.html", Selectors.ResultStats, false},

		{"search_captcha_new.html", Selectors.Captcha, true},
		{"search_captcha_new.html", Selectors.CaptchaPage, true},
		{"search_captcha_new.html", Selectors.ResultStats, false},

		{"search_sorry.html", Selectors.CaptchaPage, true},
		{"search_sorry.html", Selectors.ResultStats, false},

		{"search_soft_block.html", Selectors.SoftBlock, true},
		{"search_soft_block.html", Selectors.Captcha, false},
		{"search_soft_block.html", Selectors.CaptchaPage, false},
		{"search_soft_block.html", Selectors.ResultStats, false},

		{"search_results.html", Selectors.ResultStats, true},
		{"search_results.html", Selectors.Captcha, false},
		{"search_results.html", Selectors.CaptchaPage, false},

		{"search_no_results.html", Selectors.ResultStats, true},
		{"search_no_results.html", Selectors.Captcha, false},
		{"search_no_results.html", Selectors.CaptchaPage, false},
		{"search_no_results.html", Selectors.NoResults, true},
	}

	for _, tt := range tests {
		t.Run(tt.fixture+"/"+tt.selector, func(t *testing.T) {
			t.Parallel()
			assertSelector(t, tt.fixture, tt.selector, tt.wantHit)
		})
	}
}

// Mirrors the WaitForElements probe: the first ready hit decides the outcome.
func TestGoogleReadySelectorsOnFixtures(t *testing.T) {
	t.Parallel()

	for fixture, want := range map[string]string{
		"search_results.html":    searchReadySelectors()[0],
		"search_no_results.html": Selectors.NoResultsCard,
		"search_sorry.html":      "",
		"search_soft_block.html": "",
	} {
		resp := testutil.ResponseFromFixture(t, fixture)
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			t.Fatalf("parse fixture: %v", err)
		}
		got := ""
		for _, selector := range searchReadySelectors() {
			if doc.Find(selector).Length() > 0 {
				got = selector
				break
			}
		}
		if got != want {
			t.Errorf("%s: first ready selector %q, want %q", fixture, got, want)
		}
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
			t.Fatalf("selector %q not found in %s - update selectors.go", selector, fixture)
		} else {
			t.Fatalf("selector %q unexpectedly present in %s", selector, fixture)
		}
	}
}

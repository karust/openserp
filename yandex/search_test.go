//go:build integration
// +build integration

package yandex

import (
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

func createTestBrowser(t *testing.T) *core.Browser {
	t.Helper()
	opts := core.BrowserOpts{IsHeadless: false, IsLeakless: false, Timeout: time.Second * 15, LeavePageOpen: true}
	b, err := core.NewBrowser(opts)
	if err != nil {
		t.Fatalf("failed to create test browser: %v", err)
	}
	return b
}

func TestSearchYandex(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := createTestBrowser(t)
	yand := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "HEY", Limit: 10}
	results, err := yand.Search(query)
	if err != nil {
		if err == core.ErrSearchTimeout || err == core.ErrCaptcha {
			t.Skipf("skipping unstable live yandex search result: %v", err)
		}
		t.Fatalf("Cannot [SearchYandex]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[SearchYandex] returned empty result")
	}
}

func TestImageYandex(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := createTestBrowser(t)
	yand := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "furry tiger", Limit: 30}
	results, err := yand.SearchImage(query)
	if err != nil {
		t.Fatalf("Cannot [ImageYandex]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[ImageYandex] returned empty result")
	}
}

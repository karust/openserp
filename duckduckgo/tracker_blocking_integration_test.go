//go:build integration
// +build integration

package duckduckgo

import (
	"context"
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchDuckDuckGoWithTrackerBlocking(t *testing.T) {
	testutil.RequireIntegration(t)

	browser, err := core.NewBrowser(core.BrowserOpts{
		IsHeadless:    true,
		Timeout:       30 * time.Second,
		BlockTrackers: true,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer func() { _ = browser.Close() }()

	engine := New(*browser, ithelper.EngineOptions())
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	results, err := engine.Search(ctx, core.Query{Text: "golang programming", Limit: 5})
	ithelper.HandleError(t, "DuckDuckGo search with tracker blocking", err)
	if len(results) == 0 {
		t.Fatal("DuckDuckGo returned no results with tracker blocking")
	}
	for _, result := range results {
		if result.URL == "" || result.Title == "" {
			t.Fatalf("incomplete result with tracker blocking: %+v", result)
		}
	}
}

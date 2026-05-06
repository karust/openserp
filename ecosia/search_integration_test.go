//go:build integration
// +build integration

package ecosia

import (
	"context"
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchEcosia(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	engine := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golang programming", Limit: 10}
	results, err := engine.Search(context.Background(), query)
	ithelper.HandleError(t, "ecosia web search", err)

	if len(results) == 0 {
		t.Fatal("returned empty results")
	}
	if results[0].URL == "" {
		t.Fatal("first result URL is empty")
	}
	if results[0].Title == "" {
		t.Fatal("first result title is empty")
	}
}

func TestImageSearchEcosia(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	engine := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golden retriever puppy", Limit: 10}
	results, err := engine.SearchImage(context.Background(), query)
	ithelper.HandleError(t, "ecosia image search", err)

	if len(results) == 0 {
		t.Fatal("returned empty image results")
	}
	if results[0].URL == "" {
		t.Fatal("first image result URL is empty")
	}
	if results[0].Title == "" {
		t.Fatal("first image result title is empty")
	}
}

//go:build integration
// +build integration

package duckduckgo

import (
	"context"
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchDuckDuckGo(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	ddg := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "wikipedia", Limit: 10}
	results, err := ddg.Search(context.Background(), query)
	ithelper.HandleError(t, "duckduckgo web search", err)

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

func TestImageSearchDuckDuckGo(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	ddg := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golden retriever puppy", Limit: 10}
	results, err := ddg.SearchImage(context.Background(), query)
	ithelper.HandleError(t, "duckduckgo image search", err)

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

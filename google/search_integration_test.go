//go:build integration
// +build integration

package google

import (
	"context"
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchGoogle(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	gogl := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golang programming", Limit: 10}
	results, err := gogl.Search(context.Background(), query)
	ithelper.HandleError(t, "google web search", err)

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

func TestImageSearchGoogle(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	gogl := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golden retriever puppy", Limit: 10}
	results, err := gogl.SearchImage(context.Background(), query)
	ithelper.HandleError(t, "google image search", err)

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

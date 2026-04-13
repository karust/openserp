//go:build integration
// +build integration

package baidu

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchBaidu(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	baid := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golang programming", Limit: 10}
	results, err := baid.Search(query)
	ithelper.HandleError(t, "baidu web search", err)

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

func TestImageSearchBaidu(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	baid := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golden retriever puppy", Limit: 10}
	results, err := baid.SearchImage(query)
	ithelper.HandleError(t, "baidu image search", err)

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

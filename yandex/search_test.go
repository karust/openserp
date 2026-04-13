//go:build integration
// +build integration

package yandex

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchYandex(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	yand := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "HEY", Limit: 10}
	results, err := yand.Search(query)
	ithelper.HandleError(t, "yandex web search", err)

	if len(results) == 0 {
		t.Fatalf("[SearchYandex] returned empty result")
	}
}

func TestImageYandex(t *testing.T) {
	testutil.RequireIntegration(t)

	browser := ithelper.CreateBrowser(t)
	yand := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "furry tiger", Limit: 30}
	results, err := yand.SearchImage(query)
	ithelper.HandleError(t, "yandex image search", err)

	if len(results) == 0 {
		t.Fatalf("[ImageYandex] returned empty result")
	}
}

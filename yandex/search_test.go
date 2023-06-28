package yandex

import (
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

var browser *core.Browser

func init() {
	opts := core.BrowserOpts{IsHeadless: true, IsLeakless: true, Timeout: time.Second * 2, WaitRequests: false}
	browser, _ = core.NewBrowser(opts)
}

func TestSearchYandex(t *testing.T) {

	yand := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "HEY", Limit: 10}
	results, err := yand.Search(query)
	if err != nil {
		t.Fatalf("Cannot [SearchYandex]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[SearchYandex] returned empty result")
	}
}

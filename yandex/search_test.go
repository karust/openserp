package yandex

import (
	"fmt"
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

var browser *core.Browser

func init() {
	opts := core.BrowserOpts{IsHeadless: false, IsLeakless: false, Timeout: time.Second * 5}
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

func TestImageYandex(t *testing.T) {

	yand := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "furry tiger"}
	results, err := yand.SearchImage(query)
	if err != nil {
		t.Fatalf("Cannot [ImageYandex]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[ImageYandex] returned empty result")
	}

	fmt.Println(results)
}

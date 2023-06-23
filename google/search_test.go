package google

import (
	"testing"
	"time"

	"github.com/karust/googler/core"
)

var browser *core.Browser

func init() {
	opts := core.BrowserOpts{IsHeadless: true, IsLeakless: true, Timeout: time.Second * 2, WaitRequests: false}
	browser, _ = core.NewBrowser(opts)
}

func TestSearchGoogle(t *testing.T) {
	gogl := New(*browser)

	query := core.Query{Text: "HEY", Limit: 10}
	results, err := gogl.Search(query)
	if err != nil {
		t.Fatalf("Cannot [SearchGoogle]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[SearchGoogle] returned empty result")
	}
}

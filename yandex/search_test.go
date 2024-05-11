package yandex

import (
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

var browser *core.Browser

func init() {
	opts := core.BrowserOpts{IsHeadless: false, IsLeakless: false, Timeout: time.Second * 15, LeavePageOpen: true}
	browser, _ = core.NewBrowser(opts)
}

// func TestParseImgData(t *testing.T) {

// 	jsonData, _ := os.ReadFile("./testImgData.json")
// 	var obj ImageData
// 	if err := json.Unmarshal(jsonData, &obj); err != nil {
// 		t.Fatal(err)
// 	}

// 	if (len(obj.InitalState.SerpList.Items.Entities)) != 30 {
// 		t.Fail()
// 	}
// }

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

	query := core.Query{Text: "furry tiger", Limit: 30}
	results, err := yand.SearchImage(query)
	if err != nil {
		t.Fatalf("Cannot [ImageYandex]: %s", err)
	}

	if len(results) < 30 {
		t.Fatalf("[ImageYandex] returned empty result")
	}
}

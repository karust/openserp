package baidu

import (
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

var browser *core.Browser
var testQuery = core.Query{Text: "go", Site: "tutorialspoint.com", DateInterval: "20140101..20230101", Limit: 10}

func init() {
	core.InitLogger(true, true)

	opts := core.BrowserOpts{IsHeadless: false, IsLeakless: false, Timeout: time.Second * 10}
	browser, _ = core.NewBrowser(opts)
}

func TestUrlBuild(t *testing.T) {
	res, err := BuildURL(testQuery)
	if err != nil {
		t.Fatal(err)
	}

	want := "https://www.baidu.com/s?f=8&gpc=stf%3D1388534400%2C1672531200%7Cstftype%3D2&ie=utf-8&rn=10&wd=go+site%3Atutorialspoint.com"

	if want != res {
		t.Fatalf("Wanted result `%s` doesn't match to resulted `%s`", want, res)
	}
}

func TestSearch(t *testing.T) {
	baid := New(*browser, core.SearchEngineOptions{})
	results, err := baid.Search(testQuery)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("No results got from Baidu search")
	}
}

func TestImageUrlBuild(t *testing.T) {
	query := core.Query{Text: "金毛猎犬"}

	got, err := BuildImageURL(query, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := "https://image.baidu.com/search/acjson?cl=2&fp=result&ie=utf-8&ipn=rj&oe=utf-8&pn=0&rn=30&tn=resultjson_com&word=%E9%87%91%E6%AF%9B%E7%8C%8E%E7%8A%AC"
	if want != got {
		t.Fatalf("Want: `%s`, Got `%s`", want, got)
	}
}

func TestImageSearch(t *testing.T) {
	baid := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "each each data", Limit: 60}
	results, err := baid.SearchImage(query)
	if err != nil {
		t.Fatalf("Cannot [ImageBaidu]: %s", err)
	}

	if len(results) < 60 {
		t.Fatalf("[ImageBaidu] returned not full result")
	}
}

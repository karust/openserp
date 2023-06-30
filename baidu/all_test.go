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

	opts := core.BrowserOpts{IsHeadless: true, IsLeakless: false, Timeout: time.Second * 2, WaitRequests: false}
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

// func TestSearchRaw(t *testing.T) {
// 	results, err := Search(testQuery)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	fmt.Println(results)
// }

func TestSearchBaidu(t *testing.T) {
	baid := New(*browser, core.SearchEngineOptions{})
	results, err := baid.Search(testQuery)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("No results got from Baidu search")
	}
}

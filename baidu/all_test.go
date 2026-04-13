package baidu

import (
	"net/url"
	"testing"

	"github.com/karust/openserp/core"
)

var testQuery = core.Query{Text: "go", Site: "tutorialspoint.com", DateInterval: "20140101..20230101", Limit: 10}

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

func TestImageUrlBuild(t *testing.T) {
	query := core.Query{Text: "金毛猎犬"}

	got, err := BuildImageURL(query, 0)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("invalid URL returned: %v", err)
	}
	if parsed.Host != "image.baidu.com" {
		t.Fatalf("unexpected host: %s", parsed.Host)
	}
	q := parsed.Query()
	if q.Get("word") != "金毛猎犬" {
		t.Fatalf("expected word query to be preserved, got %q", q.Get("word"))
	}
	if q.Get("tn") != "resultjson_com" {
		t.Fatalf("expected tn=resultjson_com, got %q", q.Get("tn"))
	}
}

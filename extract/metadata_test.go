package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestPublicationMetadata(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><meta property="og:site_name" content="Example"><meta property="article:published_time" content="2026-10-12T08:00:00Z"></head><body><time datetime="2026-10-13">Yesterday</time></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	meta := parseMetadata(doc, "https://example.org")
	for key, want := range map[string]string{"og:site_name": "Example", "article:published_time": "2026-10-12T08:00:00Z", "time:datetime": "2026-10-13"} {
		if meta.OGTags[key] != want {
			t.Errorf("%s=%q", key, meta.OGTags[key])
		}
	}
}

package google

import (
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

var browser *core.Browser

func init() {
	opts := core.BrowserOpts{IsHeadless: true, IsLeakless: false, Timeout: time.Second * 5, LeavePageOpen: false}
	browser, _ = core.NewBrowser(opts)
}

func TestSearchGoogle(t *testing.T) {
	gogl := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "HEY", Limit: 10}
	results, err := gogl.Search(query)
	if err != nil {
		t.Fatalf("Cannot [SearchGoogle]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[SearchGoogle] returned empty result")
	}
}

func TestParseSourceImageURL(t *testing.T) {
	//href1 := `/imgres?imgurl=https%3A%2F%2Fupload.wikimedia.org%2Fwikipedia%2Fcommons%2F2%2F26%2FMarmota_marmota_Alpes2.jpg&amp;tbnid=Be_RycOe8xzlpM&amp;vet=12ahUKEwjkh6WzwIeAAxWV_yoKHRzHC9wQMygAegUIARD0AQ..i&amp;imgrefurl=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FAlpine_marmot&amp;docid=7miWbc2QiSw9uM&amp;w=801&amp;h=599&amp;q=alpine%20marmot&amp;ved=2ahUKEwjkh6WzwIeAAxWV_yoKHRzHC9wQMygAegUIARD0AQ`
	href2 := `/imgres?imgurl=https%3A%2F%2Fstatic.wikia.nocookie.net%2Fnaturerules1%2Fimages%2Ff%2Ff2%2F13d79d934ccf6f7919777fcb6dbb6e6c.jpg%2Frevision%2Flatest%3Fcb%3D20210218225522&tbnid=JxC8NUyBjdNbdM&vet=12ahUKEwiHrJnN1YeAAxXvEBAIHfRADAAQMygCegUIARD4AQ..i&imgrefurl=https%3A%2F%2Fnaturerules1.fandom.com%2Fwiki%2FAlpine_Marmot&docid=XXYeDjL67badNM&w=1600&h=1200&q=alpine%20marmot&ved=2ahUKEwiHrJnN1YeAAxXvEBAIHfRADAAQMygCegUIARD4AQ`
	want := SourceImage{
		OriginalURL: "https://static.wikia.nocookie.net/naturerules1/images/f/f2/13d79d934ccf6f7919777fcb6dbb6e6c.jpg/revision/latest?cb=20210218225522",
		PageURL:     "https://naturerules1.fandom.com/wiki/Alpine_Marmot",
		Width:       "1600",
		Height:      "1200",
	}

	got, err := parseSourceImageURL(href2)
	if err != nil {
		t.Fatal(err)
	}

	if want != got {
		t.Fatalf("Want: %v, Got: %v", want, got)
	}
}

func TestImageSearch(t *testing.T) {
	gogl := New(*browser, core.SearchEngineOptions{})
	query := core.Query{Text: "Ferrari Testarossa", Limit: 77}
	results, err := gogl.SearchImage(query)

	if err != nil {
		t.Fatalf("Cannot search images: %s", err)
	}

	if len(results) < 77 {
		t.Fatalf("Returned not full result")
	}

	if results[0].URL == "" {
		t.Fatalf("First result doesn't contain URL, %v+", results[0])
	}
}

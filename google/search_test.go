package google

import (
	"net/url"
	"testing"

	"github.com/karust/openserp/core"
)

func TestBuildSearchURL(t *testing.T) {
	tests := []struct {
		name    string
		query   core.Query
		wantErr bool
		check   func(*testing.T, url.Values, string)
	}{
		{
			name: "combined params with unicode and start zero",
			query: core.Query{
				Text:         "golang тест",
				Site:         "example.com",
				Filetype:     "pdf",
				DateInterval: "20240101..20240131",
				Limit:        50,
				Start:        0,
				Filter:       false,
				LangCode:     "RU",
			},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.google.ru" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("q"); got != "golang тест site:example.com filetype:pdf" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("oq"); got != params.Get("q") {
					t.Fatalf("oq should match q, got %q vs %q", got, params.Get("q"))
				}
				if got := params.Get("tbs"); got != "cdr:1,cd_min:20240101,cd_max:20240131" {
					t.Fatalf("unexpected tbs: %q", got)
				}
				if got := params.Get("num"); got != "50" {
					t.Fatalf("unexpected num: %q", got)
				}
				if got := params.Get("start"); got != "" {
					t.Fatalf("start should be omitted when Start=0, got %q", got)
				}
				if got := params.Get("filter"); got != "0" {
					t.Fatalf("unexpected filter value: %q", got)
				}
				if got := params.Get("hl"); got != "ru" {
					t.Fatalf("unexpected hl value: %q", got)
				}
				if got := params.Get("gl"); got != "ru" {
					t.Fatalf("unexpected gl value: %q", got)
				}
				if got := params.Get("lr"); got != "lang_ru" {
					t.Fatalf("unexpected lr value: %q", got)
				}
				if got := params.Get("pws"); got != "0" {
					t.Fatalf("unexpected pws value: %q", got)
				}
				if got := params.Get("nfpr"); got != "1" {
					t.Fatalf("unexpected nfpr value: %q", got)
				}
			},
		},
		{
			name: "german language sets matching google locale params",
			query: core.Query{
				Text:     "megadeth tickets",
				Filter:   true,
				LangCode: "DE",
			},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.google.de" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("hl"); got != "de" {
					t.Fatalf("unexpected hl value: %q", got)
				}
				if got := params.Get("gl"); got != "de" {
					t.Fatalf("unexpected gl value: %q", got)
				}
				if got := params.Get("lr"); got != "lang_de" {
					t.Fatalf("unexpected lr value: %q", got)
				}
			},
		},
		{
			name: "site and filetype without text",
			query: core.Query{
				Site:     "example.com",
				Filetype: "txt",
				Filter:   true,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("q"); got != " site:example.com filetype:txt" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("filter"); got != "" {
					t.Fatalf("filter should be omitted when Filter=true, got %q", got)
				}
			},
		},
		{
			name: "very large start",
			query: core.Query{
				Text:   "golang",
				Start:  2147483647,
				Filter: true,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("start"); got != "2147483647" {
					t.Fatalf("unexpected start value: %q", got)
				}
			},
		},
		{
			name: "negative start returns error",
			query: core.Query{
				Text:  "golang",
				Start: -1,
			},
			wantErr: true,
		},
		{
			name:    "empty query returns error",
			query:   core.Query{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BuildURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("invalid URL returned: %v", err)
			}
			if tt.check != nil {
				tt.check(t, parsed.Query(), parsed.Host)
			}
		})
	}
}

func TestBuildImageSearchURL(t *testing.T) {
	tests := []struct {
		name    string
		query   core.Query
		wantErr bool
		check   func(*testing.T, url.Values, string)
	}{
		{
			name: "combined params with unicode",
			query: core.Query{
				Text:         "горы",
				Site:         "example.com",
				Filetype:     "jpg",
				DateInterval: "20240301..20240315",
				Limit:        25,
				LangCode:     "EN",
			},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.google.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("tbm"); got != "isch" {
					t.Fatalf("unexpected tbm: %q", got)
				}
				if got := params.Get("q"); got != "горы site:example.com filetype:jpg" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("oq"); got != params.Get("q") {
					t.Fatalf("oq should match q, got %q vs %q", got, params.Get("q"))
				}
				if got := params.Get("num"); got != "25" {
					t.Fatalf("unexpected num value: %q", got)
				}
				if got := params.Get("tbs"); got != "cdr:1,cd_min:20240301,cd_max:20240315" {
					t.Fatalf("unexpected tbs value: %q", got)
				}
				if got := params.Get("hl"); got != "en" {
					t.Fatalf("unexpected hl value: %q", got)
				}
				if got := params.Get("gl"); got != "us" {
					t.Fatalf("unexpected gl value: %q", got)
				}
				if got := params.Get("lr"); got != "lang_en" {
					t.Fatalf("unexpected lr value: %q", got)
				}
			},
		},
		{
			name: "invalid date interval returns error",
			query: core.Query{
				Text:         "golang",
				DateInterval: "20240101",
			},
			wantErr: true,
		},
		{
			name: "empty query returns error",
			query: core.Query{
				Text: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildImageURL(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BuildImageURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("invalid URL returned: %v", err)
			}
			if tt.check != nil {
				tt.check(t, parsed.Query(), parsed.Host)
			}
		})
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

func TestSolveCaptchaWithoutConfiguredSolverReturnsFalse(t *testing.T) {
	gogl := New(core.Browser{}, core.SearchEngineOptions{})
	if got := gogl.solveCaptcha(nil, "sitekey", "datas", ""); got {
		t.Fatal("expected solveCaptcha to fail without solver/page context")
	}
}

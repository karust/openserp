package bing

import (
	"net/url"
	"testing"

	"github.com/karust/openserp/core"
)

func TestBuildURL(t *testing.T) {
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
				LangCode:     "RU",
				Limit:        30,
				Start:        0,
			},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.bing.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("q"); got != "golang тест site:example.com filetype:pdf" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("pq"); got != params.Get("q") {
					t.Fatalf("pq should match q, got %q vs %q", got, params.Get("q"))
				}
				if got := params.Get("setlang"); got != "ru" {
					t.Fatalf("unexpected setlang value: %q", got)
				}
				if got := params.Get("mkt"); got != "ru-RU" {
					t.Fatalf("unexpected mkt value: %q", got)
				}
				if got := params.Get("cc"); got != "RU" {
					t.Fatalf("unexpected cc value: %q", got)
				}
				if got := params.Get("filters"); got != `ex1:"ez5_19723_19753"` {
					t.Fatalf("unexpected filters value: %q", got)
				}
				if got := params.Get("count"); got != "30" {
					t.Fatalf("unexpected count value: %q", got)
				}
				if got := params.Get("first"); got != "" {
					t.Fatalf("first should be omitted when Start=0, got %q", got)
				}
				if got := params.Get("form"); got != "QBLH" {
					t.Fatalf("unexpected form value: %q", got)
				}
				if got := params.Get("qs"); got != "HS" {
					t.Fatalf("unexpected qs value: %q", got)
				}
				if got := params.Get("sp"); got != "-1" {
					t.Fatalf("unexpected sp value: %q", got)
				}
			},
		},
		{
			name: "date operators in text are converted to filters",
			query: core.Query{
				Text:  "megadeth tickets after:2026-01-01 before:2026-04-27",
				Limit: 10,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("q"); got != "megadeth tickets" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("pq"); got != "megadeth tickets" {
					t.Fatalf("unexpected pq value: %q", got)
				}
				if got := params.Get("filters"); got != `ex1:"ez5_20454_20570"` {
					t.Fatalf("unexpected filters value: %q", got)
				}
				if got := params.Get("count"); got != "" {
					t.Fatalf("count should be omitted when Limit<=10, got %q", got)
				}
				// LangCode unset → no locale params; let Bing pick defaults
				// from the request rather than biasing toward en-US.
				for _, key := range []string{"mkt", "setlang", "cc"} {
					if got := params.Get(key); got != "" {
						t.Fatalf("expected %s to be empty, got %q", key, got)
					}
				}
			},
		},
		{
			name: "date param overrides date operators in text",
			query: core.Query{
				Text:         "megadeth tickets after:2026-01-01 before:2026-04-27",
				DateInterval: "20240101..20240131",
				LangCode:     "en-DE",
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("q"); got != "megadeth tickets" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("filters"); got != `ex1:"ez5_19723_19753"` {
					t.Fatalf("unexpected filters value: %q", got)
				}
				if got := params.Get("mkt"); got != "en-DE" {
					t.Fatalf("unexpected mkt value: %q", got)
				}
				if got := params.Get("setlang"); got != "en" {
					t.Fatalf("unexpected setlang value: %q", got)
				}
				if got := params.Get("cc"); got != "DE" {
					t.Fatalf("unexpected cc value: %q", got)
				}
			},
		},
		{
			name: "region overrides bing market country",
			query: core.Query{
				Text:     "weather",
				LangCode: "en",
				Region:   "DE",
				Limit:    10,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("mkt"); got != "en-DE" {
					t.Fatalf("unexpected mkt value: %q", got)
				}
				if got := params.Get("setlang"); got != "en" {
					t.Fatalf("unexpected setlang value: %q", got)
				}
				if got := params.Get("cc"); got != "DE" {
					t.Fatalf("unexpected cc value: %q", got)
				}
			},
		},
		{
			name: "region only sets bing country",
			query: core.Query{
				Text:   "weather",
				Region: "RU",
				Limit:  10,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("cc"); got != "RU" {
					t.Fatalf("unexpected cc value: %q", got)
				}
				if got := params.Get("mkt"); got != "" {
					t.Fatalf("mkt should be omitted without language, got %q", got)
				}
				if got := params.Get("setlang"); got != "" {
					t.Fatalf("setlang should be omitted without language, got %q", got)
				}
			},
		},
		{
			name: "very large start",
			query: core.Query{
				Text:  "golang",
				Start: 2147483647,
				Limit: 20,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("first"); got != "2147483648" {
					t.Fatalf("unexpected first value: %q", got)
				}
				if got := params.Get("count"); got != "" {
					t.Fatalf("count should be omitted when first is used, got %q", got)
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
			name: "reversed date interval returns error",
			query: core.Query{
				Text:         "golang",
				DateInterval: "20240131..20240101",
			},
			wantErr: true,
		},
		{
			name:    "empty fields return error",
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
				t.Fatalf("BuildURL() returned invalid URL: %v", err)
			}
			if tt.check != nil {
				tt.check(t, parsed.Query(), parsed.Host)
			}
		})
	}
}

func TestBuildImageURL(t *testing.T) {
	tests := []struct {
		name    string
		query   core.Query
		wantErr bool
		check   func(*testing.T, url.Values, string)
	}{
		{
			name:  "basic image query",
			query: core.Query{Text: "test"},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.bing.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("q"); got != "test" {
					t.Fatalf("unexpected q value: %q", got)
				}
			},
		},
		{
			name: "combined params with unicode",
			query: core.Query{
				Text:     "коты",
				Site:     "example.com",
				Filetype: "png",
				LangCode: "EN",
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("q"); got != "коты site:example.com" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("setlang"); got != "en" {
					t.Fatalf("unexpected setlang value: %q", got)
				}
				if got := params.Get("mkt"); got != "en-US" {
					t.Fatalf("unexpected mkt value: %q", got)
				}
				if got := params.Get("cc"); got != "US" {
					t.Fatalf("unexpected cc value: %q", got)
				}
				if got := params.Get("form"); got != "HDRSC2" {
					t.Fatalf("unexpected form value: %q", got)
				}
				if got := params.Get("first"); got != "1" {
					t.Fatalf("unexpected first value: %q", got)
				}
				if got := params.Get("scenario"); got != "ImageBasicHover" {
					t.Fatalf("unexpected scenario value: %q", got)
				}
			},
		},
		{
			name: "image region overrides market country",
			query: core.Query{
				Text:     "cats",
				LangCode: "en",
				Region:   "GB",
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("mkt"); got != "en-GB" {
					t.Fatalf("unexpected mkt value: %q", got)
				}
				if got := params.Get("cc"); got != "GB" {
					t.Fatalf("unexpected cc value: %q", got)
				}
			},
		},
		{
			name:    "empty fields return error",
			query:   core.Query{},
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
				t.Fatalf("BuildImageURL() returned invalid URL: %v", err)
			}
			if tt.check != nil {
				tt.check(t, parsed.Query(), parsed.Host)
			}
		})
	}
}

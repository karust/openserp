package duckduckgo

import (
	"net/url"
	"testing"

	"github.com/karust/openserp/core"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name    string
		query   core.Query
		page    int
		wantErr bool
		check   func(*testing.T, url.Values, string)
	}{
		{
			name:  "basic search",
			query: core.Query{Text: "golang programming"},
			page:  0,
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "duckduckgo.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("q"); got != "golang programming" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("ia"); got != "web" {
					t.Fatalf("unexpected ia value: %q", got)
				}
				if got := params.Get("t"); got != "h" {
					t.Fatalf("unexpected t value: %q", got)
				}
				if got := params.Get("s"); got != "" {
					t.Fatalf("s should be omitted on first page, got %q", got)
				}
			},
		},
		{
			name: "combined params with unicode",
			query: core.Query{
				Text:         "поиск",
				Site:         "github.com",
				Filetype:     "pdf",
				LangCode:     "RU",
				DateInterval: "20240101..20240131",
			},
			page: 0,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("q"); got != "поиск site:github.com filetype:pdf" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("df"); got != "2024-01-01..2024-01-31" {
					t.Fatalf("unexpected df value: %q", got)
				}
				if got := params.Get("kl"); got != "ru-ru" {
					t.Fatalf("unexpected kl value: %q", got)
				}
			},
		},
		{
			name: "region overrides duckduckgo market",
			query: core.Query{
				Text:     "weather",
				LangCode: "de",
				Region:   "AT",
			},
			page: 0,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("kl"); got != "at-de" {
					t.Fatalf("unexpected kl value: %q", got)
				}
			},
		},
		{
			name:  "very large page offset",
			query: core.Query{Text: "golang"},
			page:  100000,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("s"); got != "2500000" {
					t.Fatalf("unexpected pagination offset: %q", got)
				}
			},
		},
		{
			name:  "negative page does not add offset",
			query: core.Query{Text: "golang"},
			page:  -1,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("s"); got != "" {
					t.Fatalf("expected empty offset for negative page, got %q", got)
				}
			},
		},
		{
			name: "invalid date interval returns error",
			query: core.Query{
				Text:         "golang",
				DateInterval: "invalid",
			},
			page:    0,
			wantErr: true,
		},
		{
			name:    "empty query returns error",
			query:   core.Query{},
			page:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(tt.query, tt.page)
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
			name: "combined params with unicode",
			query: core.Query{
				Text:         "горы",
				Site:         "example.com",
				Filetype:     "jpg",
				LangCode:     "RU",
				DateInterval: "20240201..20240229",
			},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "duckduckgo.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("q"); got != "горы site:example.com filetype:jpg" {
					t.Fatalf("unexpected q value: %q", got)
				}
				if got := params.Get("iax"); got != "images" || params.Get("ia") != "images" {
					t.Fatalf("expected image mode params, got iax=%q ia=%q", params.Get("iax"), params.Get("ia"))
				}
				if got := params.Get("df"); got != "2024-02-01..2024-02-29" {
					t.Fatalf("unexpected df value: %q", got)
				}
				if got := params.Get("kl"); got != "ru-ru" {
					t.Fatalf("unexpected kl value: %q", got)
				}
			},
		},
		{
			name: "invalid date interval returns error",
			query: core.Query{
				Text:         "golang",
				DateInterval: "202401",
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

func TestDuckDuckGoLanguageMapping(t *testing.T) {
	tests := []struct {
		name     string
		langCode string
		region   string
		wantKL   string
	}{
		{
			name:     "language only maps to default region",
			langCode: "DE",
			wantKL:   "de-de",
		},
		{
			name:     "regional language maps to duckduckgo region",
			langCode: "de-AT",
			wantKL:   "at-de",
		},
		{
			name:     "region param overrides language default",
			langCode: "de",
			region:   "CH",
			wantKL:   "ch-de",
		},
		{
			name:     "unknown language omits kl",
			langCode: "xx",
			wantKL:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := core.Query{Text: "golang", LangCode: tt.langCode, Region: tt.region}
			got, err := BuildURL(query, 0)
			if err != nil {
				t.Fatalf("BuildURL() error = %v", err)
			}

			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("BuildURL() returned invalid URL: %v", err)
			}

			if gotKL := parsed.Query().Get("kl"); gotKL != tt.wantKL {
				t.Fatalf("unexpected kl value: %q", gotKL)
			}
		})
	}
}

func TestShouldFetchDuckDuckGoPage(t *testing.T) {
	results := func(organic, ads int) []core.SearchResult {
		out := make([]core.SearchResult, 0, organic+ads)
		for i := 0; i < ads; i++ {
			out = append(out, core.SearchResult{URL: "https://ad.example/" + string(rune('a'+i)), Ad: true})
		}
		for i := 0; i < organic; i++ {
			out = append(out, core.SearchResult{URL: "https://example.com/" + string(rune('a'+i))})
		}
		return out
	}

	tests := []struct {
		name         string
		results      []core.SearchResult
		limit        int
		pagesFetched int
		want         bool
	}{
		{
			name:         "first page is always fetched",
			limit:        10,
			pagesFetched: 0,
			want:         true,
		},
		{
			name:         "default limit does not chase a short first page",
			results:      results(8, 2),
			limit:        10,
			pagesFetched: 1,
			want:         false,
		},
		{
			name:         "explicit larger limit can paginate",
			results:      results(8, 0),
			limit:        11,
			pagesFetched: 1,
			want:         true,
		},
		{
			name:         "satisfied larger limit stops",
			results:      results(11, 0),
			limit:        11,
			pagesFetched: 1,
			want:         false,
		},
		{
			name:         "unset internal query stops after first page",
			results:      results(8, 0),
			limit:        0,
			pagesFetched: 1,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := core.ShouldFetchResultPage(core.CountOrganicResults(tt.results), tt.limit, tt.pagesFetched)
			if got != tt.want {
				t.Fatalf("ShouldFetchResultPage() = %t, want %t", got, tt.want)
			}
		})
	}
}

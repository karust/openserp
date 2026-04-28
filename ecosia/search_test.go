package ecosia

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
			name:  "basic search omits page param on page 0",
			query: core.Query{Text: "llm tool use"},
			page:  0,
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.ecosia.org" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("q"); got != "llm tool use" {
					t.Fatalf("unexpected q: %q", got)
				}
				if got := params.Get("method"); got != "index" {
					t.Fatalf("unexpected method: %q", got)
				}
				if got := params.Get("p"); got != "" {
					t.Fatalf("expected no p param on page 0, got %q", got)
				}
			},
		},
		{
			name: "site, filetype and pagination",
			query: core.Query{
				Text:     "поиск",
				Site:     "github.com",
				Filetype: "pdf",
				LangCode: "de-AT",
			},
			page: 2,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("q"); got != "поиск site:github.com filetype:pdf" {
					t.Fatalf("unexpected q: %q", got)
				}
				if got := params.Get("p"); got != "2" {
					t.Fatalf("unexpected p: %q", got)
				}
				if got := params.Get("mkt"); got != "" {
					t.Fatalf("unexpected mkt: %q", got)
				}
			},
		},
		{
			name:  "5-day span buckets to week",
			query: core.Query{Text: "golang", DateInterval: "20240101..20240106"},
			page:  0,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("freshness"); got != "week" {
					t.Fatalf("unexpected freshness: %q", got)
				}
			},
		},
		{
			name:  "long span is silently dropped",
			query: core.Query{Text: "golang", DateInterval: "20240101..20240601"},
			page:  0,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("freshness"); got != "" {
					t.Fatalf("freshness should not be set for spans > 31d, got %q", got)
				}
			},
		},
		{
			name:    "non-spec keyword input errors",
			query:   core.Query{Text: "golang", DateInterval: "week"},
			wantErr: true,
		},
		{
			name:    "malformed date errors",
			query:   core.Query{Text: "golang", DateInterval: "2024-01-01..2024-01-31"},
			wantErr: true,
		},
		{
			name:    "end before start errors",
			query:   core.Query{Text: "golang", DateInterval: "20240131..20240101"},
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
	got, err := BuildImageURL(core.Query{Text: "trees"}, 0)
	if err != nil {
		t.Fatalf("BuildImageURL() error = %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}
	if parsed.Host != "www.ecosia.org" || parsed.Path != "/images" {
		t.Fatalf("expected /images endpoint, got %s%s", parsed.Host, parsed.Path)
	}
	if parsed.Query().Get("q") != "trees" {
		t.Fatalf("unexpected q: %q", parsed.Query().Get("q"))
	}
	if got := parsed.Query().Get("imageType"); got != "" {
		t.Fatalf("expected no imageType when Filetype unset, got %q", got)
	}
	if got := parsed.Query().Get("p"); got != "" {
		t.Fatalf("expected no p param on page 0, got %q", got)
	}

	if _, err := BuildImageURL(core.Query{}, 0); err == nil {
		t.Fatalf("expected error for empty query")
	}
}

func TestBuildImageURLPagination(t *testing.T) {
	got, err := BuildImageURL(core.Query{Text: "trees"}, 2)
	if err != nil {
		t.Fatalf("BuildImageURL() error = %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}
	if g := parsed.Query().Get("p"); g != "2" {
		t.Fatalf("expected p=2, got %q", g)
	}
}

func TestBuildImageURLFreshness(t *testing.T) {
	got, err := BuildImageURL(core.Query{Text: "trees", DateInterval: "20240101..20240101"}, 0)
	if err != nil {
		t.Fatalf("BuildImageURL() err = %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}
	if g := parsed.Query().Get("freshness"); g != "day" {
		t.Fatalf("expected freshness=day, got %q", g)
	}
}

func TestEcosiaFreshness(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty input is no-op", input: "", want: ""},
		{name: "same day", input: "20240101..20240101", want: "day"},
		{name: "1-day span hits day boundary", input: "20240101..20240102", want: "day"},
		{name: "2-day span buckets to week", input: "20240101..20240103", want: "week"},
		{name: "7-day span hits week boundary", input: "20240101..20240108", want: "week"},
		{name: "8-day span buckets to month", input: "20240101..20240109", want: "month"},
		{name: "31-day span hits month boundary", input: "20240101..20240201", want: "month"},
		{name: "32-day span is dropped", input: "20240101..20240202", want: ""},
		{name: "very long span is dropped", input: "20200101..20240101", want: ""},
		{name: "missing separator errors", input: "20240101", wantErr: true},
		{name: "wrong format errors", input: "2024-01-01..2024-01-31", wantErr: true},
		{name: "non-numeric errors", input: "abcdefgh..ijklmnop", wantErr: true},
		{name: "end before start errors", input: "20240131..20240101", wantErr: true},
		{name: "keyword input errors", input: "week", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ecosiaFreshness(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ecosiaFreshness(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("ecosiaFreshness(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildImageURLImageType(t *testing.T) {
	tests := []struct {
		name     string
		filetype string
		want     string
		wantErr  bool
	}{
		{name: "clipart", filetype: "clipart", want: "clipart"},
		{name: "uppercase normalized", filetype: "PHOTO", want: "photo"},
		{name: "animatedgif", filetype: "animatedgif", want: "animatedgif"},
		{name: "rejects extension", filetype: "gif", wantErr: true},
		{name: "rejects unknown", filetype: "vector", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildImageURL(core.Query{Text: "trees", Filetype: tt.filetype}, 0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BuildImageURL() err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("invalid URL: %v", err)
			}
			if g := parsed.Query().Get("imageType"); g != tt.want {
				t.Fatalf("unexpected imageType: %q want %q", g, tt.want)
			}
		})
	}
}

func TestStartPage(t *testing.T) {
	tests := []struct {
		name     string
		start    int
		wantPage int
		wantRank int
		wantErr  bool
	}{
		{name: "zero offset starts at page 0", start: 0, wantPage: 0, wantRank: 1},
		{name: "page-aligned offset 10", start: 10, wantPage: 1, wantRank: 11},
		{name: "page-aligned offset 50", start: 50, wantPage: 5, wantRank: 51},
		{name: "off-grid offset rounds down", start: 15, wantPage: 1, wantRank: 11},
		{name: "off-grid offset 24 rounds to page 2", start: 24, wantPage: 2, wantRank: 21},
		{name: "negative offset errors", start: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, rank, err := startPage(tt.start)
			if (err != nil) != tt.wantErr {
				t.Fatalf("startPage(%d) err = %v, wantErr %v", tt.start, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if page != tt.wantPage || rank != tt.wantRank {
				t.Fatalf("startPage(%d) = (%d, %d), want (%d, %d)",
					tt.start, page, rank, tt.wantPage, tt.wantRank)
			}
		})
	}
}

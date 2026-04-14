package baidu

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
				Text:         "go 搜索",
				Site:         "tutorialspoint.com",
				Filetype:     "pdf",
				DateInterval: "20140101..20230101",
				Limit:        10,
				Start:        0,
			},
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "www.baidu.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("wd"); got != "go 搜索 site:tutorialspoint.com filetype:pdf" {
					t.Fatalf("unexpected wd value: %q", got)
				}
				if got := params.Get("gpc"); got != "stf=1388534400,1672531200|stftype=2" {
					t.Fatalf("unexpected gpc value: %q", got)
				}
				if got := params.Get("rn"); got != "10" {
					t.Fatalf("unexpected rn value: %q", got)
				}
				if got := params.Get("pn"); got != "" {
					t.Fatalf("pn should be omitted when Start=0, got %q", got)
				}
				if got := params.Get("f"); got != "8" {
					t.Fatalf("unexpected f value: %q", got)
				}
				if got := params.Get("ie"); got != "utf-8" {
					t.Fatalf("unexpected ie value: %q", got)
				}
			},
		},
		{
			name: "very large start",
			query: core.Query{
				Text:  "golang",
				Start: 2147483647,
			},
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("pn"); got != "2147483647" {
					t.Fatalf("unexpected pn value: %q", got)
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
				t.Fatalf("invalid URL returned: %v", err)
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
		pageNum int
		wantErr bool
		check   func(*testing.T, url.Values, string)
	}{
		{
			name:    "unicode query",
			query:   core.Query{Text: "金毛猎犬"},
			pageNum: 0,
			check: func(t *testing.T, params url.Values, host string) {
				t.Helper()
				if host != "image.baidu.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if got := params.Get("word"); got != "金毛猎犬" {
					t.Fatalf("expected unicode query, got %q", got)
				}
				if got := params.Get("tn"); got != "resultjson_com" {
					t.Fatalf("unexpected tn value: %q", got)
				}
			},
		},
		{
			name: "combined params with pagination",
			query: core.Query{
				Text:  "golang",
				Limit: 25,
			},
			pageNum: 3,
			check: func(t *testing.T, params url.Values, _ string) {
				t.Helper()
				if got := params.Get("rn"); got != "30" {
					t.Fatalf("unexpected rn value: %q", got)
				}
				if got := params.Get("pn"); got != "90" {
					t.Fatalf("unexpected pn value: %q", got)
				}
			},
		},
		{
			name:    "empty fields return error",
			query:   core.Query{},
			pageNum: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildImageURL(tt.query, tt.pageNum)
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

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
		wantErr bool
	}{
		{
			name:  "basic search",
			query: core.Query{Text: "golang programming"},
		},
		{
			name:  "search with site filter",
			query: core.Query{Text: "golang", Site: "github.com"},
		},
		{
			name:  "search with filetype",
			query: core.Query{Text: "documentation", Filetype: "pdf"},
		},
		{
			name:    "empty query",
			query:   core.Query{Text: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(tt.query, 0)
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
			params := parsed.Query()
			if params.Get("q") == "" {
				t.Fatalf("BuildURL() should include q parameter, got %s", got)
			}
			if params.Get("ia") != "web" {
				t.Fatalf("BuildURL() should include ia=web, got %s", got)
			}
			if params.Get("t") != "h" {
				t.Fatalf("BuildURL() should include t=h, got %s", got)
			}
		})
	}
}

func TestBuildImageURL(t *testing.T) {
	tests := []struct {
		name    string
		query   core.Query
		wantErr bool
	}{
		{
			name:  "basic image search",
			query: core.Query{Text: "golang logo"},
		},
		{
			name:    "empty query",
			query:   core.Query{Text: ""},
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
			params := parsed.Query()
			if params.Get("q") == "" {
				t.Fatalf("BuildImageURL() should include q parameter, got %s", got)
			}
			if params.Get("iax") != "images" || params.Get("ia") != "images" {
				t.Fatalf("BuildImageURL() should target image mode, got %s", got)
			}
		})
	}
}

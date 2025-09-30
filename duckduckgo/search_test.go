package duckduckgo

import (
	"testing"

	"github.com/karust/openserp/core"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name     string
		query    core.Query
		expected string
		wantErr  bool
	}{
		{
			name: "Basic search",
			query: core.Query{
				Text: "golang programming",
			},
			expected: "https://duckduckgo.com/?q=golang+programming&t=h&ia=web",
			wantErr:  false,
		},
		{
			name: "Search with site filter",
			query: core.Query{
				Text: "golang",
				Site: "github.com",
			},
			expected: "https://duckduckgo.com/?q=golang+site%3Agithub.com&t=h&ia=web",
			wantErr:  false,
		},
		{
			name: "Search with filetype",
			query: core.Query{
				Text:     "documentation",
				Filetype: "pdf",
			},
			expected: "https://duckduckgo.com/?q=documentation+filetype%3Apdf&t=h&ia=web",
			wantErr:  false,
		},
		{
			name: "Empty query",
			query: core.Query{
				Text: "",
			},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("BuildURL() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildImageURL(t *testing.T) {
	tests := []struct {
		name     string
		query    core.Query
		expected string
		wantErr  bool
	}{
		{
			name: "Basic image search",
			query: core.Query{
				Text: "golang logo",
			},
			expected: "https://duckduckgo.com/?q=golang+logo&t=h&iax=images&ia=images",
			wantErr:  false,
		},
		{
			name: "Empty query",
			query: core.Query{
				Text: "",
			},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildImageURL(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildImageURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("BuildImageURL() = %v, want %v", got, tt.expected)
			}
		})
	}
}

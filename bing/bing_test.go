package bing

import (
	"net/url"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

func TestBuildImageURL(t *testing.T) {
	tests := []struct {
		name     string
		query    core.Query
		wantErr  bool
		wantCont string
	}{
		{
			name:     "basic image query",
			query:    core.Query{Text: "test"},
			wantErr:  false,
			wantCont: "q=test",
		},
		{
			name:     "image query with site",
			query:    core.Query{Text: "cats", Site: "example.com"},
			wantErr:  false,
			wantCont: "q=cats+site%3Aexample.com",
		},
		{
			name:     "image query with filetype",
			query:    core.Query{Text: "dogs", Filetype: "png"},
			wantErr:  false,
			wantCont: "q=dogs",
		},
		{
			name:     "empty query",
			query:    core.Query{Text: ""},
			wantErr:  true,
			wantCont: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildImageURL(tt.query)

			if (err != nil) != tt.wantErr {
				t.Errorf("BuildImageURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != "" {
				if !strings.Contains(got, tt.wantCont) {
					t.Errorf("BuildImageURL() = %v, should contain %v", got, tt.wantCont)
				}

				// Test that URL is valid
				_, err := url.Parse(got)
				if err != nil {
					t.Errorf("BuildImageURL() returned invalid URL: %v", err)
				}

				// Should be a Bing images URL
				if !strings.Contains(got, "bing.com/images/search") {
					t.Errorf("BuildImageURL() should return Bing images URL, got: %v", got)
				}
			}
		})
	}
}

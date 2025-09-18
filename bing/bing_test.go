package bing

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

var browser *core.Browser

func init() {
	opts := core.BrowserOpts{IsHeadless: false, IsLeakless: false, UseStealth: true, Timeout: time.Second * 5, LeavePageOpen: true}
	browser, _ = core.NewBrowser(opts)
}

func TestSearchBing(t *testing.T) {
	bing := New(*browser, core.SearchEngineOptions{})

	query := core.Query{Text: "golang programming", Limit: 10}
	results, err := bing.Search(query)
	if err != nil {
		t.Fatalf("Cannot [SearchBing]: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("[SearchBing] returned empty result")
	}

	// Check that we have some basic fields populated
	firstResult := results[0]
	if firstResult.Title == "" {
		t.Errorf("First result missing title: %+v", firstResult)
	}
	if firstResult.URL == "" {
		t.Errorf("First result missing URL: %+v", firstResult)
	}
	if firstResult.Rank == 0 {
		t.Errorf("First result missing rank: %+v", firstResult)
	}
}

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
			wantCont: "q=dogs+filetype%3Apng",
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

func TestBingImageSearch(t *testing.T) {
	bing := New(*browser, core.SearchEngineOptions{RateTime: 5})

	query := core.Query{
		Text:     "golden puppy",
		Limit:    25,
		Filetype: "jpg",
	}

	results, err := bing.SearchImage(query)
	if err != nil {
		t.Fatalf("Cannot search Bing images: %s", err)
	}

	if len(results) == 0 {
		t.Fatalf("Bing image search returned empty result")
	}

	// Check that we have image results with proper fields
	firstResult := results[0]
	if firstResult.URL == "" {
		t.Errorf("First result missing image URL: %+v", firstResult)
	}
	if firstResult.Title == "" {
		t.Errorf("First result missing title: %+v", firstResult)
	}

	// Check that we have either image URL or source URL
	hasImageURL := firstResult.URL != ""
	hasSourceURL := firstResult.URL != ""
	if !hasImageURL && !hasSourceURL {
		t.Errorf("First result should have either image URL or source URL: %+v", firstResult)
	}

	// For image results, URL should typically point to an image file
	if hasImageURL {
		// Check if it looks like an image URL (common extensions)
		imageExtensions := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"}
		hasImageExtension := false
		for _, ext := range imageExtensions {
			if strings.Contains(strings.ToLower(firstResult.URL), ext) {
				hasImageExtension = true
				break
			}
		}
		if !hasImageExtension {
			t.Logf("Image URL doesn't have common extension (might be valid): %s", firstResult.URL)
		}
	}

	t.Logf("Found %d image results", len(results))
	t.Logf("First result - Title: %s", firstResult.Title)
	t.Logf("First result - Image URL: %s", firstResult.URL)
	t.Logf("First result - Source URL: %s", firstResult.URL)
}

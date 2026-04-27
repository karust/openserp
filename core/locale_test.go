package core

import "testing"

func TestParseLocale(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantLang string
		wantCC   string
	}{
		{"empty", "", "", ""},
		{"whitespace only", "   ", "", ""},
		{"language only", "EN", "en", ""},
		{"language with region dash", "en-US", "en", "US"},
		{"language with region underscore", "de_AT", "de", "AT"},
		{"mixed casing", "Pt-bR", "pt", "BR"},
		{"trailing whitespace", "  fr-CA  ", "fr", "CA"},
		{"empty region after dash", "ru-", "ru", ""},
		{"language only after split", "-US", "", ""},
		{"extra subtags ignored", "en-US-x-private", "en", "US"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLocale(tt.in)
			if got.Language != tt.wantLang || got.Country != tt.wantCC {
				t.Fatalf("ParseLocale(%q) = {%q, %q}, want {%q, %q}",
					tt.in, got.Language, got.Country, tt.wantLang, tt.wantCC)
			}
		})
	}
}

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

func TestBuildAcceptLanguageHeader(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "language only with default country", in: "de", want: "de-DE,de;q=0.9"},
		{name: "language only with mapped country", in: "pt", want: "pt-BR,pt;q=0.9"},
		{name: "explicit region", in: "en-GB", want: "en-GB,en;q=0.9"},
		{name: "unknown language emits bare tag", in: "sw", want: "sw"},
		{name: "invalid locale", in: "-US", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAcceptLanguageHeader(tt.in)
			if got != tt.want {
				t.Fatalf("BuildAcceptLanguageHeader(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

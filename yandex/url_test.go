package yandex

import (
	"net/url"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

func TestBuildURLLanguageOperator(t *testing.T) {
	tests := []struct {
		name     string
		langCode string
		wantOp   string // expected substring in text query, "" means no lang: operator
	}{
		{"empty lang adds no operator", "", ""},
		{"uppercase language is normalized", "RU", "lang:ru"},
		{"region is dropped from lang operator", "en-US", "lang:en"},
		{"underscore form is accepted", "DE_at", "lang:de"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(core.Query{Text: "golang", LangCode: tt.langCode}, 0)
			if err != nil {
				t.Fatalf("BuildURL() error = %v", err)
			}

			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("BuildURL() returned invalid URL: %v", err)
			}

			text := parsed.Query().Get("text")
			if tt.wantOp == "" {
				if strings.Contains(text, "lang:") {
					t.Fatalf("expected no lang: operator, got text=%q", text)
				}
				return
			}
			if !strings.Contains(text, tt.wantOp) {
				t.Fatalf("expected text to contain %q, got %q", tt.wantOp, text)
			}
		})
	}
}

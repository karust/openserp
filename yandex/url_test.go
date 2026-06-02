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

func TestBuildURLRegionLR(t *testing.T) {
	tests := []struct {
		name   string
		region string
		wantLR string
	}{
		{name: "numeric yandex region is lr", region: "213", wantLR: "213"},
		{name: "whitespace is trimmed", region: " 1 ", wantLR: "1"},
		{name: "country region maps to yandex lr", region: "DE", wantLR: "96"},
		{name: "locale region maps to yandex lr", region: "en-GB", wantLR: "102"},
		{name: "empty region omits lr", region: "", wantLR: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildURL(core.Query{Text: "golang", Region: tt.region}, 0)
			if err != nil {
				t.Fatalf("BuildURL() error = %v", err)
			}

			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("BuildURL() returned invalid URL: %v", err)
			}
			if gotLR := parsed.Query().Get("lr"); gotLR != tt.wantLR {
				t.Fatalf("unexpected lr value: %q want %q", gotLR, tt.wantLR)
			}
			wantRstr := ""
			if tt.wantLR != "" {
				wantRstr = "true"
			}
			if gotRstr := parsed.Query().Get("rstr"); gotRstr != wantRstr {
				t.Fatalf("unexpected rstr value: %q for lr %q", gotRstr, tt.wantLR)
			}
		})
	}
}

func TestBuildImageURLRegionLR(t *testing.T) {
	got, err := BuildImageURL(core.Query{Text: "golang", Region: "2"}, 0)
	if err != nil {
		t.Fatalf("BuildImageURL() error = %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("BuildImageURL() returned invalid URL: %v", err)
	}
	if gotLR := parsed.Query().Get("lr"); gotLR != "2" {
		t.Fatalf("unexpected lr value: %q", gotLR)
	}
	if gotRstr := parsed.Query().Get("rstr"); gotRstr != "true" {
		t.Fatalf("unexpected rstr value: %q", gotRstr)
	}
}

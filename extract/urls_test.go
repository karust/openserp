package extract

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestExtractBaseAcrossModes(t *testing.T) {
	for _, tc := range []struct{ name, base, wantPrefix string }{
		{"empty", "", "https://example.com/de/"},
		{"root", "/", "https://example.com/"},
		{"relative", "../assets/", "https://example.com/assets/"},
		{"cross_origin", "https://cdn.example.net/assets/", "https://cdn.example.net/assets/"},
		{"file", "https://cdn.example.net/assets/index.html", "https://cdn.example.net/assets/"},
		{"protocol_relative", "//cdn.example.net/assets/", "https://cdn.example.net/assets/"},
	} {
		for _, fullPage := range []bool{false, true} {
			for _, mode := range []Mode{ModeFast, ModeRendered} {
				t.Run(fmt.Sprintf("%s/full_page=%t/%s", tc.name, fullPage, mode), func(t *testing.T) {
					raw := `<!doctype html><html><head><title>Base resolution article</title>
<base href="` + tc.base + `"><link rel="canonical" href="canonical.html"></head>
<body><article><h1>Base resolution article</h1>
<p>Readers use source references to verify research findings and compare the methods used
in different experiments. Each reference should identify the original publication so that
readers can inspect its measurements, evaluate its assumptions, and reproduce its analysis.
Read the <a href="de/article.html">supporting research</a> for details about the experiment,
the equipment used to collect the data, and the conclusions drawn from the observations.</p>
<p>The illustration below shows the measurements from the experiment, including the controls
and the observed differences between groups.</p>
<img src="images/example.png" alt="Experiment measurements"></article></body></html>`
					result := runExtract(t, staticRaw(raw), RenderedFetcher(staticRaw(raw)), ExtractRequest{URL: "https://example.com/de/page.html", Mode: mode, FullPage: fullPage})
					if len([]rune(result.Text)) < minCleanTextRunes {
						t.Fatal("fixture too short")
					}
					if result.URL != "https://example.com/de/page.html" {
						t.Errorf("document URL changed: %s", result.URL)
					}
					if result.Canonical != tc.wantPrefix+"canonical.html" {
						t.Errorf("canonical: %s", result.Canonical)
					}
					if len(result.Links) != 1 || result.Links[0].URL != tc.wantPrefix+"de/article.html" {
						t.Errorf("links: %#v", result.Links)
					}
					for _, suffix := range []string{"de/article.html", "images/example.png"} {
						if !strings.Contains(result.Markdown, "("+tc.wantPrefix+suffix+")") {
							t.Errorf("markdown missing %s; got %s", tc.wantPrefix+suffix, result.Markdown)
						}
					}
				})
			}
		}
	}
}

func TestEffectiveBaseURL(t *testing.T) {
	const documentURL = "https://example.com/de/page.html"
	for _, tc := range []struct{ name, head, want string }{
		{"absent", "", documentURL},
		{"empty", `<base href="">`, documentURL},
		{"relative", `<base href="../assets/">`, "https://example.com/assets/"},
		{"protocol_relative", `<base href="//cdn.example.net/">`, "https://cdn.example.net/"},
		{"first_wins", `<base href="/one/"><base href="/two/">`, "https://example.com/one/"},
		{"first_invalid", `<base href="http://%"><base href="/two/">`, documentURL},
		{"first_empty", `<base href=""><base href="/two/">`, documentURL},
		{"target_only", `<base target="_blank"><base href="/two/">`, "https://example.com/two/"},
		{"javascript", `<base href="javascript:alert(1)">`, documentURL},
		{"data", `<base href="data:text/html,hello">`, documentURL},
		{"missing_host", `<base href="https:///assets/">`, documentURL},
		{"template", `<template><base href="https://unused.example/"></template><base href="/correct/">`, "https://example.com/correct/"},
		{"template_only", `<template><base href="https://unused.example/"></template>`, documentURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := documentFromString("<!doctype html><html><head>" + tc.head + "</head><body></body></html>")
			if err != nil {
				t.Fatal(err)
			}
			if got := effectiveBaseURL(doc, documentURL); got != tc.want {
				t.Errorf("base = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractFinalURL(t *testing.T) {
	for _, head := range []string{"", `<base href="./">`, `<template><base href="https://unused.example/"></template>`} {
		for _, mode := range []Mode{ModeFast, ModeRendered} {
			t.Run(fmt.Sprintf("%s/%s", mode, head), func(t *testing.T) {
				fetch := func(context.Context, ExtractRequest) (*FetchResponse, error) {
					return &FetchResponse{
						StatusCode: 200,
						FinalURL:   "https://destination.example/docs/page.html",
						Body: []byte(`<html><head>` + head + `<link rel="canonical" href="canonical.html"></head>
<body><p>Read the <a href="guide.html">guide</a>.</p><img src="diagram.png" alt="Diagram"></body></html>`),
					}, nil
				}
				result := runExtract(t, fetch, fetch, ExtractRequest{URL: "https://source.example/old/page", Mode: mode, FullPage: true})
				if result.URL != "https://source.example/old/page" {
					t.Fatalf("request URL changed: %s", result.URL)
				}
				if result.Canonical != "https://destination.example/docs/canonical.html" {
					t.Errorf("canonical = %s", result.Canonical)
				}
				if len(result.Links) != 1 || result.Links[0].URL != "https://destination.example/docs/guide.html" {
					t.Errorf("links = %#v", result.Links)
				}
				for _, path := range []string{"guide.html", "diagram.png"} {
					if !strings.Contains(result.Markdown, "(https://destination.example/docs/"+path+")") {
						t.Errorf("incorrect URL for %s: %s", path, result.Markdown)
					}
				}
			})
		}
	}
}

func TestResolveContentURLs(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"article.html", "https://example.com/docs/article.html"},
		{"../article.html", "https://example.com/article.html"},
		{"/article.html", "https://example.com/article.html"},
		{"//cdn.example/img.png", "https://cdn.example/img.png"},
		{"?page=2", "https://example.com/docs/index.html?page=2"},
		{"#section", "#section"},
		{"javascript:void(0)", "javascript:void(0)"},
		{"https://other.example/page", "https://other.example/page"},
		{"mailto:help@example.com", "mailto:help@example.com"},
		{"data:image/png;base64,AAAA", "data:image/png;base64,AAAA"},
		{"", ""},
		{"http://%", "http://%"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			doc, err := documentFromString(`<a href="` + tc.raw + `">Link</a><img src="` + tc.raw + `"><img data-src="` + tc.raw + `">`)
			if err != nil {
				t.Fatal(err)
			}
			resolveContentURLs(doc, "https://example.com/docs/index.html")
			for _, attr := range []string{"href", "src", "data-src"} {
				got, _ := doc.Find("[" + attr + "]").Attr(attr)
				if got != tc.want {
					t.Errorf("%s = %q, want %q", attr, got, tc.want)
				}
			}
		})
	}
}

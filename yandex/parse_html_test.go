package yandex

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseYandexHTML(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/search_results.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	results, err := ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}

	rank := 0
	for i, r := range results {
		if r.Ad {
			continue
		}
		rank++
		if r.Rank != rank {
			t.Fatalf("rank sequence broken at index %d: got %d, want %d", i, r.Rank, rank)
		}
		if r.URL == "" {
			t.Fatalf("result %d: empty URL", i)
		}
		if r.Title == "" {
			t.Fatalf("result %d: empty Title", i)
		}
		if !strings.HasPrefix(r.URL, "http") {
			t.Fatalf("result %d: URL not absolute: %s", i, r.URL)
		}
	}
	if rank == 0 {
		t.Fatal("expected at least one organic result")
	}
}

func TestParseYandexHTMLEmpty(t *testing.T) {
	t.Parallel()

	results, err := ParseHTML(bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("ParseHTML() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected zero results for empty HTML, got %d", len(results))
	}
}

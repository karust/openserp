package testutil

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/karust/openserp/core"
)

// ResponseFromFixture reads an HTML file from the package's testdata/ directory
// and returns it wrapped in an *http.Response suitable for parser functions.
func ResponseFromFixture(t *testing.T, file string) *http.Response {
	t.Helper()

	path := filepath.Join("testdata", file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}

	return ResponseFromBytes(data)
}

// ResponseFromString wraps a raw HTML string in an *http.Response.
func ResponseFromString(html string) *http.Response {
	return ResponseFromBytes([]byte(html))
}

// ResponseFromBytes wraps raw bytes in an *http.Response with status 200.
func ResponseFromBytes(data []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

// AssertSequentialRanks verifies that result ranks start at 1 and increase by 1.
func AssertSequentialRanks(t *testing.T, results []core.SearchResult) {
	t.Helper()

	for i, r := range results {
		if r.Rank != i+1 {
			t.Fatalf("rank sequence broken at index %d: got %d, want %d", i, r.Rank, i+1)
		}
	}
}

// AssertFirstResultFilled checks that the first result has non-empty URL, Title, and Description.
func AssertFirstResultFilled(t *testing.T, results []core.SearchResult) {
	t.Helper()

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	first := results[0]
	if first.URL == "" {
		t.Fatal("first result URL is empty")
	}
	if first.Title == "" {
		t.Fatal("first result title is empty")
	}
	if first.Description == "" {
		t.Fatal("first result description is empty")
	}
}

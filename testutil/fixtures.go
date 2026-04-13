package testutil

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
// Accepts any slice of structs with an int Rank field (avoids core import cycle).
func AssertSequentialRanks(t *testing.T, results any) {
	t.Helper()

	v := reflect.ValueOf(results)
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected a slice of results, got %T", results)
	}

	for i := 0; i < v.Len(); i++ {
		item := v.Index(i)
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		rank := int(item.FieldByName("Rank").Int())
		if rank != i+1 {
			t.Fatalf("rank sequence broken at index %d: got %d, want %d", i, rank, i+1)
		}
	}
}

// AssertFirstResultFilled checks that the first result has non-empty URL, Title, and Description.
// Accepts any slice of structs with those string fields (avoids core import cycle).
func AssertFirstResultFilled(t *testing.T, results any) {
	t.Helper()

	v := reflect.ValueOf(results)
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected a slice of results, got %T", results)
	}
	if v.Len() == 0 {
		t.Fatal("expected at least one result")
	}

	first := v.Index(0)
	if first.Kind() == reflect.Ptr {
		first = first.Elem()
	}

	for _, field := range []string{"URL", "Title", "Description"} {
		val := first.FieldByName(field)
		if !val.IsValid() || val.String() == "" {
			t.Fatalf("first result %s is empty", field)
		}
	}
}

package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FP-1: panics from engine code must be converted to ErrEngineInternal by the
// central recovery in the resilient layer (invokeEngine) instead of killing
// the process. SearchImage had no per-engine recovery in 5 of 6 engines.
func TestInvokeEngineRecoversPanics(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
			panic("rod: page crashed")
		},
		imageFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
			panic("rod: object not found")
		},
	}

	for _, isImage := range []bool{false, true} {
		results, err := invokeEngine(context.Background(), engine, Query{Text: "golang"}, isImage)
		if results != nil {
			t.Fatalf("isImage=%v: expected nil results after panic, got %v", isImage, results)
		}
		if !errors.Is(err, ErrEngineInternal) {
			t.Fatalf("isImage=%v: expected ErrEngineInternal, got %v", isImage, err)
		}
	}
}

func TestPanickingSearchImageReturns502AndServerSurvives(t *testing.T) {
	engine := &engineMock{
		name:        "google",
		initialized: true,
		imageFn: func(_ context.Context, _ Query) ([]SearchResult, error) {
			panic("rod: page crashed")
		},
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	srv := NewServerWithOptions("127.0.0.1", 7130, opts, engine)

	req := httptest.NewRequest(http.MethodGet, "/google/image?text=golang", nil)
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("image request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for panicking SearchImage, got %d", resp.StatusCode)
	}
	var payload JSONErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != "engine_internal" {
		t.Fatalf("expected error=engine_internal, got %q", payload.Error)
	}

	second := request(t, srv, "/google/search?text=golang")
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected server to keep serving after panic, got %d", second.StatusCode)
	}
}

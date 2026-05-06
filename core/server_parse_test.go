package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type parserMock struct {
	engineMock
	parseHTMLFn func(io.Reader) ([]SearchResult, error)
}

func (p *parserMock) ParseHTML(r io.Reader) ([]SearchResult, error) {
	if p.parseHTMLFn != nil {
		return p.parseHTMLFn(r)
	}
	return []SearchResult{
		{Rank: 1, URL: "https://example.com/1", Title: "Result One", Description: "Desc one"},
		{Rank: 2, URL: "https://example.com/2", Title: "Result Two", Description: "Desc two"},
	}, nil
}

func postHTML(t *testing.T, s *Server, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/html")
	resp, err := s.app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

func TestParseEndpointReturnsEnvelope(t *testing.T) {
	engine := &parserMock{engineMock: engineMock{name: "google", initialized: true}}
	srv := NewServerWithOptions("127.0.0.1", 7120, DefaultServerOptions(), engine)

	resp := postHTML(t, srv, "/google/parse", "<html><body>sample serp</body></html>")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(env.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(env.Results))
	}
	if env.Results[0].URL != "https://example.com/1" {
		t.Fatalf("unexpected first result URL: %s", env.Results[0].URL)
	}
}

func TestParseEndpointEmptyBodyReturns400(t *testing.T) {
	engine := &parserMock{engineMock: engineMock{name: "google", initialized: true}}
	srv := NewServerWithOptions("127.0.0.1", 7121, DefaultServerOptions(), engine)

	req := httptest.NewRequest(http.MethodPost, "/google/parse", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "text/html")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestParseEndpointParserErrorReturns400(t *testing.T) {
	engine := &parserMock{
		engineMock: engineMock{name: "bing", initialized: true},
		parseHTMLFn: func(_ io.Reader) ([]SearchResult, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}
	srv := NewServerWithOptions("127.0.0.1", 7122, DefaultServerOptions(), engine)

	resp := postHTML(t, srv, "/bing/parse", "<html>bad</html>")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestParseEndpointNotRegisteredForNonParserEngine(t *testing.T) {
	// engineMock does NOT implement HTMLParser so no /mock/parse route is registered.
	engine := &engineMock{name: "mock", initialized: true}
	srv := NewServerWithOptions("127.0.0.1", 7123, DefaultServerOptions(), engine)

	resp := postHTML(t, srv, "/mock/parse", "<html><body></body></html>")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for non-parser engine, got %d", resp.StatusCode)
	}
}

func TestParseEndpointMarkdownFormat(t *testing.T) {
	engine := &parserMock{engineMock: engineMock{name: "google", initialized: true}}
	srv := NewServerWithOptions("127.0.0.1", 7124, DefaultServerOptions(), engine)

	req := httptest.NewRequest(http.MethodPost, "/google/parse?format=markdown", strings.NewReader("<html><body>serp</body></html>"))
	req.Header.Set("Content-Type", "text/html")
	resp, err := srv.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/markdown") {
		t.Fatalf("expected markdown content type, got %s", ct)
	}
}

func TestParseEndpointDuckAlias(t *testing.T) {
	engine := &parserMock{engineMock: engineMock{name: "duckduckgo", initialized: true}}
	srv := NewServerWithOptions("127.0.0.1", 7125, DefaultServerOptions(), engine)

	resp := postHTML(t, srv, "/duck/parse", "<html><body>sample serp</body></html>")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	extractpkg "github.com/karust/openserp/extract"
)

func TestEnrichEnvelopeWithExtractionRetriesThinAndFailedCandidates(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/thin":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><head><title>tripadvisor.com</title></head><body>tripadvisor.com</body></html>`))
		case "/blocked":
			w.WriteHeader(http.StatusBadGateway)
		case "/useful":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body><article><h1>Useful page</h1><p>This useful page has enough body text to count as extracted content and should be selected after earlier candidates fail.</p></article></body></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer target.Close()

	opts := DefaultServerOptions()
	opts.Extract = extractpkg.Config{
		Enabled:              true,
		DefaultMode:          string(extractpkg.ModeFast),
		Timeout:              time.Second,
		MaxBytes:             256 * 1024,
		MaxConcurrent:        2,
		AllowPrivateNetworks: true,
	}
	s := &Server{opts: opts}
	env := &Envelope{Results: []Result{
		{URL: target.URL + "/thin"},
		{URL: target.URL + "/blocked"},
		{URL: target.URL + "/useful"},
	}}
	q := Query{Extract: true, ExtractTop: 1, ExtractMode: string(extractpkg.ModeFast)}

	s.enrichEnvelopeWithExtraction(context.Background(), env, q, "json")

	if env.Results[0].Extracted == nil || env.Results[0].Extracted.Error != "empty extracted content" {
		t.Fatalf("first candidate extracted = %+v, want empty-content error", env.Results[0].Extracted)
	}
	if env.Results[1].Extracted == nil || env.Results[1].Extracted.Error == "" {
		t.Fatalf("second candidate extracted = %+v, want failure error", env.Results[1].Extracted)
	}
	if env.Results[2].Extracted == nil || env.Results[2].Extracted.Error != "" {
		t.Fatalf("third candidate extracted = %+v, want successful retry", env.Results[2].Extracted)
	}
	if !strings.Contains(env.Results[2].Extracted.Content, "Useful page") {
		t.Fatalf("third candidate content = %q", env.Results[2].Extracted.Content)
	}
}

func TestExtractRejectsLinkLocalAddressByDefault(t *testing.T) {
	opts := DefaultServerOptions()
	opts.Extract = extractpkg.DefaultConfig()
	s := NewServerWithOptions("127.0.0.1", 0, opts)

	req, err := http.NewRequest(http.MethodPost, "/extract", strings.NewReader(`{"url":"http://169.254.169.254/latest/meta-data/"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestExtractRejectsLocalhostByDefault(t *testing.T) {
	opts := DefaultServerOptions()
	opts.Extract = extractpkg.DefaultConfig()
	s := NewServerWithOptions("127.0.0.1", 0, opts)

	req, err := http.NewRequest(http.MethodGet, "/extract?url=http://localhost/private", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestValidateExtractTargetURLNormalizesBarePublicIP(t *testing.T) {
	if err := validateExtractTargetURL(context.Background(), "1.1.1.1", false); err != nil {
		t.Fatalf("expected bare public IP target to validate after scheme normalization: %v", err)
	}
}

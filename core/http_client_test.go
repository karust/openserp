package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestDrainAndCloseResponseDrainsAndCloses(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("unread payload")}
	resp := &http.Response{Body: body}

	DrainAndCloseResponse(resp)

	if !body.closed {
		t.Fatal("expected body to be closed")
	}

	n, err := body.Read(make([]byte, 1))
	if err != io.EOF || n != 0 {
		t.Fatalf("expected body drained to EOF, got n=%d err=%v", n, err)
	}
}

func TestClassifySearchHTTPStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "unknown browser status", status: 0, want: nil},
		{name: "ok", status: http.StatusOK, want: nil},
		{name: "blocked", status: http.StatusForbidden, want: ErrBlocked},
		{name: "rate limited", status: http.StatusTooManyRequests, want: ErrRateLimited},
		{name: "server error", status: http.StatusBadGateway, want: ErrBlocked},
		{name: "unexpected status", status: http.StatusNotFound, want: ErrParser},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ClassifySearchHTTPStatus(tt.status)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestDrainAndCloseResponseNilSafe(t *testing.T) {
	DrainAndCloseResponse(nil)
	DrainAndCloseResponse(&http.Response{})
}

func TestRawHTTPClientTracksNetworkBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("payload"))
	}))
	defer server.Close()

	client, err := NewRawHTTPClient(Query{})
	if err != nil {
		t.Fatalf("new raw client: %v", err)
	}

	ctx := WithNetworkUsage(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer DrainAndCloseResponse(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("unexpected body: %q", string(body))
	}
	if got := NetworkBytesFromContext(ctx); got < int64(len(body)) {
		t.Fatalf("expected tracked bytes >= body length, got %d", got)
	}
}

func TestRawHTTPClientTracksProxyErrorBytes(t *testing.T) {
	proxyBody := "proxy auth required"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = w.Write([]byte(proxyBody))
	}))
	defer proxy.Close()

	client, err := NewRawHTTPClient(Query{ProxyURL: proxy.URL})
	if err != nil {
		t.Fatalf("new raw client: %v", err)
	}

	ctx := WithNetworkUsage(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	DrainAndCloseResponse(resp)
	if !errors.Is(err, ErrProxyAuth) {
		t.Fatalf("expected proxy auth error, got %v", err)
	}
	if got := NetworkBytesFromContext(ctx); got < int64(len(proxyBody)) {
		t.Fatalf("expected tracked bytes >= proxy body length, got %d", got)
	}
}

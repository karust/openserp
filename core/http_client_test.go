package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

func TestRawHTTPClientAppliesBrowserHeaderDefaults(t *testing.T) {
	resetRawHTTPClientCache(t)

	var userAgent, acceptLanguage, secCHUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		acceptLanguage = r.Header.Get("Accept-Language")
		secCHUA = r.Header.Get("Sec-CH-UA")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client, err := NewRawHTTPClient(Query{LangCode: "fr", Region: "FR"})
	if err != nil {
		t.Fatalf("new raw client: %v", err)
	}

	ctx := WithBrowserProfileUsage(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	DrainAndCloseResponse(resp)

	if userAgent == "" || strings.Contains(userAgent, "Go-http-client") {
		t.Fatalf("unexpected User-Agent %q", userAgent)
	}
	if want := BuildAcceptLanguageHeader("fr-FR"); acceptLanguage != want {
		t.Fatalf("Accept-Language = %q, want %q", acceptLanguage, want)
	}
	if secCHUA == "" {
		t.Fatal("expected Sec-CH-UA to be set")
	}
	if ids := BrowserProfileIDsFromContext(ctx); len(ids) != 1 || ids[0] == "" {
		t.Fatalf("expected one recorded profile id, got %v", ids)
	}
}

func TestRawHTTPClientGuardRejectsInitialPrivateURLWithProxy(t *testing.T) {
	client, err := NewRawHTTPClient(Query{
		ProxyURL:             "http://127.0.0.1:1",
		GuardPrivateNetworks: true,
	})
	if err != nil {
		t.Fatalf("new raw client: %v", err)
	}

	resp, err := client.Get("http://127.0.0.1/")
	DrainAndCloseResponse(resp)
	if !errors.Is(err, ErrTargetNotAllowed) {
		t.Fatalf("expected target guard error, got %v", err)
	}
}

func TestRawHTTPClientProxyAuthError(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = w.Write([]byte("proxy auth required"))
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
}

// TestRawSearchRequestReusesPooledClient checks that same-profile calls share
// one connection and a stable Chrome UA. Bytes/headers are covered elsewhere.
func TestRawSearchRequestReusesPooledClient(t *testing.T) {
	resetRawHTTPClientCache(t)

	var connCount atomic.Int32
	var mu sync.Mutex
	var userAgents []string

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		userAgents = append(userAgents, r.Header.Get("User-Agent"))
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connCount.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	query := Query{LangCode: "de", Region: "DE"}
	for i := 0; i < 2; i++ {
		ctx := WithEngine(WithBrowserProfileUsage(context.Background()), "google")
		readRawSearchBodyForTest(t, ctx, server.URL, query)
		if ids := BrowserProfileIDsFromContext(ctx); len(ids) != 1 || ids[0] == "" {
			t.Fatalf("request %d recorded profile ids %v, want exactly one", i, ids)
		}
	}

	if got := connCount.Load(); got != 1 {
		t.Fatalf("expected one reused TCP connection, got %d", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(userAgents) != 2 {
		t.Fatalf("expected two captured User-Agents, got %d", len(userAgents))
	}
	if userAgents[0] == "" || strings.Contains(userAgents[0], "Go-http-client") || userAgents[0] != userAgents[1] {
		t.Fatalf("expected stable Chrome User-Agent, got %q then %q", userAgents[0], userAgents[1])
	}
}

// TestRawRequestProfilesRoundRobinCoherently checks rotation hits every Chrome
// major and keeps UA major == Sec-CH-UA major == TLS fingerprint.
func TestRawRequestProfilesRoundRobinCoherently(t *testing.T) {
	tlsByMajor := map[int]string{}
	for _, p := range rawChromeProfiles {
		tlsByMajor[p.major] = p.tls.GetClientHelloStr()
	}

	seen := map[int]bool{}
	for i := 0; i < 200; i++ {
		ctx := WithProxyLaneKey(WithEngine(context.Background(), "google"),
			ProxyLaneKey{Engine: "google", SessionID: "sid-" + strconv.Itoa(i)})

		profile := rawRequestProfileFor(ctx, Query{Region: "US"})
		major, err := strconv.Atoi(chromeMajorVersion(extractChromeVersion(profile.userAgent)))
		if err != nil {
			t.Fatalf("raw User-Agent has no Chrome major: %q", profile.userAgent)
		}
		wantTLS, ok := tlsByMajor[major]
		if !ok {
			t.Fatalf("UA major %d has no configured tls-client profile", major)
		}
		if profile.tlsProfile.GetClientHelloStr() != wantTLS {
			t.Fatalf("major %d: TLS fingerprint does not match UA", major)
		}
		if !strings.Contains(profile.secCHUA, `;v="`+strconv.Itoa(major)+`"`) {
			t.Fatalf("Sec-CH-UA %q does not match UA major %d", profile.secCHUA, major)
		}
		seen[major] = true
	}

	for _, p := range rawChromeProfiles {
		if !seen[p.major] {
			t.Fatalf("configured Chrome major %d never selected; seen=%v", p.major, seen)
		}
	}
}

func readRawSearchBodyForTest(t *testing.T, ctx context.Context, searchURL string, query Query) string {
	t.Helper()

	resp, err := RawSearchRequest(ctx, searchURL, query)
	if err != nil {
		t.Fatalf("raw search request: %v", err)
	}
	defer DrainAndCloseResponse(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read raw search body: %v", err)
	}
	return string(body)
}

func resetRawHTTPClientCache(t *testing.T) {
	t.Helper()

	rawHTTPClientCache.Lock()
	entries := rawHTTPClientCache.clients
	rawHTTPClientCache.clients = map[rawHTTPClientKey]*rawHTTPClientEntry{}
	rawHTTPClientCache.Unlock()

	for _, entry := range entries {
		entry.client.CloseIdleConnections()
	}
}

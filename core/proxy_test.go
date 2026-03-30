package core

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	socks5 "github.com/armon/go-socks5"
	xcontext "golang.org/x/net/context"
)

func TestNormalizeProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty", raw: "", want: ""},
		{name: "http", raw: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "https", raw: "https://127.0.0.1:8443", want: "https://127.0.0.1:8443"},
		{name: "socks5", raw: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{name: "socks5h upper", raw: "SOCKS5H://127.0.0.1:1080", want: "socks5h://127.0.0.1:1080"},
		{name: "missing scheme", raw: "127.0.0.1:8080", wantErr: true},
		{name: "missing host", raw: "http://", wantErr: true},
		{name: "unsupported scheme", raw: "ftp://127.0.0.1:21", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeProxyURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNormalizeProxyConfigDefaultsAndDeduplicates(t *testing.T) {
	cfg, err := NormalizeProxyConfig(ProxyConfig{
		Runtime:   "RAW",
		StaticURL: " socks5h://127.0.0.1:1080 ",
		PoolURLs: []string{
			"",
			"http://proxy-one:8080",
			"http://proxy-one:8080",
			"socks5://proxy-two:1080",
		},
	})
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}

	if cfg.Runtime != ProxyRuntimeRaw {
		t.Fatalf("expected raw runtime, got %s", cfg.Runtime)
	}
	if cfg.StaticURL != "socks5h://127.0.0.1:1080" {
		t.Fatalf("unexpected static proxy: %s", cfg.StaticURL)
	}
	if cfg.PoolFailureThreshold != DefaultProxyPoolFailureThreshold {
		t.Fatalf("expected default threshold %d, got %d", DefaultProxyPoolFailureThreshold, cfg.PoolFailureThreshold)
	}
	if len(cfg.PoolURLs) != 2 {
		t.Fatalf("expected 2 deduplicated pool URLs, got %d", len(cfg.PoolURLs))
	}
}

func TestProxyPoolRoundRobinAndFailureRecovery(t *testing.T) {
	pool, err := NewProxyPool([]string{"http://proxy1:8080", "http://proxy2:8080"}, 2)
	if err != nil {
		t.Fatalf("new proxy pool: %v", err)
	}

	if got := pool.Next(); got != "http://proxy1:8080" {
		t.Fatalf("expected first proxy1, got %s", got)
	}
	if got := pool.Next(); got != "http://proxy2:8080" {
		t.Fatalf("expected second proxy2, got %s", got)
	}

	pool.ReportFailure("http://proxy1:8080")
	pool.ReportFailure("http://proxy1:8080")
	if got := pool.Next(); got != "http://proxy2:8080" {
		t.Fatalf("expected proxy2 while proxy1 disabled, got %s", got)
	}

	pool.ReportFailure("http://proxy2:8080")
	pool.ReportFailure("http://proxy2:8080")
	if got := pool.Next(); got != "http://proxy1:8080" {
		t.Fatalf("expected pool reset to proxy1 after exhaustion, got %s", got)
	}

	pool.ReportFailure("http://proxy1:8080")
	pool.ReportSuccess("http://proxy1:8080")
	stats := pool.Stats()
	if stats.Disabled != 0 {
		t.Fatalf("expected no disabled proxies after recovery, got %d", stats.Disabled)
	}
	if stats.Active != 2 {
		t.Fatalf("expected both proxies active, got %d", stats.Active)
	}
}

func TestMaskProxyURLRedactsCredentials(t *testing.T) {
	if got := MaskProxyURL("http://user:pass@127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected masked proxy value: %s", got)
	}
}

func TestNewRawHTTPClientSocks5hUsesProxyDNS(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer target.Close()

	targetAddr, err := net.ResolveTCPAddr("tcp", target.Listener.Addr().String())
	if err != nil {
		t.Fatalf("resolve target listener: %v", err)
	}

	const proxyOnlyHost = "proxy-target.invalid"
	proxyAddr := startSOCKS5TestServer(t, proxyOnlyHost, targetAddr.IP)

	directClient := &http.Client{Timeout: 500 * time.Millisecond}
	targetURL := fmt.Sprintf("http://%s:%d/", proxyOnlyHost, targetAddr.Port)
	if _, err := directClient.Get(targetURL); err == nil {
		t.Fatal("expected direct request to fail without proxy DNS")
	}

	client, err := NewRawHTTPClient(Query{ProxyURL: "socks5h://" + proxyAddr})
	if err != nil {
		t.Fatalf("new raw http client: %v", err)
	}

	resp, err := client.Get(targetURL)
	if err != nil {
		t.Fatalf("expected proxied request to succeed, got %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxied body: %v", err)
	}
	if string(body) != "proxied" {
		t.Fatalf("unexpected proxied body: %q", string(body))
	}
}

type staticResolver struct {
	host string
	ip   net.IP
}

func (r staticResolver) Resolve(ctx xcontext.Context, name string) (xcontext.Context, net.IP, error) {
	if name == r.host {
		return ctx, r.ip, nil
	}
	return ctx, nil, net.UnknownNetworkError(name)
}

func startSOCKS5TestServer(t *testing.T, host string, ip net.IP) string {
	t.Helper()

	server, err := socks5.New(&socks5.Config{
		Resolver: staticResolver{host: host, ip: ip},
		Logger:   log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("create socks5 server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks5: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		_ = server.Serve(listener)
	}()

	return listener.Addr().String()
}

//go:build integration
// +build integration

package core

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

const proxyIntegrationEnabledEnv = "OPENSERP_PROXY_TESTS"

type proxyIntegrationURLs struct {
	targetURL       string
	socks5hAuthURL  string
	socks5hPlainURL string
	httpAuthURL     string
	httpPlainURL    string
	badSocks5URL    string
	badHTTPURL      string
}

func TestIntegrationSocks5hAuthProxyDNS(t *testing.T) {
	cfg := proxyIntegrationConfig(t)
	assertProxyFetchesTarget(t, cfg.targetURL, cfg.socks5hAuthURL)
}

func TestIntegrationSocks5hPlainProxyDNS(t *testing.T) {
	cfg := proxyIntegrationConfig(t)
	assertProxyFetchesTarget(t, cfg.targetURL, cfg.socks5hPlainURL)
}

func TestIntegrationHTTPAuthProxy(t *testing.T) {
	cfg := proxyIntegrationConfig(t)
	assertProxyFetchesTarget(t, cfg.targetURL, cfg.httpAuthURL)
}

func TestIntegrationHTTPPlainProxy(t *testing.T) {
	cfg := proxyIntegrationConfig(t)
	assertProxyFetchesTarget(t, cfg.targetURL, cfg.httpPlainURL)
}

func TestIntegrationRawSOCKSProxyPoolRotation(t *testing.T) {
	cfg := proxyIntegrationConfig(t)
	assertProxyPoolRotation(t, cfg.targetURL, []string{cfg.badSocks5URL, cfg.socks5hAuthURL})
}

func TestIntegrationRawHTTPProxyPoolRotation(t *testing.T) {
	cfg := proxyIntegrationConfig(t)
	assertProxyPoolRotation(t, cfg.targetURL, []string{cfg.badHTTPURL, cfg.httpAuthURL})
}

type proxyIntegrationEngine struct {
	targetURL string
	limiter   *rate.Limiter
	proxies   []string
}

func (e *proxyIntegrationEngine) Name() string {
	return "google"
}

func (e *proxyIntegrationEngine) IsInitialized() bool {
	return true
}

func (e *proxyIntegrationEngine) GetRateLimiter() *rate.Limiter {
	return e.limiter
}

func (e *proxyIntegrationEngine) Search(q Query) ([]SearchResult, error) {
	e.proxies = append(e.proxies, q.ProxyURL)

	body, err := fetchViaRawProxy(q.ProxyURL, q.Insecure, e.targetURL)
	if err != nil {
		return nil, err
	}

	return []SearchResult{{
		Rank:        1,
		URL:         e.targetURL,
		Title:       "proxy-ok",
		Description: body,
	}}, nil
}

func (e *proxyIntegrationEngine) SearchImage(q Query) ([]SearchResult, error) {
	return nil, ErrSearchTimeout
}

func proxyIntegrationConfig(t *testing.T) proxyIntegrationURLs {
	t.Helper()

	if os.Getenv(proxyIntegrationEnabledEnv) != "1" {
		t.Skipf("set %s=1 to run proxy integration tests", proxyIntegrationEnabledEnv)
	}

	return proxyIntegrationURLs{
		targetURL:       envOrDefault("OPENSERP_PROXY_TEST_TARGET_URL", "http://proxy-target:8080/"),
		socks5hAuthURL:  envOrDefault("OPENSERP_PROXY_TEST_SOCKS5H_AUTH_URL", "socks5h://test:test@127.0.0.1:19080"),
		socks5hPlainURL: envOrDefault("OPENSERP_PROXY_TEST_SOCKS5H_PLAIN_URL", "socks5h://127.0.0.1:19082"),
		httpAuthURL:     envOrDefault("OPENSERP_PROXY_TEST_HTTP_AUTH_URL", "http://test:test@127.0.0.1:18888"),
		httpPlainURL:    envOrDefault("OPENSERP_PROXY_TEST_HTTP_PLAIN_URL", "http://127.0.0.1:18889"),
		badSocks5URL:    envOrDefault("OPENSERP_PROXY_TEST_BAD_SOCKS5_URL", "socks5://127.0.0.1:19081"),
		badHTTPURL:      envOrDefault("OPENSERP_PROXY_TEST_BAD_HTTP_URL", "http://127.0.0.1:18890"),
	}
}

func assertProxyFetchesTarget(t *testing.T, targetURL, proxyURL string) {
	t.Helper()

	target := mustParseURL(t, targetURL)
	assertHostCannotResolveTarget(t, target.Hostname())

	body, err := fetchViaRawProxy(proxyURL, false, targetURL)
	if err != nil {
		t.Fatalf("expected proxied request via %s to succeed, got %v", proxyURL, err)
	}
	if !strings.Contains(body, "proxy-ok") {
		t.Fatalf("expected proxy target response, got %q", body)
	}
}

func assertProxyPoolRotation(t *testing.T, targetURL string, pool []string) {
	t.Helper()

	engine := &proxyIntegrationEngine{
		targetURL: targetURL,
		limiter:   rate.NewLimiter(rate.Inf, 1),
	}

	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 1
	opts.Resilience.Retry.InitialBackoff = 0
	opts.Resilience.Retry.MaxBackoff = 0
	opts.Resilience.Retry.BackoffFactor = 1
	opts.Resilience.Proxy = ProxyConfig{
		Runtime:              ProxyRuntimeRaw,
		PoolURLs:             pool,
		PoolFailureThreshold: 1,
	}

	srv := NewServerWithOptions("127.0.0.1", 7190, opts, engine)
	resp := request(t, srv, "/google/search?text=proxy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected rotated proxy request to succeed, got %d", resp.StatusCode)
	}

	if len(engine.proxies) != 2 {
		t.Fatalf("expected 2 proxy attempts, got %d", len(engine.proxies))
	}
	if engine.proxies[0] != pool[0] || engine.proxies[1] != pool[1] {
		t.Fatalf("unexpected proxy rotation order: %#v", engine.proxies)
	}

	statsResp := request(t, srv, "/resilience/stats")
	var stats map[string]interface{}
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	proxyStats := stats["proxy"].(map[string]interface{})
	poolStats := proxyStats["pool"].(map[string]interface{})
	if got := poolStats["active"].(float64); got != 1 {
		t.Fatalf("expected 1 active proxy, got %v", got)
	}
	if got := poolStats["disabled"].(float64); got != 1 {
		t.Fatalf("expected 1 disabled proxy, got %v", got)
	}
}

func fetchViaRawProxy(proxyURL string, insecure bool, targetURL string) (string, error) {
	client, err := NewRawHTTPClient(Query{
		ProxyURL: proxyURL,
		Insecure: insecure,
	})
	if err != nil {
		return "", err
	}

	resp, err := client.Get(targetURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

func assertHostCannotResolveTarget(t *testing.T, hostname string) {
	t.Helper()

	if _, err := net.DefaultResolver.LookupHost(context.Background(), hostname); err == nil {
		t.Fatalf("expected direct host-side DNS lookup for %q to fail", hostname)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse target URL %q: %v", raw, err)
	}
	return parsed
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

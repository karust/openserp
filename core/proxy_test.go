package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	socks5 "github.com/armon/go-socks5"
	xcontext "golang.org/x/net/context"
)

type timeoutTestError struct{}

func (timeoutTestError) Error() string   { return "i/o timeout" }
func (timeoutTestError) Timeout() bool   { return true }
func (timeoutTestError) Temporary() bool { return true }

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

func TestNormalizeProxiesConfigDefaultsAndDeduplicates(t *testing.T) {
	cfg, err := NormalizeProxiesConfig(ProxiesConfig{
		Global: " HTTP://proxy-global:8080 ",
		Entries: []ProxyEntryConfig{
			{URL: " http://proxy-one:8080 ", Tags: []string{"default", "us"}},
			{URL: "http://proxy-one:8080", Tags: []string{"de", "us"}},
			{URL: "socks5://proxy-two:1080", Tags: []string{"default"}},
		},
	})
	if err != nil {
		t.Fatalf("normalize proxies config: %v", err)
	}

	if cfg.Global != "http://proxy-global:8080" {
		t.Fatalf("expected normalized global proxy, got %q", cfg.Global)
	}
	if cfg.Health.FailureThreshold != DefaultProxyFailureThreshold {
		t.Fatalf("expected default failure threshold %d, got %d", DefaultProxyFailureThreshold, cfg.Health.FailureThreshold)
	}
	if len(cfg.Entries) != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d", len(cfg.Entries))
	}
	if cfg.Entries[0].URL != "http://proxy-one:8080" {
		t.Fatalf("unexpected normalized URL for first entry: %s", cfg.Entries[0].URL)
	}
	if len(cfg.Entries[0].Tags) != 3 {
		t.Fatalf("expected merged tags in first entry, got %#v", cfg.Entries[0].Tags)
	}
}

func TestNormalizeProxiesConfigRejectsInvalidEntries(t *testing.T) {
	_, err := NormalizeProxiesConfig(ProxiesConfig{
		Entries: []ProxyEntryConfig{{URL: "ftp://proxy:21", Tags: []string{"default"}}},
	})
	if err == nil {
		t.Fatal("expected invalid scheme error")
	}

	_, err = NormalizeProxiesConfig(ProxiesConfig{
		Entries: []ProxyEntryConfig{{URL: "http://proxy:8080", Tags: []string{" "}}},
	})
	if err == nil {
		t.Fatal("expected empty tags error")
	}

	_, err = NormalizeProxiesConfig(ProxiesConfig{
		Global:  "ftp://proxy:21",
		Entries: []ProxyEntryConfig{{URL: "http://proxy:8080", Tags: []string{"default"}}},
	})
	if err == nil {
		t.Fatal("expected invalid global proxy scheme error")
	}
}

func TestResolveEffectiveProxyPolicy(t *testing.T) {
	offPolicy := ResolveEffectiveProxyPolicy("", "")
	if offPolicy.Mode != ProxyModeOff {
		t.Fatalf("expected mode off, got %s", offPolicy.Mode)
	}
	if offPolicy.Tag != "" {
		t.Fatalf("expected empty tag for off mode, got %q", offPolicy.Tag)
	}

	tagOnlyPolicy := ResolveEffectiveProxyPolicy("", "US")
	if tagOnlyPolicy.Mode != ProxyModeTagPool || tagOnlyPolicy.Tag != "us" {
		t.Fatalf("unexpected effective policy with tag override: %#v", tagOnlyPolicy)
	}

	globalPolicy := ResolveEffectiveProxyPolicy("http://proxy-global:8080", "eu")
	if globalPolicy.Mode != ProxyModeTagPool || globalPolicy.Tag != "" {
		t.Fatalf("expected global proxy to ignore engine tags, got %#v", globalPolicy)
	}
}

func TestProxyRegistryRoundRobinAndFailureRecovery(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
		{URL: "http://proxy2:8080", Tags: []string{"default"}},
	}, 2)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	ctx := context.Background()

	if got := registry.NextByTag("default"); got != "http://proxy1:8080" {
		t.Fatalf("expected first proxy1, got %s", got)
	}
	if got := registry.NextByTag("default"); got != "http://proxy2:8080" {
		t.Fatalf("expected second proxy2, got %s", got)
	}

	registry.ReportFailure(ctx, "http://proxy1:8080")
	registry.ReportFailure(ctx, "http://proxy1:8080")
	if got := registry.NextByTag("default"); got != "http://proxy2:8080" {
		t.Fatalf("expected proxy2 while proxy1 disabled, got %s", got)
	}

	// Exhaust pool; quarantine kicks in, no proxy served immediately.
	registry.ReportFailure(ctx, "http://proxy2:8080")
	registry.ReportFailure(ctx, "http://proxy2:8080")
	if got := registry.NextByTag("default"); got != "" {
		t.Fatalf("expected empty while pool is quarantined, got %s", got)
	}

	expireProxyTagQuarantine(t, registry, "default")

	// After quarantine expiry, one probe proxy is re-enabled.
	got := registry.NextByTag("default")
	if got == "" {
		t.Fatal("expected a probe proxy after quarantine expiry, got empty")
	}

	// A success on the probe clears the quarantine and re-enables the pool.
	registry.ReportSuccess(ctx, got)
	stats := registry.BuildStats()
	if stats.UnhealthyCount != 1 {
		t.Fatalf("expected one still-disabled proxy after single probe recovery, got unhealthy_count=%d", stats.UnhealthyCount)
	}
	if stats.HealthyCount != 1 {
		t.Fatalf("expected one healthy proxy after probe success, got %d", stats.HealthyCount)
	}
}

func TestMaskProxyURLRedactsCredentials(t *testing.T) {
	if got := MaskProxyURL("http://user:pass@127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected masked proxy value: %s", got)
	}
}

func TestBrowserOptsLogFieldsRedactSecrets(t *testing.T) {
	fields := browserOptsLogFields(BrowserOpts{
		ProxyURL:             "http://user:sentinel-password@proxy.example:8080",
		CaptchaSolverApiKey:  "captcha-secret",
		CaptchaSolverEnabled: true,
	})
	rendered := fmt.Sprintf("%v", fields)
	if strings.Contains(rendered, "sentinel-password") || strings.Contains(rendered, "captcha-secret") {
		t.Fatalf("browser option log fields leaked secret: %s", rendered)
	}
	if fields["proxy"] != "http://proxy.example:8080" {
		t.Fatalf("expected masked proxy field, got %#v", fields["proxy"])
	}
	if fields["captcha_solver_has_key"] != true {
		t.Fatalf("expected captcha key presence boolean")
	}
}

func TestProxyURLForBrowserLaunchStripsCredentials(t *testing.T) {
	u, err := url.Parse("http://user:pass@127.0.0.1:18888")
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	got := proxyURLForBrowserLaunch(u)
	want := "http://127.0.0.1:18888"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestProxyURLForBrowserLaunchNormalizesSocks5h(t *testing.T) {
	u, err := url.Parse("socks5h://test:test@127.0.0.1:19080")
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	got := proxyURLForBrowserLaunch(u)
	want := "socks5://127.0.0.1:19080"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestProxyStatsMaskCredentials(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://user:pass@proxy.example:8080", Tags: []string{"default"}},
	}, 1)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	stats := registry.BuildStats()
	if len(stats.Entries) != 1 {
		t.Fatalf("expected one proxy stats entry, got %d", len(stats.Entries))
	}
	if got := stats.Entries[0].Proxy; got != "http://proxy.example:8080" {
		t.Fatalf("expected masked proxy in stats, got %q", got)
	}
}

func TestNormalizeProxyTag(t *testing.T) {
	tag, err := NormalizeProxyTag(" US ")
	if err != nil {
		t.Fatalf("normalize proxy tag: %v", err)
	}
	if tag != "us" {
		t.Fatalf("expected normalized tag us, got %q", tag)
	}

	if _, err := NormalizeProxyTag("   "); err == nil {
		t.Fatal("expected empty proxy tag validation error")
	}
}

func TestNormalizeProxyRequestOverride(t *testing.T) {
	override, err := NormalizeProxyRequestOverride(" direct ")
	if err != nil {
		t.Fatalf("normalize direct proxy override: %v", err)
	}
	if override != ProxyOverrideDirect {
		t.Fatalf("expected direct override, got %q", override)
	}

	override, err = NormalizeProxyRequestOverride(" US ")
	if err != nil {
		t.Fatalf("normalize tagged proxy override: %v", err)
	}
	if override != "us" {
		t.Fatalf("expected normalized tag override us, got %q", override)
	}

	override, err = NormalizeProxyRequestOverride("")
	if err != nil {
		t.Fatalf("normalize empty override: %v", err)
	}
	if override != "" {
		t.Fatalf("expected empty override, got %q", override)
	}
}

func TestIsAuthenticatedSocksProxyURL(t *testing.T) {
	if !IsAuthenticatedSocksProxyURL("socks5h://user:pass@127.0.0.1:1080") {
		t.Fatal("expected authenticated socks proxy to be detected")
	}
	if IsAuthenticatedSocksProxyURL("socks5://127.0.0.1:1080") {
		t.Fatal("expected plain socks proxy to remain browser-compatible")
	}
	if IsAuthenticatedSocksProxyURL("http://user:pass@127.0.0.1:8080") {
		t.Fatal("expected HTTP auth proxy to remain browser-compatible")
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

func TestNewRawHTTPClientDirectTLSUsesHTTP1(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Proto))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	client, err := NewRawHTTPClient(Query{Insecure: true})
	if err != nil {
		t.Fatalf("new raw http client: %v", err)
	}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("expected direct TLS request to succeed, got %v", err)
	}
	defer DrainAndCloseResponse(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if string(body) != "HTTP/1.1" {
		t.Fatalf("expected raw client to use HTTP/1.1, got %q", string(body))
	}
}

func TestClassifyProxyNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "timeout", err: timeoutTestError{}, want: ErrTimeout},
		{name: "connect", err: errors.New("proxyconnect tcp: connection refused"), want: ErrProxyConnect},
		{name: "auth", err: errors.New("Proxy Authentication Required 407"), want: ErrProxyAuth},
		{name: "parser", err: ErrParser, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyProxyNetworkError(tt.err)
			if tt.want == nil {
				if got != tt.err {
					t.Fatalf("expected unchanged error, got %v", got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("expected original error to be preserved, got %v", got)
			}
		})
	}
}

func TestClassifyMainDocumentStatus(t *testing.T) {
	if !errors.Is(classifyMainDocumentStatus(http.StatusForbidden), ErrBlocked) {
		t.Fatal("expected 403 to classify as ErrBlocked")
	}
	if !errors.Is(classifyMainDocumentStatus(http.StatusTooManyRequests), ErrRateLimited) {
		t.Fatal("expected 429 to classify as ErrRateLimited")
	}
	if classifyMainDocumentStatus(http.StatusOK) != nil {
		t.Fatal("expected 200 to remain unclassified")
	}
}

func TestNewRawHTTPClientClassifiesProxyAuthFailure(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()

	client, err := NewRawHTTPClient(Query{ProxyURL: proxy.URL})
	if err != nil {
		t.Fatalf("new raw http client: %v", err)
	}

	resp, err := client.Get("http://example.com/")
	DrainAndCloseResponse(resp)
	if !errors.Is(err, ErrProxyAuth) {
		t.Fatalf("expected proxy auth error, got %v", err)
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

func expireProxyTagQuarantine(t *testing.T, registry *ProxyRegistry, tag string) {
	t.Helper()

	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.tagQuarantine[normalizeTag(tag)] = time.Now().Add(-time.Second)
}

func proxyHealthyCount(stats ProxyStats) int {
	count := 0
	for _, entry := range stats.Entries {
		if !entry.Disabled {
			count++
		}
	}
	return count
}

func TestReportFailureDoesNotDegradeProxyOnNonNetworkError(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
	}, 2)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	rs := &ResilientSearcher{proxyRegistry: registry}
	for _, err := range []error{ErrParser, ErrCaptcha} {
		for range 50 {
			rs.reportProxyAttempt(context.Background(), "http://proxy1:8080", err)
		}
	}

	stats := registry.BuildStats()
	if stats.UnhealthyCount != 0 {
		t.Fatalf("non-network errors must not degrade proxy health: unhealthy_count=%d", stats.UnhealthyCount)
	}
}

func TestReportFailureDegradeProxyOnNetworkError(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
	}, 2)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	rs := &ResilientSearcher{proxyRegistry: registry}
	rs.reportProxyAttempt(context.Background(), "http://proxy1:8080", ErrProxyConnect)
	rs.reportProxyAttempt(context.Background(), "http://proxy1:8080", ErrProxyConnect)

	stats := registry.BuildStats()
	if stats.UnhealthyCount != 1 {
		t.Fatalf("proxy network errors must degrade proxy: unhealthy_count=%d", stats.UnhealthyCount)
	}
}

func TestPoolQuarantineAfterExhaustion(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
	}, 1)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	ctx := context.Background()

	// Exhaust the single proxy; this should set a quarantine.
	registry.ReportFailure(ctx, "http://proxy1:8080")

	// Verify quarantine is set and NextByTag returns empty.
	got := registry.NextByTagWithContext(ctx, "default")
	if got != "" {
		t.Fatalf("expected empty result during quarantine, got %q", got)
	}
}

func TestPoolQuarantineExpiresAndProbesSingleProxy(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
		{URL: "http://proxy2:8080", Tags: []string{"default"}},
	}, 1)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	ctx := context.Background()

	// Exhaust both proxies.
	registry.ReportFailure(ctx, "http://proxy1:8080")
	registry.ReportFailure(ctx, "http://proxy2:8080")

	expireProxyTagQuarantine(t, registry, "default")

	// After expiry, exactly one proxy should be re-enabled as a probe.
	got := registry.NextByTagWithContext(ctx, "default")
	if got == "" {
		t.Fatal("expected a proxy after quarantine expiry, got empty")
	}

	stats := registry.BuildStats()
	if got := proxyHealthyCount(stats); got != 1 {
		t.Fatalf("expected exactly 1 probe proxy re-enabled after quarantine, got %d healthy", got)
	}
}

func TestPoolQuarantineRestartsAfterFailedProbe(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
	}, 1)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	ctx := context.Background()
	registry.ReportFailure(ctx, "http://proxy1:8080")
	expireProxyTagQuarantine(t, registry, "default")

	probe := registry.NextByTagWithContext(ctx, "default")
	if probe == "" {
		t.Fatal("expected probe proxy after quarantine expiry")
	}

	registry.ReportFailure(ctx, probe)
	if got := registry.NextByTagWithContext(ctx, "default"); got != "" {
		t.Fatalf("expected renewed quarantine after failed probe, got %q", got)
	}
}

func TestReportSuccessClearsQuarantine(t *testing.T) {
	registry, err := NewProxyRegistry([]ProxyEntryConfig{
		{URL: "http://proxy1:8080", Tags: []string{"default"}},
	}, 1)
	if err != nil {
		t.Fatalf("new proxy registry: %v", err)
	}

	ctx := context.Background()
	registry.ReportFailure(ctx, "http://proxy1:8080")

	// Confirm quarantine set.
	if registry.NextByTagWithContext(ctx, "default") != "" {
		t.Fatal("expected pool to be quarantined after exhaustion")
	}

	// Recovery: success clears quarantine.
	registry.ReportSuccess(ctx, "http://proxy1:8080")

	got := registry.NextByTagWithContext(ctx, "default")
	if got == "" {
		t.Fatal("expected proxy available after successful recovery")
	}
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

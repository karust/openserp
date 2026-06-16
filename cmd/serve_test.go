package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

func TestRawEngineCachesRateLimiter(t *testing.T) {
	engine := &rawEngine{name: "google"}
	first := engine.GetRateLimiter()
	if first == nil {
		t.Fatal("expected limiter")
	}
	if second := engine.GetRateLimiter(); second != first {
		t.Fatal("expected rawEngine to return the cached limiter")
	}
}

func TestCommandDefaultsToQuiet(t *testing.T) {
	if !commandDefaultsToQuiet(searchCMD) {
		t.Fatal("expected search command to default to quiet")
	}
	if commandDefaultsToQuiet(serveCMD) {
		t.Fatal("expected serve command to keep server logging by default")
	}
}

func TestBrowserPoolKey(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty -> direct", "", directBrowserKey},
		{"unauth http -> direct", "http://proxy.example:8080", directBrowserKey},
		{"unauth socks -> direct", "socks5://proxy.example:1080", directBrowserKey},
		{"auth socks -> direct (rejected upstream)", "socks5://user:pass@proxy.example:1080", directBrowserKey},
		{"auth http", "http://user:pass@proxy.example:8080", "http|proxy.example:8080|user"},
		{"auth https different scheme", "https://user:pass@proxy.example:8443", "https|proxy.example:8443|user"},
		{"different password same key", "http://user:other-pass@proxy.example:8080", "http|proxy.example:8080|user"},
		{"different user different key", "http://user2:pass@proxy.example:8080", "http|proxy.example:8080|user2"},
		{"different host different key", "http://user:pass@proxy2.example:8080", "http|proxy2.example:8080|user"},
		{"different port different key", "http://user:pass@proxy.example:9090", "http|proxy.example:9090|user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := browserPoolKey(tc.raw); got != tc.want {
				t.Fatalf("browserPoolKey(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBrowserLaunchURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty -> empty", "", ""},
		{"unauth http -> empty (per-context path)", "http://proxy.example:8080", ""},
		{"unauth socks -> empty", "socks5://proxy.example:1080", ""},
		{"auth http -> normalized", "http://user:pass@proxy.example:8080", "http://user:pass@proxy.example:8080"},
		{"auth https -> normalized", "https://u:p@proxy.example:8443", "https://u:p@proxy.example:8443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := browserLaunchURL(tc.raw)
			if got != tc.want {
				t.Fatalf("browserLaunchURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBrowserPoolEvictLRU(t *testing.T) {
	// Pre-populate with bare entries (browser=nil) so we exercise eviction
	// without launching real Chrome. closePooledBrowser handles nil safely.
	pool := &browserPool{
		maxProcesses: 2,
		browsers:     map[string]*pooledBrowser{},
		stopSweeper:  make(chan struct{}),
		sweeperDone:  make(chan struct{}),
	}
	close(pool.sweeperDone)

	now := time.Now()
	pool.browsers["a"] = &pooledBrowser{lastUsedAt: now.Add(-3 * time.Second)}
	pool.browsers["b"] = &pooledBrowser{lastUsedAt: now.Add(-2 * time.Second)}
	pool.browsers["c"] = &pooledBrowser{lastUsedAt: now.Add(-1 * time.Second)}

	pool.mu.Lock()
	pool.evictLRULocked()
	pool.mu.Unlock()

	if _, ok := pool.browsers["a"]; ok {
		t.Fatal("expected oldest entry 'a' to be evicted")
	}
	if _, ok := pool.browsers["b"]; !ok {
		t.Fatal("expected entry 'b' to remain")
	}
	if _, ok := pool.browsers["c"]; !ok {
		t.Fatal("expected entry 'c' to remain")
	}
	if pool.evictedLRU != 1 {
		t.Fatalf("expected 1 LRU eviction, got %d", pool.evictedLRU)
	}
}

func TestBrowserPoolBrowserStats(t *testing.T) {
	pool := &browserPool{
		maxProcesses: 4,
		browsers:     map[string]*pooledBrowser{},
		stopSweeper:  make(chan struct{}),
		sweeperDone:  make(chan struct{}),
	}
	close(pool.sweeperDone)

	// Pre-bound entry without a launched browser should not count as active.
	pool.browsers["pre-bound"] = &pooledBrowser{launchProxyURL: "http://u:p@proxy.example:8080", lastUsedAt: time.Now()}
	pool.evictedLRU = 2
	pool.evictedIdle = 5

	stats := pool.browserStats()
	if stats.Max != 4 {
		t.Fatalf("expected max=4, got %d", stats.Max)
	}
	if stats.Active != 0 {
		t.Fatalf("expected active=0 (entry has no live browser), got %d", stats.Active)
	}
	if stats.EvictedLRU != 2 || stats.EvictedIdle != 5 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestValidateBrowserProxyPolicyRejectsAuthenticatedSocks(t *testing.T) {
	tests := []struct {
		name     string
		proxyCfg core.ProxyConfig
		policy   core.ProxyPolicy
	}{
		{
			name: "global authenticated socks",
			proxyCfg: core.ProxyConfig{
				Proxies: core.ProxiesConfig{
					Global: "socks5h://user:pass@127.0.0.1:1080",
				},
			},
			policy: core.ProxyPolicy{Mode: core.ProxyModeTagPool},
		},
		{
			name: "tag pool authenticated socks",
			proxyCfg: core.ProxyConfig{
				Proxies: core.ProxiesConfig{
					Entries: []core.ProxyEntryConfig{
						{URL: "socks5://user:pass@127.0.0.1:1080", Tags: []string{"us"}},
					},
				},
			},
			policy: core.ProxyPolicy{Mode: core.ProxyModeTagPool, Tag: "us"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBrowserProxyPolicy(tt.proxyCfg, tt.policy)
			if err == nil {
				t.Fatal("expected browser proxy validation to fail")
			}
			if !strings.Contains(err.Error(), "authenticated SOCKS proxy") {
				t.Fatalf("expected explicit authenticated SOCKS error, got %v", err)
			}
		})
	}
}

func TestValidateBrowserProxyPolicyAllowsHTTPAuthAndPlainSocks(t *testing.T) {
	tests := []struct {
		name     string
		proxyCfg core.ProxyConfig
		policy   core.ProxyPolicy
	}{
		{
			name: "global http auth",
			proxyCfg: core.ProxyConfig{
				Proxies: core.ProxiesConfig{
					Global: "http://user:pass@127.0.0.1:8080",
				},
			},
			policy: core.ProxyPolicy{Mode: core.ProxyModeTagPool},
		},
		{
			name: "tag pool plain socks",
			proxyCfg: core.ProxyConfig{
				Proxies: core.ProxiesConfig{
					Entries: []core.ProxyEntryConfig{
						{URL: "socks5://127.0.0.1:1080", Tags: []string{"eu"}},
					},
				},
			},
			policy: core.ProxyPolicy{Mode: core.ProxyModeTagPool, Tag: "eu"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBrowserProxyPolicy(tt.proxyCfg, tt.policy); err != nil {
				t.Fatalf("expected browser proxy validation to succeed, got %v", err)
			}
		})
	}
}

func TestValidateBrowserProxyPolicyRejectsTaggedAuthenticatedSocksInPool(t *testing.T) {
	proxyCfg := core.ProxyConfig{
		Proxies: core.ProxiesConfig{
			Entries: []core.ProxyEntryConfig{
				{URL: "http://127.0.0.1:8080", Tags: []string{"default"}},
				{URL: "socks5://user:pass@127.0.0.1:1080", Tags: []string{"default"}},
			},
		},
	}

	err := validateBrowserProxyPolicy(proxyCfg, core.ProxyPolicy{Mode: core.ProxyModeTagPool, Tag: "default"})
	if err == nil {
		t.Fatal("expected browser proxy validation to fail for tag pool")
	}
	if !strings.Contains(err.Error(), "authenticated SOCKS proxy") {
		t.Fatalf("expected explicit authenticated SOCKS error, got %v", err)
	}
}

func TestResolveCaptchaSolverConfigDisabledWithoutKey(t *testing.T) {
	origEnabled := config.Captcha.SolverEnabled
	origKey := config.Config2Capcha.ApiKey
	defer func() {
		config.Captcha.SolverEnabled = origEnabled
		config.Config2Capcha.ApiKey = origKey
	}()

	config.Captcha.SolverEnabled = false
	config.Config2Capcha.ApiKey = ""

	enabled, key, err := resolveCaptchaSolverConfig()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if enabled {
		t.Fatal("expected solver to be disabled")
	}
	if key != "" {
		t.Fatalf("expected empty solver key when disabled, got %q", key)
	}
}

func TestResolveCaptchaSolverConfigEnabledWithoutKeyFails(t *testing.T) {
	origEnabled := config.Captcha.SolverEnabled
	origKey := config.Config2Capcha.ApiKey
	defer func() {
		config.Captcha.SolverEnabled = origEnabled
		config.Config2Capcha.ApiKey = origKey
	}()

	config.Captcha.SolverEnabled = true
	config.Config2Capcha.ApiKey = ""

	_, _, err := resolveCaptchaSolverConfig()
	if err == nil {
		t.Fatal("expected missing API key error")
	}
	if !strings.Contains(err.Error(), "captcha solver is enabled") {
		t.Fatalf("expected clear startup error, got %v", err)
	}
}

func TestResolveCaptchaSolverConfigEnabledWithKey(t *testing.T) {
	origEnabled := config.Captcha.SolverEnabled
	origKey := config.Config2Capcha.ApiKey
	defer func() {
		config.Captcha.SolverEnabled = origEnabled
		config.Config2Capcha.ApiKey = origKey
	}()

	config.Captcha.SolverEnabled = true
	config.Config2Capcha.ApiKey = "api-key"

	enabled, key, err := resolveCaptchaSolverConfig()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !enabled {
		t.Fatal("expected solver to be enabled")
	}
	if key != "api-key" {
		t.Fatalf("expected configured API key, got %q", key)
	}
}

func TestBuildFingerprintBrowserOptionsRespectsBlockConfig(t *testing.T) {
	origBlockResources := config.App.BlockResources
	origBlockTrackers := config.App.BlockTrackers
	origHead := config.App.IsBrowserHead
	origLeakless := config.App.IsLeakless
	origTimeout := config.App.Timeout
	origBrowserPath := config.App.BrowserPath
	origInsecure := config.Server.Insecure
	origDebug := config.Server.IsDebug
	defer func() {
		config.App.BlockResources = origBlockResources
		config.App.BlockTrackers = origBlockTrackers
		config.App.IsBrowserHead = origHead
		config.App.IsLeakless = origLeakless
		config.App.Timeout = origTimeout
		config.App.BrowserPath = origBrowserPath
		config.Server.Insecure = origInsecure
		config.Server.IsDebug = origDebug
	}()

	config.App.BlockResources = "image,font,css,media"
	config.App.BlockTrackers = true
	config.App.IsBrowserHead = false
	config.App.IsLeakless = false
	config.App.Timeout = 15
	config.App.BrowserPath = ""
	config.Server.Insecure = false
	config.Server.IsDebug = false

	opts := buildFingerprintBrowserOptions()
	if len(opts.BlockResourceTypes) != 4 {
		t.Fatalf("expected 4 blocked resource types, got %d", len(opts.BlockResourceTypes))
	}
	if !opts.BlockTrackers {
		t.Fatal("expected tracker blocking to be enabled")
	}

}

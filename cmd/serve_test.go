package cmd

import (
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

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

package core

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	ProxyRuntimeBrowser              = "browser"
	ProxyRuntimeRaw                  = "raw"
	ProxyModeDisabled                = "disabled"
	ProxyModeStatic                  = "static"
	ProxyModePool                    = "pool"
	DefaultProxyPoolFailureThreshold = 3
)

var supportedProxySchemes = map[string]struct{}{
	"http":    {},
	"https":   {},
	"socks5":  {},
	"socks5h": {},
}

type ProxyConfig struct {
	Runtime              string
	StaticURL            string
	PoolURLs             []string
	PoolFailureThreshold int
}

type ProxyPool struct {
	mu               sync.Mutex
	proxies          []ProxyEntry
	next             int
	failureThreshold int
}

type ProxyEntry struct {
	URL        string
	FailCount  int
	IsDisabled bool
}

type ProxyPoolStats struct {
	FailureThreshold int
	Total            int
	Active           int
	Disabled         int
}

func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		Runtime:              ProxyRuntimeBrowser,
		PoolFailureThreshold: DefaultProxyPoolFailureThreshold,
	}
}

func NormalizeProxyConfig(cfg ProxyConfig) (ProxyConfig, error) {
	cfg.Runtime = normalizeProxyRuntime(cfg.Runtime)
	if cfg.PoolFailureThreshold <= 0 {
		cfg.PoolFailureThreshold = DefaultProxyPoolFailureThreshold
	}

	staticURL, err := NormalizeProxyURL(cfg.StaticURL)
	if err != nil {
		return cfg, fmt.Errorf("invalid static proxy: %w", err)
	}
	poolURLs, err := NormalizeProxyURLs(cfg.PoolURLs)
	if err != nil {
		return cfg, fmt.Errorf("invalid proxy pool: %w", err)
	}

	cfg.StaticURL = staticURL
	cfg.PoolURLs = poolURLs
	return cfg, nil
}

func NormalizeProxyURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		return "", fmt.Errorf("proxy URL must include a scheme")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("proxy URL must include a host")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if _, ok := supportedProxySchemes[parsed.Scheme]; !ok {
		return "", fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}

	return parsed.String(), nil
}

func NormalizeProxyURLs(rawURLs []string) ([]string, error) {
	normalized := make([]string, 0, len(rawURLs))
	seen := make(map[string]struct{}, len(rawURLs))

	for _, raw := range rawURLs {
		proxyURL, err := NormalizeProxyURL(raw)
		if err != nil {
			return nil, err
		}
		if proxyURL == "" {
			continue
		}
		if _, ok := seen[proxyURL]; ok {
			continue
		}
		seen[proxyURL] = struct{}{}
		normalized = append(normalized, proxyURL)
	}

	return normalized, nil
}

func MaskProxyURL(raw string) string {
	proxyURL, err := NormalizeProxyURL(raw)
	if err != nil || proxyURL == "" {
		return "invalid-proxy"
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return "invalid-proxy"
	}

	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
}

func NewProxyPool(proxyURLs []string, failureThreshold int) (*ProxyPool, error) {
	normalizedURLs, err := NormalizeProxyURLs(proxyURLs)
	if err != nil {
		return nil, err
	}
	if failureThreshold <= 0 {
		failureThreshold = DefaultProxyPoolFailureThreshold
	}

	entries := make([]ProxyEntry, 0, len(normalizedURLs))
	for _, proxyURL := range normalizedURLs {
		entries = append(entries, ProxyEntry{URL: proxyURL})
	}

	return &ProxyPool{
		proxies:          entries,
		failureThreshold: failureThreshold,
	}, nil
}

func (p *ProxyPool) Next() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.proxies) == 0 {
		return ""
	}

	if p.allDisabledLocked() {
		logrus.Warn("Proxy pool exhausted, re-enabling all configured proxies")
		for i := range p.proxies {
			p.proxies[i].IsDisabled = false
			p.proxies[i].FailCount = 0
		}
	}

	for i := 0; i < len(p.proxies); i++ {
		idx := (p.next + i) % len(p.proxies)
		if p.proxies[idx].IsDisabled {
			continue
		}

		p.next = (idx + 1) % len(p.proxies)
		selected := p.proxies[idx].URL
		logrus.Debugf("Selected proxy from pool: %s", MaskProxyURL(selected))
		return selected
	}

	return ""
}

func (p *ProxyPool) ReportFailure(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.proxies {
		if p.proxies[i].URL != proxyURL {
			continue
		}

		p.proxies[i].FailCount++
		if p.proxies[i].FailCount >= p.failureThreshold {
			p.proxies[i].IsDisabled = true
			logrus.Warnf(
				"Disabled proxy after %d failures: %s",
				p.proxies[i].FailCount,
				MaskProxyURL(proxyURL),
			)
		}
		return
	}
}

func (p *ProxyPool) ReportSuccess(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.proxies {
		if p.proxies[i].URL != proxyURL {
			continue
		}

		p.proxies[i].FailCount = 0
		p.proxies[i].IsDisabled = false
		return
	}
}

func (p *ProxyPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.proxies)
}

func (p *ProxyPool) Stats() ProxyPoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := ProxyPoolStats{
		FailureThreshold: p.failureThreshold,
		Total:            len(p.proxies),
	}
	for _, proxy := range p.proxies {
		if proxy.IsDisabled {
			stats.Disabled++
			continue
		}
		stats.Active++
	}

	return stats
}

func (p *ProxyPool) allDisabledLocked() bool {
	if len(p.proxies) == 0 {
		return false
	}
	for _, proxy := range p.proxies {
		if !proxy.IsDisabled {
			return false
		}
	}
	return true
}

func normalizeProxyRuntime(runtime string) string {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case ProxyRuntimeRaw:
		return ProxyRuntimeRaw
	default:
		return ProxyRuntimeBrowser
	}
}

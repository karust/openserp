package core

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	ProxyRuntimeBrowser          = "browser"
	ProxyRuntimeRaw              = "raw"
	ProxyModeOff                 = "off"
	ProxyModeTagPool             = "tag_pool"
	DefaultProxyFailureThreshold = 3
	ProxyOverrideDirect          = "direct"
)

var supportedProxySchemes = map[string]struct{}{
	"http":    {},
	"https":   {},
	"socks5":  {},
	"socks5h": {},
}

var ErrProxyUnavailable = errors.New("proxy unavailable")

type ProxyPolicy struct {
	Mode string `json:"mode" mapstructure:"mode"`
	Tag  string `json:"tag,omitempty" mapstructure:"tag"`
}

type ProxyEntryConfig struct {
	URL  string   `json:"url" mapstructure:"url"`
	Tags []string `json:"tags" mapstructure:"tags"`
}

type ProxiesHealthConfig struct {
	FailureThreshold int `json:"failure_threshold" mapstructure:"failure_threshold"`
}

type ProxiesConfig struct {
	Global  string              `json:"global,omitempty" mapstructure:"global"`
	Entries []ProxyEntryConfig  `json:"entries" mapstructure:"entries"`
	Health  ProxiesHealthConfig `json:"health" mapstructure:"health"`
}

type ProxyConfig struct {
	Runtime        string            // raw or browser runtime behavior
	Proxies        ProxiesConfig     // canonical proxy inventory
	EnginePolicies map[string]string // engine-specific proxy tags
	Registry       *ProxyRegistry    // optional shared registry from caller
}

type ProxyTagSummary struct {
	Configured int `json:"configured"`
	Healthy    int `json:"healthy"`
}

type ProxyStatsEntry struct {
	Proxy    string   `json:"proxy"`
	Tags     []string `json:"tags"`
	Healthy  bool     `json:"healthy"`
	Failures int      `json:"failures"`
	Disabled bool     `json:"disabled"`
}

type ProxyEngineStats struct {
	Tag           string `json:"tag,omitempty"`
	SelectedProxy string `json:"selected_proxy"`
}

type ProxyStats struct {
	ConfiguredCount int                         `json:"configured_count"`
	HealthyCount    int                         `json:"healthy_count"`
	UnhealthyCount  int                         `json:"unhealthy_count"`
	Tags            map[string]ProxyTagSummary  `json:"tags"`
	Entries         []ProxyStatsEntry           `json:"entries"`
	Engines         map[string]ProxyEngineStats `json:"engines,omitempty"`
}

type proxyState struct {
	url      string
	tags     []string
	failures int
	disabled bool
}

type ProxyRegistry struct {
	mu               sync.Mutex
	states           map[string]*proxyState
	order            []string
	tagIndex         map[string][]string
	nextByTag        map[string]int
	failureThreshold int
}

func DefaultProxiesConfig() ProxiesConfig {
	return ProxiesConfig{
		Global:  "",
		Entries: []ProxyEntryConfig{},
		Health:  ProxiesHealthConfig{FailureThreshold: DefaultProxyFailureThreshold},
	}
}

func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		Runtime:        ProxyRuntimeBrowser,
		Proxies:        DefaultProxiesConfig(),
		EnginePolicies: map[string]string{},
	}
}

func NormalizeProxyConfig(cfg ProxyConfig) (ProxyConfig, error) {
	cfg.Runtime = normalizeProxyRuntime(cfg.Runtime)

	var err error
	cfg.Proxies, err = NormalizeProxiesConfig(cfg.Proxies)
	if err != nil {
		return cfg, err
	}

	if cfg.EnginePolicies == nil {
		cfg.EnginePolicies = map[string]string{}
	}
	normalizedEnginePolicies := make(map[string]string, len(cfg.EnginePolicies))
	for rawEngine, rawTag := range cfg.EnginePolicies {
		engine := normalizeEngineName(rawEngine)
		if engine == "" {
			continue
		}
		tag := normalizeTag(rawTag)
		if tag == "" {
			continue
		}
		normalizedEnginePolicies[engine] = tag
	}
	cfg.EnginePolicies = normalizedEnginePolicies

	if cfg.Registry == nil {
		if len(cfg.Proxies.Entries) > 0 {
			registry, err := NewProxyRegistry(cfg.Proxies.Entries, cfg.Proxies.Health.FailureThreshold)
			if err != nil {
				return cfg, err
			}
			cfg.Registry = registry
		}
	}

	return cfg, nil
}

func NormalizeProxiesConfig(cfg ProxiesConfig) (ProxiesConfig, error) {
	global, err := NormalizeProxyURL(cfg.Global)
	if err != nil {
		return cfg, fmt.Errorf("invalid proxies.global: %w", err)
	}
	cfg.Global = global

	failureThreshold := cfg.Health.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = DefaultProxyFailureThreshold
	}

	normalizedEntries := make([]ProxyEntryConfig, 0, len(cfg.Entries))
	entryByURL := make(map[string]int, len(cfg.Entries))

	for i, rawEntry := range cfg.Entries {
		proxyURL, err := NormalizeProxyURL(rawEntry.URL)
		if err != nil {
			return cfg, fmt.Errorf("invalid proxies.entries[%d].url: %w", i, err)
		}
		if proxyURL == "" {
			return cfg, fmt.Errorf("invalid proxies.entries[%d].url: value is required", i)
		}

		tags, err := normalizeProxyTags(rawEntry.Tags)
		if err != nil {
			return cfg, fmt.Errorf("invalid proxies.entries[%d].tags: %w", i, err)
		}

		if idx, ok := entryByURL[proxyURL]; ok {
			normalizedEntries[idx].Tags = mergeTags(normalizedEntries[idx].Tags, tags)
			continue
		}

		normalizedEntries = append(normalizedEntries, ProxyEntryConfig{
			URL:  proxyURL,
			Tags: tags,
		})
		entryByURL[proxyURL] = len(normalizedEntries) - 1
	}

	cfg.Entries = normalizedEntries
	cfg.Health = ProxiesHealthConfig{FailureThreshold: failureThreshold}
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

func ResolveEffectiveProxyPolicy(globalProxyURL string, engineTag string) ProxyPolicy {
	if strings.TrimSpace(globalProxyURL) != "" {
		return ProxyPolicy{Mode: ProxyModeTagPool}
	}

	tag := normalizeTag(engineTag)
	if tag == "" {
		return ProxyPolicy{Mode: ProxyModeOff}
	}

	return ProxyPolicy{Mode: ProxyModeTagPool, Tag: tag}
}

func NormalizeProxyTag(raw string) (string, error) {
	tag := normalizeTag(raw)
	if tag == "" {
		return "", fmt.Errorf("value is required")
	}
	return tag, nil
}

func NormalizeProxyRequestOverride(raw string) (string, error) {
	override := normalizeTag(raw)
	if override == "" {
		return "", nil
	}
	if override == ProxyOverrideDirect {
		return ProxyOverrideDirect, nil
	}
	return NormalizeProxyTag(override)
}

func IsAuthenticatedSocksProxyURL(raw string) bool {
	normalized, err := NormalizeProxyURL(raw)
	if err != nil || normalized == "" {
		return false
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return false
	}

	if (parsed.Scheme == "socks5" || parsed.Scheme == "socks5h") && parsed.User != nil {
		return true
	}

	return false
}

func NewProxyRegistry(entries []ProxyEntryConfig, failureThreshold int) (*ProxyRegistry, error) {
	if failureThreshold <= 0 {
		failureThreshold = DefaultProxyFailureThreshold
	}

	states := make(map[string]*proxyState, len(entries))
	order := make([]string, 0, len(entries))
	tagIndex := make(map[string][]string)

	for idx, entry := range entries {
		proxyURL, err := NormalizeProxyURL(entry.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy registry entry[%d] url: %w", idx, err)
		}
		if proxyURL == "" {
			return nil, fmt.Errorf("invalid proxy registry entry[%d] url: value is required", idx)
		}

		tags, err := normalizeProxyTags(entry.Tags)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy registry entry[%d] tags: %w", idx, err)
		}

		states[proxyURL] = &proxyState{url: proxyURL, tags: tags}
		order = append(order, proxyURL)
		for _, tag := range tags {
			tagIndex[tag] = append(tagIndex[tag], proxyURL)
		}
	}

	return &ProxyRegistry{
		states:           states,
		order:            order,
		tagIndex:         tagIndex,
		nextByTag:        make(map[string]int, len(tagIndex)),
		failureThreshold: failureThreshold,
	}, nil
}

func (r *ProxyRegistry) NextByTag(tag string) string {
	tag = normalizeTag(tag)
	if tag == "" {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	urls := r.tagIndex[tag]
	if len(urls) == 0 {
		return ""
	}

	if r.allDisabledLocked(urls) {
		logrus.Warnf("Proxy tag pool exhausted for %q, re-enabling tagged proxies", tag)
		for _, proxyURL := range urls {
			state := r.states[proxyURL]
			state.disabled = false
			state.failures = 0
		}
	}

	start := r.nextByTag[tag]
	for i := 0; i < len(urls); i++ {
		idx := (start + i) % len(urls)
		proxyURL := urls[idx]
		state := r.states[proxyURL]
		if state.disabled {
			continue
		}

		r.nextByTag[tag] = (idx + 1) % len(urls)
		logrus.Debugf("Selected proxy for tag=%s: %s", tag, MaskProxyURL(proxyURL))
		return proxyURL
	}

	return ""
}

func (r *ProxyRegistry) ReportFailure(proxyURL string) {
	proxyURL, err := NormalizeProxyURL(proxyURL)
	if err != nil || proxyURL == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.states[proxyURL]
	if !ok {
		return
	}

	state.failures++
	if state.failures >= r.failureThreshold {
		state.disabled = true
		logrus.Warnf("Disabled proxy after %d failures: %s", state.failures, MaskProxyURL(proxyURL))
	}
}

func (r *ProxyRegistry) ReportSuccess(proxyURL string) {
	proxyURL, err := NormalizeProxyURL(proxyURL)
	if err != nil || proxyURL == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.states[proxyURL]
	if !ok {
		return
	}

	state.failures = 0
	state.disabled = false
}

func (r *ProxyRegistry) HasHealthyProxyForTag(tag string) bool {
	tag = normalizeTag(tag)
	if tag == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, proxyURL := range r.tagIndex[tag] {
		if state, ok := r.states[proxyURL]; ok && !state.disabled {
			return true
		}
	}

	return false
}

func (r *ProxyRegistry) BuildStats() ProxyStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := ProxyStats{
		Tags:    map[string]ProxyTagSummary{},
		Entries: make([]ProxyStatsEntry, 0, len(r.order)),
	}

	for _, proxyURL := range r.order {
		state := r.states[proxyURL]
		healthy := !state.disabled
		if healthy {
			stats.HealthyCount++
		} else {
			stats.UnhealthyCount++
		}
		stats.ConfiguredCount++

		stats.Entries = append(stats.Entries, ProxyStatsEntry{
			Proxy:    MaskProxyURL(state.url),
			Tags:     append([]string(nil), state.tags...),
			Healthy:  healthy,
			Failures: state.failures,
			Disabled: state.disabled,
		})

		for _, tag := range state.tags {
			summary := stats.Tags[tag]
			summary.Configured++
			if healthy {
				summary.Healthy++
			}
			stats.Tags[tag] = summary
		}
	}

	return stats
}

func (r *ProxyRegistry) allDisabledLocked(urls []string) bool {
	if len(urls) == 0 {
		return false
	}
	for _, proxyURL := range urls {
		if state, ok := r.states[proxyURL]; ok && !state.disabled {
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

func normalizeProxyTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, fmt.Errorf("at least one tag is required")
	}

	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, rawTag := range tags {
		tag := normalizeTag(rawTag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}

	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one non-empty tag is required")
	}

	sort.Strings(normalized)
	return normalized, nil
}

func normalizeTag(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func mergeTags(base []string, additional []string) []string {
	combined := make(map[string]struct{}, len(base)+len(additional))
	for _, tag := range base {
		combined[tag] = struct{}{}
	}
	for _, tag := range additional {
		combined[tag] = struct{}{}
	}

	merged := make([]string, 0, len(combined))
	for tag := range combined {
		merged = append(merged, tag)
	}
	sort.Strings(merged)
	return merged
}

func normalizeEngineName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

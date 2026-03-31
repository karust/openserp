package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// ResilientSearcher wraps engines with retry and circuit breaker protection.
type ResilientSearcher struct {
	engines   []SearchEngine
	cbManager *CircuitBreakerManager
	retryCfg  RetryConfig

	proxyRuntime  string
	proxyCfg      ProxyConfig
	proxyRegistry *ProxyRegistry
	proxyDefaults ProxyPolicy

	effectivePolicies map[string]ProxyPolicy
}

type ProxyExecutionMeta struct {
	Mode string `json:"mode"`
	Tag  string `json:"tag,omitempty"`
	Used string `json:"used"`
}

type ResilientConfig struct {
	Retry          RetryConfig
	CircuitBreaker CircuitBreakerConfig
	Proxy          ProxyConfig
}

func DefaultResilientConfig() ResilientConfig {
	return ResilientConfig{
		Retry:          DefaultRetryConfig(),
		CircuitBreaker: DefaultCircuitBreakerConfig(),
		Proxy:          DefaultProxyConfig(),
	}
}

func NewResilientSearcher(engines []SearchEngine, cfg ResilientConfig) *ResilientSearcher {
	proxyCfg, err := NormalizeProxyConfig(cfg.Proxy)
	if err != nil {
		logrus.Errorf("Invalid proxy config, using defaults: %v", err)
		proxyCfg = DefaultProxyConfig()
		proxyCfg, _ = NormalizeProxyConfig(proxyCfg)
	}

	rs := &ResilientSearcher{
		engines:           engines,
		cbManager:         NewCircuitBreakerManager(cfg.CircuitBreaker),
		retryCfg:          cfg.Retry,
		proxyRuntime:      proxyCfg.Runtime,
		proxyCfg:          proxyCfg,
		proxyRegistry:     proxyCfg.Registry,
		proxyDefaults:     ResolveEffectiveProxyPolicy(proxyCfg.Proxies.Global, ""),
		effectivePolicies: make(map[string]ProxyPolicy, len(engines)),
	}

	for _, engine := range engines {
		engineName := normalizeEngineName(engine.Name())
		override := proxyCfg.EnginePolicies[engineName]
		effective := ResolveEffectiveProxyPolicy(proxyCfg.Proxies.Global, override)
		rs.effectivePolicies[engineName] = effective
	}

	return rs
}

// SearchPrimary keeps dedicated endpoints engine-pure (no fallback).
func (rs *ResilientSearcher) SearchPrimary(primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	results, proxyMeta, err := rs.searchWithProtection(primaryEngine, q, false)
	if err != nil {
		return nil, primaryEngine.Name(), proxyMeta, err
	}
	return results, primaryEngine.Name(), proxyMeta, nil
}

// SearchWithFallback retries primary and then tries other initialized engines.
func (rs *ResilientSearcher) SearchWithFallback(primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	return rs.searchWithFallback(primaryEngine, q, false)
}

func (rs *ResilientSearcher) SearchImagePrimary(primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	results, proxyMeta, err := rs.searchWithProtection(primaryEngine, q, true)
	if err != nil {
		return nil, primaryEngine.Name(), proxyMeta, err
	}
	return results, primaryEngine.Name(), proxyMeta, nil
}

func (rs *ResilientSearcher) SearchImageWithFallback(primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	return rs.searchWithFallback(primaryEngine, q, true)
}

func (rs *ResilientSearcher) searchWithFallback(primaryEngine SearchEngine, q Query, isImage bool) ([]SearchResult, string, ProxyExecutionMeta, error) {
	results, proxyMeta, err := rs.searchWithProtection(primaryEngine, q, isImage)
	if err == nil {
		return results, primaryEngine.Name(), proxyMeta, nil
	}
	if errors.Is(err, ErrProxyUnavailable) {
		logrus.Warnf("[Resilient] Primary engine %s proxy policy failed closed: %s", primaryEngine.Name(), err)
		return nil, primaryEngine.Name(), proxyMeta, err
	}

	action := "failed"
	successMessage := "Fallback to %s succeeded with %d results"
	if isImage {
		action = "image search failed"
		successMessage = "Image fallback to %s succeeded with %d results"
	}

	logrus.Warnf("[Resilient] Primary engine %s %s: %s. Trying fallback engines...", primaryEngine.Name(), action, err)
	for _, fallbackEngine := range rs.engines {
		if fallbackEngine.Name() == primaryEngine.Name() || !fallbackEngine.IsInitialized() {
			continue
		}

		results, fallbackMeta, fallbackErr := rs.searchWithProtection(fallbackEngine, q, isImage)
		if fallbackErr == nil {
			logrus.Infof("[Resilient] "+successMessage, fallbackEngine.Name(), len(results))
			return results, fallbackEngine.Name(), fallbackMeta, nil
		}
		logrus.Warnf("[Resilient] Fallback engine %s also failed: %s", fallbackEngine.Name(), fallbackErr)
	}

	return nil, primaryEngine.Name(), proxyMeta, ErrAllEnginesFailed
}

func (rs *ResilientSearcher) searchWithProtection(engine SearchEngine, q Query, isImage bool) ([]SearchResult, ProxyExecutionMeta, error) {
	cb := rs.cbManager.Get(engine.Name())
	if !cb.AllowRequest() {
		return nil, ProxyExecutionMeta{}, ErrCircuitOpen
	}

	policy := rs.effectivePolicyForEngine(engine.Name())
	attemptMeta := rs.baseProxyMeta(policy)

	result := RetryableSearch(rs.retryCfg, engine.Name(), func() ([]SearchResult, error) {
		limiter := engine.GetRateLimiter()
		if limiter != nil {
			if err := limiter.Wait(context.Background()); err != nil {
				return nil, err
			}
		}

		attemptQuery := q
		proxyURL := ""
		reportToRegistry := false
		attemptMeta = rs.baseProxyMeta(policy)

		switch policy.Mode {
		case ProxyModeOff:
			attemptQuery.ProxyURL = ""
			attemptMeta.Used = "direct"
		case ProxyModeTagPool:
			proxyURL = rs.selectProxyForPolicy(policy)
			if proxyURL == "" {
				return nil, fmt.Errorf("%w: no healthy proxy available for tag %q", ErrProxyUnavailable, policy.Tag)
			}
			attemptQuery.ProxyURL = proxyURL
			reportToRegistry = policy.Tag != ""
			attemptMeta.Used = MaskProxyURL(proxyURL)
		}

		var (
			results []SearchResult
			err     error
		)
		if isImage {
			results, err = engine.SearchImage(attemptQuery)
		} else {
			results, err = engine.Search(attemptQuery)
		}

		if reportToRegistry {
			rs.reportProxyAttempt(proxyURL, err)
		}

		return results, err
	})

	if result.Err != nil {
		if !errors.Is(result.Err, ErrProxyUnavailable) {
			cb.RecordFailure()
		}
		return nil, attemptMeta, result.Err
	}

	cb.RecordSuccess()
	return result.Results, attemptMeta, nil
}

// SearchAllParallel applies retry/circuit protections per engine for mega search.
func (rs *ResilientSearcher) SearchAllParallel(q Query, engines []SearchEngine) []MegaSearchResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		if !engine.IsInitialized() {
			continue
		}
		if !rs.cbManager.Get(engine.Name()).AllowRequest() {
			logrus.Infof("[Resilient] Skipping %s in megasearch (circuit open)", engine.Name())
			continue
		}

		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			results, _, err := rs.searchWithProtection(eng, q, false)
			if err != nil {
				return
			}

			mu.Lock()
			for _, r := range results {
				allResults = append(allResults, MegaSearchResult{
					SearchResult: r,
					Engine:       eng.Name(),
				})
			}
			mu.Unlock()
		}(engine)
	}

	wg.Wait()
	return allResults
}

func (rs *ResilientSearcher) SearchAllImageParallel(q Query, engines []SearchEngine) []MegaSearchResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		if !engine.IsInitialized() {
			continue
		}
		if !rs.cbManager.Get(engine.Name()).AllowRequest() {
			logrus.Infof("[Resilient] Skipping %s in megaimage (circuit open)", engine.Name())
			continue
		}

		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			results, _, err := rs.searchWithProtection(eng, q, true)
			if err != nil {
				return
			}

			mu.Lock()
			for _, r := range results {
				allResults = append(allResults, MegaSearchResult{
					SearchResult: r,
					Engine:       eng.Name(),
				})
			}
			mu.Unlock()
		}(engine)
	}

	wg.Wait()
	return allResults
}

func (rs *ResilientSearcher) GetCircuitBreakerStats() []map[string]interface{} {
	return rs.cbManager.AllStats()
}

func (rs *ResilientSearcher) GetProxyStats() ProxyStats {
	stats := ProxyStats{
		ConfiguredCount: 0,
		HealthyCount:    0,
		UnhealthyCount:  0,
		Tags:            map[string]ProxyTagSummary{},
		Entries:         []ProxyStatsEntry{},
	}

	if rs.proxyRegistry != nil {
		stats = rs.proxyRegistry.BuildStats()
	}

	engines := map[string]ProxyEngineStats{}
	for _, engine := range rs.engines {
		engineName := normalizeEngineName(engine.Name())
		policy := rs.effectivePolicyForEngine(engineName)
		engineStats := ProxyEngineStats{}

		switch policy.Mode {
		case ProxyModeOff:
			engineStats.SelectedProxy = "direct"
		case ProxyModeTagPool:
			engineStats.Tag = policy.Tag
			if global := strings.TrimSpace(rs.proxyCfg.Proxies.Global); global != "" {
				engineStats.SelectedProxy = MaskProxyURL(global)
			} else {
				engineStats.SelectedProxy = "pooled"
			}
		}

		engines[engineName] = engineStats
	}
	if len(engines) > 0 {
		stats.Engines = engines
	}

	return stats
}

func (rs *ResilientSearcher) ResolveMegaProxyMeta(engines []SearchEngine) ProxyExecutionMeta {
	if len(engines) == 0 {
		return ProxyExecutionMeta{Mode: ProxyModeOff, Used: "direct"}
	}

	allOff := true
	proxiedTags := map[string]struct{}{}
	hasOff := false

	for _, engine := range engines {
		policy := rs.effectivePolicyForEngine(engine.Name())
		if policy.Mode == ProxyModeOff {
			hasOff = true
			continue
		}

		allOff = false
		if policy.Tag != "" {
			proxiedTags[policy.Tag] = struct{}{}
		}
	}

	if allOff {
		return ProxyExecutionMeta{Mode: ProxyModeOff, Used: "direct"}
	}

	meta := ProxyExecutionMeta{Mode: ProxyModeTagPool}
	if len(proxiedTags) == 1 {
		for tag := range proxiedTags {
			meta.Tag = tag
		}
	}

	if global := strings.TrimSpace(rs.proxyCfg.Proxies.Global); global != "" && !hasOff {
		meta.Used = MaskProxyURL(global)
		return meta
	}

	if rs.proxyRuntime == ProxyRuntimeRaw {
		meta.Used = "multiple"
		if hasOff {
			meta.Used = "mixed"
		}
		return meta
	}
	meta.Used = "multiple"
	if hasOff {
		meta.Used = "mixed"
	}
	return meta
}

func (rs *ResilientSearcher) baseProxyMeta(policy ProxyPolicy) ProxyExecutionMeta {
	meta := ProxyExecutionMeta{Mode: policy.Mode}
	if policy.Mode == ProxyModeTagPool {
		meta.Tag = policy.Tag
		return meta
	}
	meta.Used = "direct"
	return meta
}

func (rs *ResilientSearcher) effectivePolicyForEngine(engineName string) ProxyPolicy {
	engineName = normalizeEngineName(engineName)
	if policy, ok := rs.effectivePolicies[engineName]; ok {
		return policy
	}
	return rs.proxyDefaults
}

func (rs *ResilientSearcher) selectProxyForTag(tag string) string {
	if rs.proxyRegistry == nil {
		return ""
	}
	return rs.proxyRegistry.NextByTag(tag)
}

func (rs *ResilientSearcher) reportProxyAttempt(proxyURL string, err error) {
	if rs.proxyRegistry == nil || proxyURL == "" {
		return
	}

	if err != nil {
		rs.proxyRegistry.ReportFailure(proxyURL)
		return
	}

	rs.proxyRegistry.ReportSuccess(proxyURL)
}

func (rs *ResilientSearcher) selectProxyForPolicy(policy ProxyPolicy) string {
	if policy.Mode != ProxyModeTagPool {
		return ""
	}
	if global := strings.TrimSpace(rs.proxyCfg.Proxies.Global); global != "" {
		return global
	}
	return rs.selectProxyForTag(policy.Tag)
}

var ErrAllEnginesFailed = fmt.Errorf("all search engines failed")

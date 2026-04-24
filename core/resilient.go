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
		logrus.WithError(err).Error("Invalid proxy config, using defaults")
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
func (rs *ResilientSearcher) SearchPrimary(ctx context.Context, primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	results, proxyMeta, err := rs.searchWithProtection(ctx, primaryEngine, q, false)
	if err != nil {
		return nil, primaryEngine.Name(), proxyMeta, err
	}
	return results, primaryEngine.Name(), proxyMeta, nil
}

// SearchWithFallback retries primary and then tries other initialized engines.
func (rs *ResilientSearcher) SearchWithFallback(ctx context.Context, primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	return rs.searchWithFallback(ctx, primaryEngine, q, false)
}

func (rs *ResilientSearcher) SearchImagePrimary(ctx context.Context, primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	results, proxyMeta, err := rs.searchWithProtection(ctx, primaryEngine, q, true)
	if err != nil {
		return nil, primaryEngine.Name(), proxyMeta, err
	}
	return results, primaryEngine.Name(), proxyMeta, nil
}

func (rs *ResilientSearcher) SearchImageWithFallback(ctx context.Context, primaryEngine SearchEngine, q Query) ([]SearchResult, string, ProxyExecutionMeta, error) {
	return rs.searchWithFallback(ctx, primaryEngine, q, true)
}

func (rs *ResilientSearcher) searchWithFallback(ctx context.Context, primaryEngine SearchEngine, q Query, isImage bool) ([]SearchResult, string, ProxyExecutionMeta, error) {
	ctx = EnsureContext(ctx)

	results, proxyMeta, err := rs.searchWithProtection(ctx, primaryEngine, q, isImage)
	if err == nil {
		return results, primaryEngine.Name(), proxyMeta, nil
	}
	if ctx.Err() != nil {
		return nil, primaryEngine.Name(), proxyMeta, ctx.Err()
	}
	if errors.Is(err, ErrProxyUnavailable) {
		WithRequestEngine(ctx, primaryEngine.Name()).WithError(err).Warn("Proxy policy failed closed")
		return nil, primaryEngine.Name(), proxyMeta, err
	}

	successMessage := "Fallback to %s succeeded with %d results"
	if isImage {
		successMessage = "Image fallback to %s succeeded with %d results"
	}

	WithRequestEngine(ctx, primaryEngine.Name()).
		WithError(err).
		Warn("Primary engine failed, trying fallbacks")
	for _, fallbackEngine := range rs.engines {
		if ctx.Err() != nil {
			return nil, primaryEngine.Name(), proxyMeta, ctx.Err()
		}
		if fallbackEngine.Name() == primaryEngine.Name() || !fallbackEngine.IsInitialized() {
			continue
		}

		results, fallbackMeta, fallbackErr := rs.searchWithProtection(ctx, fallbackEngine, q, isImage)
		if fallbackErr == nil {
			WithRequestEngine(ctx, fallbackEngine.Name()).
				WithField("results_count", len(results)).
				Infof(successMessage, fallbackEngine.Name(), len(results))
			return results, fallbackEngine.Name(), fallbackMeta, nil
		}
		WithRequestEngine(ctx, fallbackEngine.Name()).WithError(fallbackErr).Debug("Fallback engine also failed")
	}

	return nil, primaryEngine.Name(), proxyMeta, ErrAllEnginesFailed
}

func (rs *ResilientSearcher) searchWithProtection(ctx context.Context, engine SearchEngine, q Query, isImage bool) ([]SearchResult, ProxyExecutionMeta, error) {
	ctx = EnsureContext(ctx)

	if ctx.Err() != nil {
		return nil, ProxyExecutionMeta{}, ctx.Err()
	}
	cb := rs.cbManager.Get(engine.Name())
	engineCtx := WithEngine(ctx, engine.Name())
	if !cb.AllowRequest(engineCtx) {
		return nil, ProxyExecutionMeta{}, ErrCircuitOpen
	}

	policy := rs.effectivePolicyForQuery(engine.Name(), q)
	attemptMeta := rs.baseProxyMeta(policy)

	result := RetryableSearch(ctx, rs.retryCfg, engine.Name(), func(callCtx context.Context) ([]SearchResult, error) {
		limiter := engine.GetRateLimiter()
		if limiter != nil {
			if err := limiter.Wait(callCtx); err != nil {
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
			proxyURL = rs.selectProxyForQuery(policy, q, engineCtx)
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
			results, err = engine.SearchImage(callCtx, attemptQuery)
		} else {
			results, err = engine.Search(callCtx, attemptQuery)
		}

		if reportToRegistry {
			rs.reportProxyAttempt(engineCtx, proxyURL, err)
		}

		return results, err
	})

	if result.Err != nil {
		if !errors.Is(result.Err, ErrProxyUnavailable) {
			cb.RecordFailure(engineCtx)
		}
		return nil, attemptMeta, result.Err
	}

	cb.RecordSuccess(engineCtx)
	return result.Results, attemptMeta, nil
}

// SearchAllParallel applies retry/circuit protections per engine for mega search.
func (rs *ResilientSearcher) SearchAllParallel(ctx context.Context, q Query, engines []SearchEngine) []MegaSearchResult {
	ctx = EnsureContext(ctx)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		if ctx.Err() != nil {
			break
		}
		if !engine.IsInitialized() {
			continue
		}
		engineCtx := WithEngine(ctx, engine.Name())
		if !rs.cbManager.Get(engine.Name()).AllowRequest(engineCtx) {
			WithRequest(engineCtx).Debug("Skipping engine in megasearch: circuit open")
			continue
		}

		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			results, _, err := rs.searchWithProtection(ctx, eng, q, false)
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

func (rs *ResilientSearcher) SearchAllImageParallel(ctx context.Context, q Query, engines []SearchEngine) []MegaSearchResult {
	ctx = EnsureContext(ctx)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		if ctx.Err() != nil {
			break
		}
		if !engine.IsInitialized() {
			continue
		}
		engineCtx := WithEngine(ctx, engine.Name())
		if !rs.cbManager.Get(engine.Name()).AllowRequest(engineCtx) {
			WithRequest(engineCtx).Debug("Skipping engine in megaimage: circuit open")
			continue
		}

		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			results, _, err := rs.searchWithProtection(ctx, eng, q, true)
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

func (rs *ResilientSearcher) ResolveMegaProxyMeta(q Query, engines []SearchEngine) ProxyExecutionMeta {
	if len(engines) == 0 {
		return ProxyExecutionMeta{Mode: ProxyModeOff, Used: "direct"}
	}

	allOff := true
	proxiedTags := map[string]struct{}{}
	hasOff := false

	for _, engine := range engines {
		policy := rs.effectivePolicyForQuery(engine.Name(), q)
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

	if q.ProxyOverride == "" {
		if global := strings.TrimSpace(rs.proxyCfg.Proxies.Global); global != "" && !hasOff {
			meta.Used = MaskProxyURL(global)
			return meta
		}
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

func (rs *ResilientSearcher) effectivePolicyForQuery(engineName string, q Query) ProxyPolicy {
	switch q.ProxyOverride {
	case "":
		return rs.effectivePolicyForEngine(engineName)
	case ProxyOverrideDirect:
		return ProxyPolicy{Mode: ProxyModeOff}
	default:
		return ProxyPolicy{Mode: ProxyModeTagPool, Tag: q.ProxyOverride}
	}
}

func (rs *ResilientSearcher) selectProxyForTag(ctx context.Context, tag string) string {
	if rs.proxyRegistry == nil {
		return ""
	}
	return rs.proxyRegistry.NextByTagWithContext(ctx, tag)
}

func (rs *ResilientSearcher) reportProxyAttempt(ctx context.Context, proxyURL string, err error) {
	if rs.proxyRegistry == nil || proxyURL == "" {
		return
	}

	if err != nil {
		// Only degrade proxy health for network-level errors. Captcha pages,
		// parser drift, and engine errors do not indicate a faulty proxy.
		if IsProxyNetworkError(err) {
			rs.proxyRegistry.ReportFailure(ctx, proxyURL)
		}
		return
	}

	rs.proxyRegistry.ReportSuccess(ctx, proxyURL)
}

func (rs *ResilientSearcher) selectProxyForQuery(policy ProxyPolicy, q Query, ctx context.Context) string {
	if policy.Mode != ProxyModeTagPool {
		return ""
	}
	if q.ProxyOverride == "" {
		if global := strings.TrimSpace(rs.proxyCfg.Proxies.Global); global != "" {
			return global
		}
	}
	return rs.selectProxyForTag(ctx, policy.Tag)
}

var ErrAllEnginesFailed = fmt.Errorf("all search engines failed")

package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

// ResilientSearcher wraps engines with retry and circuit breaker protection.
type ResilientSearcher struct {
	engines   []SearchEngine
	cbManager *CircuitBreakerManager
	retryCfg  RetryConfig
}

type ResilientConfig struct {
	Retry          RetryConfig
	CircuitBreaker CircuitBreakerConfig
}

func DefaultResilientConfig() ResilientConfig {
	return ResilientConfig{
		Retry:          DefaultRetryConfig(),
		CircuitBreaker: DefaultCircuitBreakerConfig(),
	}
}

func NewResilientSearcher(engines []SearchEngine, cfg ResilientConfig) *ResilientSearcher {
	return &ResilientSearcher{
		engines:   engines,
		cbManager: NewCircuitBreakerManager(cfg.CircuitBreaker),
		retryCfg:  cfg.Retry,
	}
}

// SearchPrimary keeps dedicated endpoints engine-pure (no fallback).
func (rs *ResilientSearcher) SearchPrimary(primaryEngine SearchEngine, q Query) ([]SearchResult, string, error) {
	results, err := rs.searchWithProtection(primaryEngine, q)
	if err != nil {
		return nil, primaryEngine.Name(), err
	}
	return results, primaryEngine.Name(), nil
}

// SearchWithFallback retries primary and then tries other initialized engines.
func (rs *ResilientSearcher) SearchWithFallback(primaryEngine SearchEngine, q Query) ([]SearchResult, string, error) {
	results, err := rs.searchWithProtection(primaryEngine, q)
	if err == nil {
		return results, primaryEngine.Name(), nil
	}

	logrus.Warnf("[Resilient] Primary engine %s failed: %s. Trying fallback engines...", primaryEngine.Name(), err)
	for _, fallbackEngine := range rs.engines {
		if fallbackEngine.Name() == primaryEngine.Name() || !fallbackEngine.IsInitialized() {
			continue
		}

		results, err := rs.searchWithProtection(fallbackEngine, q)
		if err == nil {
			logrus.Infof("[Resilient] Fallback to %s succeeded with %d results", fallbackEngine.Name(), len(results))
			return results, fallbackEngine.Name(), nil
		}
		logrus.Warnf("[Resilient] Fallback engine %s also failed: %s", fallbackEngine.Name(), err)
	}

	return nil, primaryEngine.Name(), ErrAllEnginesFailed
}

func (rs *ResilientSearcher) SearchImagePrimary(primaryEngine SearchEngine, q Query) ([]SearchResult, string, error) {
	results, err := rs.searchImageWithProtection(primaryEngine, q)
	if err != nil {
		return nil, primaryEngine.Name(), err
	}
	return results, primaryEngine.Name(), nil
}

func (rs *ResilientSearcher) SearchImageWithFallback(primaryEngine SearchEngine, q Query) ([]SearchResult, string, error) {
	results, err := rs.searchImageWithProtection(primaryEngine, q)
	if err == nil {
		return results, primaryEngine.Name(), nil
	}

	logrus.Warnf("[Resilient] Primary engine %s image search failed: %s. Trying fallback engines...", primaryEngine.Name(), err)
	for _, fallbackEngine := range rs.engines {
		if fallbackEngine.Name() == primaryEngine.Name() || !fallbackEngine.IsInitialized() {
			continue
		}

		results, err := rs.searchImageWithProtection(fallbackEngine, q)
		if err == nil {
			logrus.Infof("[Resilient] Image fallback to %s succeeded with %d results", fallbackEngine.Name(), len(results))
			return results, fallbackEngine.Name(), nil
		}
	}

	return nil, primaryEngine.Name(), ErrAllEnginesFailed
}

func (rs *ResilientSearcher) searchWithProtection(engine SearchEngine, q Query) ([]SearchResult, error) {
	cb := rs.cbManager.Get(engine.Name())
	if !cb.AllowRequest() {
		return nil, ErrCircuitOpen
	}

	result := RetryableSearch(rs.retryCfg, engine.Name(), func() ([]SearchResult, error) {
		limiter := engine.GetRateLimiter()
		if limiter != nil {
			if err := limiter.Wait(context.Background()); err != nil {
				return nil, err
			}
		}
		return engine.Search(q)
	})

	if result.Err != nil {
		cb.RecordFailure()
		return nil, result.Err
	}

	cb.RecordSuccess()
	return result.Results, nil
}

func (rs *ResilientSearcher) searchImageWithProtection(engine SearchEngine, q Query) ([]SearchResult, error) {
	cb := rs.cbManager.Get(engine.Name())
	if !cb.AllowRequest() {
		return nil, ErrCircuitOpen
	}

	result := RetryableSearch(rs.retryCfg, engine.Name(), func() ([]SearchResult, error) {
		limiter := engine.GetRateLimiter()
		if limiter != nil {
			if err := limiter.Wait(context.Background()); err != nil {
				return nil, err
			}
		}
		return engine.SearchImage(q)
	})

	if result.Err != nil {
		cb.RecordFailure()
		return nil, result.Err
	}

	cb.RecordSuccess()
	return result.Results, nil
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

			results, err := rs.searchWithProtection(eng, q)
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

			results, err := rs.searchImageWithProtection(eng, q)
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

var ErrAllEnginesFailed = fmt.Errorf("all search engines failed")

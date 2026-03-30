package core

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type SearchEngine interface {
	Search(Query) ([]SearchResult, error)
	SearchImage(Query) ([]SearchResult, error)
	IsInitialized() bool
	Name() string
	GetRateLimiter() *rate.Limiter
}

type Server struct {
	app           *fiber.App
	addr          string
	searchEngines []SearchEngine
	cache         *ResponseCache
	resilient     *ResilientSearcher
	startTime     time.Time
	opts          ServerOptions
}

type ServerOptions struct {
	CacheTTL              time.Duration
	CacheMaxSize          int
	EnableCORS            bool
	CORS                  CORSConfig
	AllowEndpointFallback bool
	Resilience            ResilientConfig
}

func DefaultServerOptions() ServerOptions {
	return ServerOptions{
		CacheTTL:              5 * time.Minute,
		CacheMaxSize:          1000,
		EnableCORS:            true,
		CORS:                  DefaultCORSConfig(),
		AllowEndpointFallback: false,
		Resilience:            DefaultResilientConfig(),
	}
}

func NewServer(host string, port int, searchEngines ...SearchEngine) *Server {
	return NewServerWithOptions(host, port, DefaultServerOptions(), searchEngines...)
}

func NewServerWithOptions(host string, port int, opts ServerOptions, searchEngines ...SearchEngine) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	app := fiber.New(fiber.Config{
		ErrorHandler: JSONErrorMiddleware(),
	})

	serv := Server{
		app:           app,
		addr:          addr,
		searchEngines: searchEngines,
		resilient:     NewResilientSearcher(searchEngines, opts.Resilience),
		startTime:     time.Now(),
		opts:          opts,
	}
	logrus.Info("Resilient search enabled: retry + circuit breaker")
	if opts.AllowEndpointFallback {
		logrus.Warn("Dedicated endpoint fallback is enabled")
	}
	if opts.CacheTTL > 0 && opts.CacheMaxSize > 0 {
		serv.cache = NewResponseCache(opts.CacheTTL, opts.CacheMaxSize)
		logrus.Infof("Response cache enabled: TTL=%s, MaxSize=%d", opts.CacheTTL, opts.CacheMaxSize)
	}

	if opts.EnableCORS {
		app.Use(CORSMiddleware(opts.CORS))
	}
	app.Use(RequestLoggerMiddleware())

	app.Get("/health", serv.handleHealthCheck)
	app.Get("/cache/stats", serv.handleCacheStats)
	app.Get("/resilience/stats", serv.handleResilienceStats)

	for _, engine := range searchEngines {
		locEngine := engine

		endpointName := strings.ToLower(locEngine.Name())
		if endpointName == "duckduckgo" {
			endpointName = "duck"
		}

		serv.app.Get(fmt.Sprintf("/%s/search", endpointName), func(c *fiber.Ctx) error {
			return serv.handleDedicatedEndpoint(c, locEngine, false)
		})

		serv.app.Get(fmt.Sprintf("/%s/image", endpointName), func(c *fiber.Ctx) error {
			return serv.handleDedicatedEndpoint(c, locEngine, true)
		})
	}

	serv.app.Get("/mega/search", serv.handleMegaSearch)
	serv.app.Get("/mega/image", serv.handleMegaImage)
	serv.app.Get("/mega/engines", serv.handleListEngines)

	return &serv
}

func (s *Server) handleDedicatedEndpoint(c *fiber.Ctx, engine SearchEngine, isImage bool) error {
	q := Query{}
	if err := q.InitFromContext(c); err != nil {
		logrus.Errorf("Error while setting %s query: %s", engine.Name(), err)
		return err
	}

	action := "search"
	if isImage {
		action = "image"
	}
	logrus.Infof("Starting SERP %s request using %s engine for query: %s", action, engine.Name(), q.Text)

	if hit, err := s.tryServeCacheHit(
		c,
		cacheHitCandidate{
			key:        BuildCacheKey(engine.Name(), action, q),
			logMessage: fmt.Sprintf("Cache hit for %s %s: %s", engine.Name(), action, q.Text),
		},
	); hit || err != nil {
		return err
	}

	var (
		res        []SearchResult
		usedEngine string
		searchErr  error
	)

	if isImage {
		if s.opts.AllowEndpointFallback {
			res, usedEngine, searchErr = s.resilient.SearchImageWithFallback(engine, q)
		} else {
			res, usedEngine, searchErr = s.resilient.SearchImagePrimary(engine, q)
		}
	} else {
		if s.opts.AllowEndpointFallback {
			res, usedEngine, searchErr = s.resilient.SearchWithFallback(engine, q)
		} else {
			res, usedEngine, searchErr = s.resilient.SearchPrimary(engine, q)
		}
	}

	if searchErr != nil {
		errToReturn := searchErr
		switch searchErr {
		case ErrCaptcha:
			errToReturn = fmt.Errorf("captcha found, please stop sending requests for a while: %w", searchErr)
		case ErrSearchTimeout:
			errToReturn = fmt.Errorf("%s", searchErr)
		}
		logrus.Errorf("Error during resilient %s %s: %s", engine.Name(), action, searchErr)
		return fiber.NewError(fiber.StatusServiceUnavailable, errToReturn.Error())
	}

	cacheStatus := ""
	// Avoid caching fallback-served responses so the requested engine can recover
	// without the endpoint continuing to serve another engine until TTL expiry.
	if s.cache != nil {
		cacheStatus = "BYPASS"
		switch {
		case usedEngine != engine.Name():
			s.cache.RecordBypass()
		case len(res) == 0:
			s.cache.RecordBypass()
		default:
			cacheKey := BuildCacheKey(engine.Name(), action, q)
			if s.cacheJSON(cacheKey, res) {
				cacheStatus = "MISS"
			}
		}
		c.Set("X-Cache", cacheStatus)
	}

	if usedEngine != "" && usedEngine != engine.Name() {
		c.Set("X-Fallback-Engine", usedEngine)
	}

	logrus.Infof("Successfully completed SERP %s using %s, returned %d results", action, usedEngine, len(res))
	return c.JSON(res)
}

type HealthStatus struct {
	Status  string                 `json:"status"`
	Uptime  string                 `json:"uptime"`
	Engines []EngineHealth         `json:"engines"`
	System  map[string]interface{} `json:"system"`
}

type EngineHealth struct {
	Name        string `json:"name"`
	Initialized bool   `json:"initialized"`
	Status      string `json:"status"`
}

// handleHealthCheck returns current service and engine status.
// Degraded state stays HTTP 200 to avoid unnecessary restarts in orchestrators.
func (s *Server) handleHealthCheck(c *fiber.Ctx) error {
	engines := make([]EngineHealth, 0, len(s.searchEngines))
	availableEngines := 0

	for _, engine := range s.searchEngines {
		status := "ready"
		isAvailable := true
		if !engine.IsInitialized() {
			status = "not_initialized"
			isAvailable = false
		}

		for _, cbStat := range s.resilient.GetCircuitBreakerStats() {
			engineName, _ := cbStat["engine"].(string)
			if engineName != engine.Name() {
				continue
			}
			circuitState, _ := cbStat["state"].(string)
			if circuitState == "open" {
				status = "circuit_open"
				isAvailable = false
			}
			break
		}

		if isAvailable {
			availableEngines++
		}

		engines = append(engines, EngineHealth{
			Name:        engine.Name(),
			Initialized: engine.IsInitialized(),
			Status:      status,
		})
	}

	overallStatus := "healthy"
	totalEngines := len(s.searchEngines)
	switch {
	case totalEngines == 0 || availableEngines == 0:
		overallStatus = "unhealthy"
	case availableEngines < totalEngines:
		overallStatus = "degraded"
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	health := HealthStatus{
		Status:  overallStatus,
		Uptime:  time.Since(s.startTime).Round(time.Second).String(),
		Engines: engines,
		System: map[string]interface{}{
			"goroutines": runtime.NumGoroutine(),
			"memory_mb":  memStats.Alloc / 1024 / 1024,
			"go_version": runtime.Version(),
		},
	}

	if overallStatus == "unhealthy" {
		c.Status(fiber.StatusServiceUnavailable)
	}
	return c.JSON(health)
}

func (s *Server) handleResilienceStats(c *fiber.Ctx) error {
	return c.JSON(map[string]interface{}{
		"circuit_breakers": s.resilient.GetCircuitBreakerStats(),
		"proxy":            s.resilient.GetProxyStats(),
	})
}

func (s *Server) handleCacheStats(c *fiber.Ctx) error {
	if s.cache == nil {
		return c.JSON(map[string]interface{}{"status": false})
	}
	return c.JSON(s.cache.Stats())
}

type MegaSearchResult struct {
	SearchResult
	Engine string `json:"engine"`
}

func (s *Server) handleMegaSearch(c *fiber.Ctx) error {
	return s.handleMegaEndpoint(c, "search", s.resilient.SearchAllParallel)
}

func (s *Server) handleMegaImage(c *fiber.Ctx) error {
	return s.handleMegaEndpoint(c, "image", s.resilient.SearchAllImageParallel)
}

func (s *Server) handleMegaEndpoint(c *fiber.Ctx, action string, run func(Query, []SearchEngine) []MegaSearchResult) error {
	q := Query{}
	if err := q.InitFromContext(c); err != nil {
		logrus.Errorf("Error while setting mega %s query: %s", action, err)
		return err
	}

	enginesToUse := s.resolveEngines(c.Query("engines", ""))
	if len(enginesToUse) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No valid search engines specified")
	}

	engineNames := make([]string, len(enginesToUse))
	for i, engine := range enginesToUse {
		engineNames[i] = engine.Name()
	}
	engineNamesJoined := strings.Join(engineNames, ",")
	logrus.Infof("Starting SERP mega %s request using engines: %s for query: %s", action, engineNamesJoined, q.Text)

	cacheHitCandidates := []cacheHitCandidate{
		{
			key:        s.buildMegaCacheKey(action, enginesToUse, q),
			logMessage: fmt.Sprintf("Cache hit for mega %s: engines=%s query=%s", action, engineNamesJoined, q.Text),
		},
	}
	cacheableEngines := s.megaCacheableEngines(enginesToUse)
	if len(cacheableEngines) > 0 && len(cacheableEngines) < len(enginesToUse) {
		cacheHitCandidates = append(cacheHitCandidates, cacheHitCandidate{
			key:        s.buildMegaCacheKey(action, cacheableEngines, q),
			logMessage: fmt.Sprintf("Cache hit for mega %s partial set: engines=%s query=%s", action, engineNamesJoined, q.Text),
		})
	}
	if hit, err := s.tryServeCacheHit(c, cacheHitCandidates...); hit || err != nil {
		return err
	}

	results := run(q, enginesToUse)
	dedupedResults := s.deduplicateMegaResults(results)

	if s.cache != nil {
		c.Set("X-Cache", s.cacheMegaResults(action, enginesToUse, q, dedupedResults))
	}

	logrus.Infof("Successfully completed SERP mega %s using %d engines, returned %d deduplicated results", action, len(enginesToUse), len(dedupedResults))
	return c.JSON(dedupedResults)
}

func (s *Server) handleListEngines(c *fiber.Ctx) error {
	var engines []map[string]interface{}

	for _, engine := range s.searchEngines {
		engineInfo := map[string]interface{}{
			"name":        engine.Name(),
			"initialized": engine.IsInitialized(),
		}

		for _, cbStat := range s.resilient.GetCircuitBreakerStats() {
			engineName, _ := cbStat["engine"].(string)
			if engineName == engine.Name() {
				engineInfo["circuit_state"] = cbStat["state"]
				break
			}
		}

		engines = append(engines, engineInfo)
	}

	return c.JSON(map[string]interface{}{
		"engines": engines,
		"total":   len(engines),
	})
}

func (s *Server) resolveEngines(enginesParam string) []SearchEngine {
	if enginesParam == "" {
		return s.searchEngines
	}

	var enginesToUse []SearchEngine
	seen := make(map[string]bool)
	engineNames := strings.Split(enginesParam, ",")
	for _, engineName := range engineNames {
		engineName = strings.TrimSpace(strings.ToLower(engineName))
		if engineName == "" || seen[engineName] {
			continue
		}
		for _, engine := range s.searchEngines {
			if strings.ToLower(engine.Name()) == engineName {
				enginesToUse = append(enginesToUse, engine)
				seen[engineName] = true
				break
			}
		}
	}
	return enginesToUse
}

func (s *Server) deduplicateMegaResults(results []MegaSearchResult) []MegaSearchResult {
	urlMap := make(map[string]MegaSearchResult)

	for _, result := range results {
		if result.URL == "" {
			continue
		}
		if _, exists := urlMap[result.URL]; !exists {
			urlMap[result.URL] = result
		}
	}

	var deduped []MegaSearchResult
	for _, result := range urlMap {
		deduped = append(deduped, result)
	}

	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].Rank < deduped[j].Rank
	})
	return deduped
}

type cacheHitCandidate struct {
	key        string
	logMessage string
}

func (s *Server) tryServeCacheHit(c *fiber.Ctx, candidates ...cacheHitCandidate) (bool, error) {
	if s.cache == nil {
		return false, nil
	}
	for _, candidate := range candidates {
		cached, ok := s.cache.Get(candidate.key)
		if !ok {
			continue
		}
		c.Set("Content-Type", "application/json")
		c.Set("X-Cache", "HIT")
		logrus.Info(candidate.logMessage)
		return true, c.Send(cached)
	}
	return false, nil
}

func (s *Server) cacheJSON(cacheKey string, payload interface{}) bool {
	if s.cache == nil {
		return false
	}
	data, err := json.Marshal(payload)
	if err != nil {
		s.cache.RecordBypass()
		return false
	}
	s.cache.Set(cacheKey, data)
	return true
}

func (s *Server) cacheMegaResults(action string, enginesToUse []SearchEngine, q Query, dedupedResults []MegaSearchResult) string {
	cacheStatus := "BYPASS"
	if s.cache == nil {
		return cacheStatus
	}
	if len(dedupedResults) == 0 {
		s.cache.RecordBypass()
		return cacheStatus
	}

	cacheEngines := s.megaCacheableEngines(enginesToUse)
	if len(cacheEngines) == 0 {
		s.cache.RecordBypass()
		return cacheStatus
	}

	if s.cacheJSON(s.buildMegaCacheKey(action, cacheEngines, q), dedupedResults) {
		return "MISS"
	}
	return cacheStatus
}

func (s *Server) buildMegaCacheKey(action string, engines []SearchEngine, q Query) string {
	names := make([]string, 0, len(engines))
	for _, eng := range engines {
		names = append(names, strings.ToLower(strings.TrimSpace(eng.Name())))
	}
	sort.Strings(names)

	// Deduplicate engine names in key to keep cache stable when order differs
	// or repeated names are passed in the engines query parameter.
	uniq := names[:0]
	last := ""
	for _, name := range names {
		if name == last {
			continue
		}
		uniq = append(uniq, name)
		last = name
	}

	return BuildCacheKey("mega:"+strings.Join(uniq, ","), action, q)
}

func (s *Server) megaCacheableEngines(engines []SearchEngine) []SearchEngine {
	open := make(map[string]bool)
	for _, stat := range s.resilient.GetCircuitBreakerStats() {
		name, _ := stat["engine"].(string)
		state, _ := stat["state"].(string)
		if strings.EqualFold(state, "open") {
			open[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}

	cacheable := make([]SearchEngine, 0, len(engines))
	for _, eng := range engines {
		if open[strings.ToLower(strings.TrimSpace(eng.Name()))] {
			continue
		}
		cacheable = append(cacheable, eng)
	}
	return cacheable
}

func (s *Server) Listen() error {
	return s.app.Listen(s.addr)
}

func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

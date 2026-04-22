package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/karust/openserp/core/fpcheck"
	"github.com/karust/openserp/core/fpcheck/detectors"
	apidocs "github.com/karust/openserp/docs"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// DefaultFingerprintArtifactDir is the artifact directory used when none is
// configured. It is relative to the server's working directory at start time.
var DefaultFingerprintArtifactDir = filepath.Join("core", "testdata")

// SearchEngine defines the contract required by the HTTP server and resilient
// search pipeline.
type SearchEngine interface {
	// Search runs a web search request and returns normalized results.
	// Implementations should return sentinel errors such as ErrCaptcha and
	// ErrSearchTimeout for policy-aware handling.
	Search(context.Context, Query) ([]SearchResult, error)
	// SearchImage runs an image search request and returns normalized results.
	SearchImage(context.Context, Query) ([]SearchResult, error)
	// IsInitialized reports whether the engine is ready to serve requests.
	IsInitialized() bool
	// Name returns a stable engine identifier used in routes and telemetry.
	Name() string
	// GetRateLimiter returns an engine-specific limiter used by resilient search.
	GetRateLimiter() *rate.Limiter
}

// Server exposes OpenSERP HTTP endpoints backed by one or more search engines.
type Server struct {
	app           *fiber.App
	addr          string
	searchEngines []SearchEngine
	cache         *ResponseCache
	resilient     *ResilientSearcher
	startTime     time.Time
	opts          ServerOptions
	draining      atomic.Bool
}

// ServerOptions configures HTTP server middleware and resilience behavior.
type ServerOptions struct {
	// CacheTTL controls response cache entry lifetime. Zero disables caching.
	CacheTTL time.Duration
	// CacheMaxSize is the maximum number of cached entries.
	CacheMaxSize int
	// EnableCORS enables cross-origin headers with the CORS config below.
	EnableCORS bool
	// CORS contains allowed origins, methods, and headers when CORS is enabled.
	CORS CORSConfig
	// AllowEndpointFallback allows dedicated engine routes to fall back to other
	// healthy engines when the primary engine fails.
	AllowEndpointFallback bool
	// EnableDebugEndpoints enables debug-only routes such as fingerprint checks.
	EnableDebugEndpoints bool
	// FingerprintArtifactDir is where debug fingerprint screenshots are written.
	FingerprintArtifactDir string
	// FingerprintBrowserOpts are the defaults for debug fingerprint runs.
	FingerprintBrowserOpts BrowserOpts
	// Resilience defines retry/circuit-breaker/proxy strategy settings.
	Resilience ResilientConfig
}

// DefaultServerOptions returns production-oriented defaults for cache, CORS,
// and resilient search policies.
func DefaultServerOptions() ServerOptions {
	return ServerOptions{
		CacheTTL:               5 * time.Minute,
		CacheMaxSize:           1000,
		EnableCORS:             true,
		CORS:                   DefaultCORSConfig(),
		AllowEndpointFallback:  false,
		EnableDebugEndpoints:   false,
		FingerprintArtifactDir: DefaultFingerprintArtifactDir,
		FingerprintBrowserOpts: BrowserOpts{
			IsHeadless: true,
			Timeout:    30 * time.Second,
		},
		Resilience: DefaultResilientConfig(),
	}
}

// NewServer creates a Server with DefaultServerOptions and registers all
// routes for the provided engines.
func NewServer(host string, port int, searchEngines ...SearchEngine) *Server {
	return NewServerWithOptions(host, port, DefaultServerOptions(), searchEngines...)
}

// NewServerWithOptions builds a Server, installs middleware, and registers API
// routes. The returned server is ready to Listen; call Shutdown for graceful
// stop.
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
	serv.draining.Store(false)
	logrus.Info("Resilient search enabled: retry + circuit breaker")
	if opts.AllowEndpointFallback {
		logrus.Warn("Dedicated endpoint fallback is enabled")
	}
	if opts.CacheTTL > 0 && opts.CacheMaxSize > 0 {
		serv.cache = NewResponseCache(opts.CacheTTL, opts.CacheMaxSize)
		logrus.WithFields(logrus.Fields{
			"cache_ttl":      opts.CacheTTL.String(),
			"cache_max_size": opts.CacheMaxSize,
		}).Info("Response cache enabled")
	}

	app.Use(RequestContextMiddleware())
	if opts.EnableCORS {
		app.Use(CORSMiddleware(opts.CORS))
	}
	app.Use(RequestLoggerMiddleware())

	app.Get("/openapi.yaml", serv.handleOpenAPISpec)
	app.Get("/docs", serv.handleSwaggerUI)
	app.Get("/docs/", serv.handleSwaggerUI)
	app.Get("/health", serv.handleHealthCheck)
	app.Get("/ready", serv.handleReadinessCheck)
	app.Get("/stats", serv.handleStats)
	app.Get("/stats/cache", serv.handleCacheStats)
	app.Get("/stats/proxy", serv.handleProxyStats)
	app.Get("/stats/cb", serv.handleCircuitBreakerStats)
	if opts.EnableDebugEndpoints {
		app.Get("/debug/fingerprint-check", serv.handleFingerprintCheck)
	}

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
	requestCtx := WithEngine(c.UserContext(), engine.Name())
	c.SetUserContext(requestCtx)

	q := Query{}
	if err := q.InitFromContext(c); err != nil {
		WithRequest(c.UserContext()).WithError(err).Error("Invalid query parameters")
		return err
	}

	requestCtx = WithQueryHash(c.UserContext(), QueryHashFromQuery(q))
	c.SetUserContext(requestCtx)

	action := "search"
	if isImage {
		action = "image"
	}
	WithRequest(requestCtx).
		WithField("action", action).
		Debugf("Starting %s request for query: %s", action, q.Text)

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
		proxyMeta  ProxyExecutionMeta
		searchErr  error
	)

	if isImage {
		if s.opts.AllowEndpointFallback {
			res, usedEngine, proxyMeta, searchErr = s.resilient.SearchImageWithFallback(requestCtx, engine, q)
		} else {
			res, usedEngine, proxyMeta, searchErr = s.resilient.SearchImagePrimary(requestCtx, engine, q)
		}
	} else {
		if s.opts.AllowEndpointFallback {
			res, usedEngine, proxyMeta, searchErr = s.resilient.SearchWithFallback(requestCtx, engine, q)
		} else {
			res, usedEngine, proxyMeta, searchErr = s.resilient.SearchPrimary(requestCtx, engine, q)
		}
	}
	s.applyProxyHeaders(c, proxyMeta)

	if searchErr != nil {
		errToReturn := searchErr
		switch {
		case errors.Is(searchErr, ErrCaptcha):
			errToReturn = fmt.Errorf("captcha found, please stop sending requests for a while: %w", searchErr)
		case errors.Is(searchErr, ErrSearchTimeout):
			errToReturn = fmt.Errorf("%s", searchErr)
		case errors.Is(searchErr, ErrParser):
			errToReturn = fmt.Errorf("%w", ErrParser)
		case errors.Is(searchErr, ErrEngineInternal):
			errToReturn = fmt.Errorf("%w", ErrEngineInternal)
		case errors.Is(searchErr, ErrProxyUnavailable):
			errToReturn = fmt.Errorf("%s", searchErr)
		}
		WithRequest(requestCtx).
			WithFields(logrus.Fields{"action": action}).
			WithError(searchErr).
			Error("Search failed")
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

	completionCtx := requestCtx
	if usedEngine != "" {
		completionCtx = WithEngine(completionCtx, usedEngine)
	}
	WithRequest(completionCtx).
		WithFields(logrus.Fields{"action": action, "results_count": len(res)}).
		Info("Search completed")
	return c.JSON(res)
}

// HealthStatus is returned by /health and summarizes service state.
type HealthStatus struct {
	Status  string                 `json:"status"`
	Uptime  string                 `json:"uptime"`
	Engines []EngineHealth         `json:"engines"`
	System  map[string]interface{} `json:"system"`
}

// ReadinessStatus is returned by /ready to indicate if this instance can
// receive new traffic.
type ReadinessStatus struct {
	Status string `json:"status"`
}

// EngineHealth describes availability of one configured engine.
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

func (s *Server) handleReadinessCheck(c *fiber.Ctx) error {
	status := ReadinessStatus{Status: "ready"}
	if s.draining.Load() {
		status.Status = "draining"
		return c.Status(fiber.StatusServiceUnavailable).JSON(status)
	}
	return c.JSON(status)
}

func (s *Server) handleStats(c *fiber.Ctx) error {
	return c.JSON(map[string]interface{}{
		"cache":            s.cacheStatsPayload(),
		"proxy":            s.resilient.GetProxyStats(),
		"circuit_breakers": s.resilient.GetCircuitBreakerStats(),
		"captcha":          CaptchaSolverMetrics(),
	})
}

func (s *Server) handleCacheStats(c *fiber.Ctx) error {
	return c.JSON(s.cacheStatsPayload())
}

func (s *Server) handleProxyStats(c *fiber.Ctx) error {
	return c.JSON(s.resilient.GetProxyStats())
}

func (s *Server) handleCircuitBreakerStats(c *fiber.Ctx) error {
	return c.JSON(map[string]interface{}{
		"circuit_breakers": s.resilient.GetCircuitBreakerStats(),
	})
}

func (s *Server) handleFingerprintCheck(c *fiber.Ctx) error {
	req, err := s.parseFingerprintCheckRequest(c)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(c.UserContext(), time.Duration(req.timeoutMs)*time.Millisecond)
	defer cancel()

	browser, err := NewBrowser(req.browserOpts)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, fmt.Sprintf("failed to create debug browser: %v", err))
	}
	defer func() {
		if closeErr := browser.Close(); closeErr != nil {
			WithRequest(c.UserContext()).WithError(closeErr).Warn("failed to close debug fingerprint browser")
		}
	}()

	artifactDir := defaultFingerprintArtifactDir(s.opts.FingerprintArtifactDir)

	reports := make([]fpcheck.Report, 0, len(req.detectors))
	for idx, detector := range req.detectors {
		runOpts := fpcheck.RunOptions{
			UseStealth:  req.browserOpts.UseStealth,
			ArtifactDir: artifactDir,
		}
		if req.waitMs > 0 && idx == len(req.detectors)-1 {
			runOpts.WaitBeforeClose = time.Duration(req.waitMs) * time.Millisecond
		}

		report, runErr := fpcheck.RunWithOptions(runCtx, browser, detector, runOpts)
		if runErr != nil {
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				return fiber.NewError(fiber.StatusGatewayTimeout, fmt.Sprintf("fingerprint check timed out after %dms", req.timeoutMs))
			}
			return fiber.NewError(
				fiber.StatusServiceUnavailable,
				fmt.Sprintf("detector %s failed: %v", detector.Name(), runErr),
			)
		}
		reports = append(reports, report)
	}

	return c.JSON(reports)
}

type fingerprintCheckRequest struct {
	detectors   []fpcheck.Detector
	timeoutMs   int
	waitMs      int
	browserOpts BrowserOpts
}

func (s *Server) parseFingerprintCheckRequest(c *fiber.Ctx) (fingerprintCheckRequest, error) {
	detectorName := strings.TrimSpace(c.Query("detector", "all"))
	customURL := strings.TrimSpace(c.Query("url", ""))
	selectedDetectors, err := detectors.Select(detectorName, customURL)
	if err != nil {
		return fingerprintCheckRequest{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	useStealth, err := parseOptionalBoolQuery(c.Query("stealth", ""), s.opts.FingerprintBrowserOpts.UseStealth)
	if err != nil {
		return fingerprintCheckRequest{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid stealth query value: %v", err))
	}

	headless, err := parseOptionalBoolQuery(c.Query("headless", ""), true)
	if err != nil {
		return fingerprintCheckRequest{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid headless query value: %v", err))
	}
	if !headless && strings.TrimSpace(os.Getenv("DISPLAY")) == "" {
		WithRequest(c.UserContext()).Warn("headless=false ignored because DISPLAY is not set; forcing headless mode")
		headless = true
	}

	timeoutMs, err := parsePositiveIntQuery(c.Query("timeout_ms", ""), 150000)
	if err != nil {
		return fingerprintCheckRequest{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid timeout_ms query value: %v", err))
	}
	waitMs, err := parseNonNegativeIntQuery(c.Query("wait_ms", ""), 0)
	if err != nil {
		return fingerprintCheckRequest{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid wait_ms query value: %v", err))
	}

	browserOpts := s.opts.FingerprintBrowserOpts
	browserOpts.IsHeadless = headless
	browserOpts.UseStealth = useStealth
	browserOpts.Timeout = time.Duration(timeoutMs) * time.Millisecond
	browserOpts.LeavePageOpen = false
	browserOpts.UserAgent = strings.TrimSpace(c.Query("user_agent", browserOpts.UserAgent))
	browserOpts.ProxyURL = strings.TrimSpace(c.Query("proxy", browserOpts.ProxyURL))
	browserOpts.LanguageCode = strings.TrimSpace(c.Query("language", browserOpts.LanguageCode))

	insecureDefault := browserOpts.Insecure
	if detectors.IsCustom(detectorName) {
		insecureDefault = true
	}
	insecure, err := parseOptionalBoolQuery(c.Query("insecure", ""), insecureDefault)
	if err != nil {
		return fingerprintCheckRequest{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid insecure query value: %v", err))
	}
	browserOpts.Insecure = insecure

	return fingerprintCheckRequest{
		detectors:   selectedDetectors,
		timeoutMs:   timeoutMs,
		waitMs:      waitMs,
		browserOpts: browserOpts,
	}, nil
}

func defaultFingerprintArtifactDir(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultFingerprintArtifactDir
	}
	return trimmed
}

func parseOptionalBoolQuery(raw string, defaultValue bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, err
	}
	return value, nil
}

func parsePositiveIntQuery(raw string, defaultValue int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	return value, nil
}

func parseNonNegativeIntQuery(raw string, defaultValue int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("must be >= 0")
	}
	return value, nil
}

// MegaSearchResult extends SearchResult with the engine source name.
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

func (s *Server) handleMegaEndpoint(c *fiber.Ctx, action string, run func(context.Context, Query, []SearchEngine) []MegaSearchResult) error {
	requestCtx := WithEngine(c.UserContext(), "mega")
	c.SetUserContext(requestCtx)

	q := Query{}
	if err := q.InitFromContext(c); err != nil {
		WithRequest(c.UserContext()).WithError(err).Error("Invalid query parameters")
		return err
	}
	requestCtx = WithQueryHash(c.UserContext(), QueryHashFromQuery(q))
	c.SetUserContext(requestCtx)

	enginesToUse := s.resolveEngines(c.Query("engines", ""))
	if len(enginesToUse) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No valid search engines specified")
	}

	engineNames := make([]string, len(enginesToUse))
	for i, engine := range enginesToUse {
		engineNames[i] = engine.Name()
	}
	engineNamesJoined := strings.Join(engineNames, ",")
	s.applyProxyHeaders(c, s.resilient.ResolveMegaProxyMeta(q, enginesToUse))
	WithRequest(requestCtx).WithFields(logrus.Fields{
		"action":  action,
		"engines": engineNamesJoined,
	}).Debugf("Starting mega %s request for query: %s", action, q.Text)

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

	results := run(requestCtx, q, enginesToUse)
	dedupedResults := s.deduplicateMegaResults(results)

	if s.cache != nil {
		c.Set("X-Cache", s.cacheMegaResults(action, enginesToUse, q, dedupedResults))
	}

	WithRequest(requestCtx).WithFields(logrus.Fields{
		"action":        action,
		"engines_count": len(enginesToUse),
		"results_count": len(dedupedResults),
	}).Info("Mega search completed")
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
		WithRequest(c.UserContext()).Debug(candidate.logMessage)
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

func (s *Server) cacheStatsPayload() interface{} {
	if s.cache == nil {
		return map[string]interface{}{"status": false}
	}
	return s.cache.Stats()
}

func (s *Server) applyProxyHeaders(c *fiber.Ctx, meta ProxyExecutionMeta) {
	mode := meta.Mode
	if mode == "" {
		mode = ProxyModeOff
	}

	tag := meta.Tag
	used := meta.Used

	if mode == ProxyModeOff {
		tag = ""
		used = "direct"
	}

	c.Set("X-Proxy-Mode", mode)
	c.Set("X-Proxy-Tag", tag)
	c.Set("X-Proxy-Used", used)
}

func (s *Server) handleOpenAPISpec(c *fiber.Ctx) error {
	c.Set("Content-Type", "application/yaml; charset=utf-8")
	return c.Send(apidocs.OpenAPIYAML)
}

func (s *Server) handleSwaggerUI(c *fiber.Ctx) error {
	const specPath = "/openapi.yaml"
	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>OpenSERP API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; background: #f6f8fb; }
    #swagger-ui { max-width: 1200px; margin: 0 auto; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: %q,
        dom_id: "#swagger-ui",
        deepLinking: true,
        displayRequestDuration: true,
        presets: [SwaggerUIBundle.presets.apis],
      });
    };
  </script>
</body>
</html>`, html.EscapeString(specPath))
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(page)
}

// SetDraining controls readiness state exposed by /ready.
func (s *Server) SetDraining(draining bool) {
	s.draining.Store(draining)
}

const defaultShutdownTimeout = 30 * time.Second

// Listen starts the Fiber HTTP server on the configured address.
func (s *Server) Listen() error {
	s.SetDraining(false)
	return s.app.Listen(s.addr)
}

// Shutdown gracefully stops the Fiber HTTP server.
func (s *Server) Shutdown() error {
	return s.ShutdownWithTimeout(defaultShutdownTimeout)
}

// ShutdownWithTimeout drains the server before force-closing active
// connections when timeout is exceeded.
func (s *Server) ShutdownWithTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	s.SetDraining(true)
	return s.app.ShutdownWithTimeout(timeout)
}

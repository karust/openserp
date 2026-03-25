package core

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	resilient     *ResilientSearcher
	startTime     time.Time
	opts          ServerOptions
}

type ServerOptions struct {
	EnableCORS            bool
	CORS                  CORSConfig
	AllowEndpointFallback bool
	Resilience            ResilientConfig
}

func DefaultServerOptions() ServerOptions {
	return ServerOptions{
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

	if opts.EnableCORS {
		app.Use(CORSMiddleware(opts.CORS))
	}
	app.Use(RequestLoggerMiddleware())

	app.Get("/health", serv.handleHealthCheck)
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
	})
}

type MegaSearchResult struct {
	SearchResult
	Engine string `json:"engine"`
}

func (s *Server) handleMegaSearch(c *fiber.Ctx) error {
	q := Query{}
	if err := q.InitFromContext(c); err != nil {
		logrus.Errorf("Error while setting megasearch query: %s", err)
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
	logrus.Infof("Starting SERP megasearch request using engines: %s for query: %s", strings.Join(engineNames, ", "), q.Text)

	results := s.resilient.SearchAllParallel(q, enginesToUse)
	dedupedResults := s.deduplicateMegaResults(results)

	logrus.Infof("Successfully completed SERP megasearch using %d engines, returned %d deduplicated results", len(enginesToUse), len(dedupedResults))
	return c.JSON(dedupedResults)
}

func (s *Server) handleMegaImage(c *fiber.Ctx) error {
	q := Query{}
	if err := q.InitFromContext(c); err != nil {
		logrus.Errorf("Error while setting megasearch image query: %s", err)
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
	logrus.Infof("Starting SERP megasearch image request using engines: %s for query: %s", strings.Join(engineNames, ", "), q.Text)

	results := s.resilient.SearchAllImageParallel(q, enginesToUse)
	dedupedResults := s.deduplicateMegaResults(results)

	logrus.Infof("Successfully completed SERP megasearch image using %d engines, returned %d deduplicated results", len(enginesToUse), len(dedupedResults))
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
	engineNames := strings.Split(enginesParam, ",")
	for _, engineName := range engineNames {
		engineName = strings.TrimSpace(strings.ToLower(engineName))
		for _, engine := range s.searchEngines {
			if strings.ToLower(engine.Name()) == engineName {
				enginesToUse = append(enginesToUse, engine)
				break
			}
		}
	}
	return enginesToUse
}

func (s *Server) searchSelectedEngines(q Query, engines []SearchEngine) []MegaSearchResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			limiter := eng.GetRateLimiter()
			if limiter != nil {
				err := limiter.Wait(context.Background())
				if err != nil {
					logrus.Errorf("Ratelimiter error during %s megasearch: %s", eng.Name(), err)
				}
			}

			results, err := eng.Search(q)
			if err != nil {
				logrus.Errorf("Error during %s megasearch: %s", eng.Name(), err)
				return
			}

			mu.Lock()
			for _, result := range results {
				megaResult := MegaSearchResult{
					SearchResult: result,
					Engine:       eng.Name(),
				}
				allResults = append(allResults, megaResult)
			}
			mu.Unlock()
		}(engine)
	}

	wg.Wait()
	return allResults
}

func (s *Server) searchSelectedEnginesImage(q Query, engines []SearchEngine) []MegaSearchResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			limiter := eng.GetRateLimiter()
			if limiter != nil {
				err := limiter.Wait(context.Background())
				if err != nil {
					logrus.Errorf("Ratelimiter error during %s megasearch image: %s", eng.Name(), err)
				}
			}

			results, err := eng.SearchImage(q)
			if err != nil {
				logrus.Errorf("Error during %s megasearch image: %s", eng.Name(), err)
				return
			}

			mu.Lock()
			for _, result := range results {
				megaResult := MegaSearchResult{
					SearchResult: result,
					Engine:       eng.Name(),
				}
				allResults = append(allResults, megaResult)
			}
			mu.Unlock()
		}(engine)
	}

	wg.Wait()
	return allResults
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

func (s *Server) Listen() error {
	return s.app.Listen(s.addr)
}

func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

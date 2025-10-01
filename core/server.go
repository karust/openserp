package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

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
}

func NewServer(host string, port int, searchEngines ...SearchEngine) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	serv := Server{
		app:           fiber.New(),
		addr:          addr,
		searchEngines: searchEngines,
	}

	for _, engine := range searchEngines {
		locEngine := engine
		limiter := engine.GetRateLimiter()

		// Custom endpoint mapping for DuckDuckGo
		endpointName := strings.ToLower(locEngine.Name())
		if endpointName == "duckduckgo" {
			endpointName = "duck"
		}

		serv.app.Get(fmt.Sprintf("/%s/search", endpointName), func(c *fiber.Ctx) error {
			q := Query{}
			err := q.InitFromContext(c)
			if err != nil {
				logrus.Errorf("Error while setting %s query: %s", locEngine.Name(), err)
				return err
			}

			logrus.Infof("Starting SERP search request using %s engine for query: %s", locEngine.Name(), q.Text)

			err = limiter.Wait(context.Background())
			if err != nil {
				logrus.Errorf("Ratelimiter error during %s query: %s", locEngine.Name(), err)
			}

			res, err := locEngine.Search(q)
			if err != nil {
				switch err {
				case ErrCaptcha:
					err = fmt.Errorf("captcha found, please stop sending requests for a while\n%s", err)
				case ErrSearchTimeout:
					err = fmt.Errorf("%s", err)
				}

				logrus.Errorf("Error during %s search: %s", locEngine.Name(), err)
				return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
			}

			logrus.Infof("Successfully completed SERP search using %s engine, returned %d results", locEngine.Name(), len(res))
			return c.JSON(res)
		})

		serv.app.Get(fmt.Sprintf("/%s/image", endpointName), func(c *fiber.Ctx) error {
			q := Query{}
			err := q.InitFromContext(c)
			if err != nil {
				logrus.Errorf("Error while setting %s query: %s", locEngine.Name(), err)
				return err
			}

			logrus.Infof("Starting SERP image search request using %s engine for query: %s", locEngine.Name(), q.Text)

			err = limiter.Wait(context.Background())
			if err != nil {
				logrus.Errorf("Ratelimiter error during %s query: %s", locEngine.Name(), err)
			}

			res, err := locEngine.SearchImage(q)

			if err != nil && len(res) > 0 {
				logrus.Warnf("Partial results returned from %s image search despite error: %s", locEngine.Name(), err)
				c.Status(503)
				return c.JSON(res)
			}

			if err != nil {
				switch err {
				case ErrCaptcha:
					err = fmt.Errorf("captcha found, please stop sending requests for a while: %s", err)
				case ErrSearchTimeout:
					err = fmt.Errorf("%s", err)
				}

				logrus.Errorf("Error during %s image search: %s", locEngine.Name(), err)
				return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
			}

			logrus.Infof("Successfully completed SERP image search using [%s], returned %d results", locEngine.Name(), len(res))
			return c.JSON(res)
		})
	}

	// Add megasearch endpoint
	serv.app.Get("/mega/search", serv.handleMegaSearch)

	// Add megasearch image endpoint
	serv.app.Get("/mega/image", serv.handleMegaImage)

	// Add endpoint to list available engines
	serv.app.Get("/mega/engines", serv.handleListEngines)

	return &serv
}

// MegaSearchResult represents a search result with engine information
type MegaSearchResult struct {
	SearchResult
	Engine string `json:"engine"`
}

// handleMegaSearch handles the /megasearch endpoint
func (s *Server) handleMegaSearch(c *fiber.Ctx) error {
	q := Query{}
	err := q.InitFromContext(c)
	if err != nil {
		logrus.Errorf("Error while setting megasearch query: %s", err)
		return err
	}

	// Get engines parameter to filter which engines to use
	enginesParam := c.Query("engines", "")
	var enginesToUse []SearchEngine

	if enginesParam != "" {
		// Parse comma-separated list of engines
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
	} else {
		// Use all engines if no specific engines specified
		enginesToUse = s.searchEngines
	}

	if len(enginesToUse) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No valid search engines specified")
	}

	// Log which engines will be used
	engineNames := make([]string, len(enginesToUse))
	for i, engine := range enginesToUse {
		engineNames[i] = engine.Name()
	}
	logrus.Infof("Starting SERP megasearch request using engines: %s for query: %s", strings.Join(engineNames, ", "), q.Text)

	// Execute searches in parallel across selected engines
	results := s.searchSelectedEngines(q, enginesToUse)

	// Deduplicate results while preserving engine information
	dedupedResults := s.deduplicateMegaResults(results)

	logrus.Infof("Successfully completed SERP megasearch using %d engines, returned %d deduplicated results", len(enginesToUse), len(dedupedResults))
	return c.JSON(dedupedResults)
}

// handleMegaImage handles the /mega/image endpoint
func (s *Server) handleMegaImage(c *fiber.Ctx) error {
	q := Query{}
	err := q.InitFromContext(c)
	if err != nil {
		logrus.Errorf("Error while setting megasearch image query: %s", err)
		return err
	}

	// Get engines parameter to filter which engines to use
	enginesParam := c.Query("engines", "")
	var enginesToUse []SearchEngine

	if enginesParam != "" {
		// Parse comma-separated list of engines
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
	} else {
		// Use all engines if no specific engines specified
		enginesToUse = s.searchEngines
	}

	if len(enginesToUse) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No valid search engines specified")
	}

	// Log which engines will be used
	engineNames := make([]string, len(enginesToUse))
	for i, engine := range enginesToUse {
		engineNames[i] = engine.Name()
	}
	logrus.Infof("Starting SERP megasearch image request using engines: %s for query: %s", strings.Join(engineNames, ", "), q.Text)

	// Execute image searches in parallel across selected engines
	results := s.searchSelectedEnginesImage(q, enginesToUse)

	// Deduplicate results while preserving engine information
	dedupedResults := s.deduplicateMegaResults(results)

	logrus.Infof("Successfully completed SERP megasearch image using %d engines, returned %d deduplicated results", len(enginesToUse), len(dedupedResults))
	return c.JSON(dedupedResults)
}

// handleListEngines lists all available search engines
func (s *Server) handleListEngines(c *fiber.Ctx) error {
	var engines []map[string]interface{}

	for _, engine := range s.searchEngines {
		engines = append(engines, map[string]interface{}{
			"name":        engine.Name(),
			"initialized": engine.IsInitialized(),
		})
	}

	return c.JSON(map[string]interface{}{
		"engines": engines,
		"total":   len(engines),
	})
}

// searchSelectedEngines performs parallel searches across selected engines
func (s *Server) searchSelectedEngines(q Query, engines []SearchEngine) []MegaSearchResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			// Apply rate limiting
			limiter := eng.GetRateLimiter()
			if limiter != nil {
				err := limiter.Wait(context.Background())
				if err != nil {
					logrus.Errorf("Ratelimiter error during %s megasearch: %s", eng.Name(), err)
				}
			}

			// Perform search
			results, err := eng.Search(q)
			if err != nil {
				logrus.Errorf("Error during %s megasearch: %s", eng.Name(), err)
				return
			}

			// Convert to MegaSearchResult with engine info
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

// searchSelectedEnginesImage performs parallel image searches across selected engines
func (s *Server) searchSelectedEnginesImage(q Query, engines []SearchEngine) []MegaSearchResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []MegaSearchResult

	for _, engine := range engines {
		wg.Add(1)
		go func(eng SearchEngine) {
			defer wg.Done()

			// Apply rate limiting
			limiter := eng.GetRateLimiter()
			if limiter != nil {
				err := limiter.Wait(context.Background())
				if err != nil {
					logrus.Errorf("Ratelimiter error during %s megasearch image: %s", eng.Name(), err)
				}
			}

			// Perform image search
			results, err := eng.SearchImage(q)
			if err != nil {
				logrus.Errorf("Error during %s megasearch image: %s", eng.Name(), err)
				return
			}

			// Convert to MegaSearchResult with engine info
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

// deduplicateMegaResults deduplicates results while preserving engine information
func (s *Server) deduplicateMegaResults(results []MegaSearchResult) []MegaSearchResult {
	urlMap := make(map[string]MegaSearchResult)

	// Process results and keep the first occurrence of each URL
	for _, result := range results {
		if result.URL == "" {
			continue
		}

		if _, exists := urlMap[result.URL]; !exists {
			urlMap[result.URL] = result
		}
	}

	// Convert map back to slice and sort by rank
	var deduped []MegaSearchResult
	for _, result := range urlMap {
		deduped = append(deduped, result)
	}

	// Sort by rank
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

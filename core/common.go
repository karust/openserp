package core

import (
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ErrCaptcha is returned when the engine detects a captcha challenge page.
// This error is treated as non-retryable by resilient search policies.
var ErrCaptcha = errors.New("captcha detected")

// ErrSearchTimeout is returned when required SERP elements are not found before
// selector or page timeouts expire.
var ErrSearchTimeout = errors.New("timeout. Cannot find element on page")

// ErrParser is returned when SERP parsing selectors drift or expected fields
// cannot be extracted from an otherwise loaded page.
var ErrParser = errors.New("parser failure")

// ErrEngineInternal is returned when an engine recovered from an unexpected
// panic and converted it into a typed error.
var ErrEngineInternal = errors.New("engine internal error")

// SearchResult represents one normalized result item returned by any engine.
type SearchResult struct {
	// Rank is a 1-based position in engine output. Some engines use negative
	// ranks for non-organic blocks such as ads or instant answers.
	Rank int `json:"rank"`
	// URL is the canonical result URL.
	URL string `json:"url"`
	// Title is the result headline shown on the SERP.
	Title string `json:"title"`
	// Description is the snippet text associated with the result.
	Description string `json:"description"`
	// Ad reports whether the result is sponsored.
	Ad bool `json:"ad"`
}

// DeduplicateResults removes items with duplicate URLs and returns a result set
// sorted by rank in ascending order.
func DeduplicateResults(results []SearchResult) []SearchResult {
	unique := make(map[string]bool)
	var deduped []SearchResult

	for _, result := range results {
		if result.URL == "" {
			continue
		}
		if !unique[result.URL] {
			unique[result.URL] = true
			deduped = append(deduped, result)
		}
	}

	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].Rank < deduped[j].Rank
	})
	return deduped
}

// ConvertSearchResultsMap converts a map-based collection to a rank-sorted
// slice and returns it by pointer.
func ConvertSearchResultsMap(searchResultsMap map[string]SearchResult) *[]SearchResult {
	searchResults := []SearchResult{}

	for _, v := range searchResultsMap {
		searchResults = append(searchResults, v)
	}

	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Rank < searchResults[j].Rank
	})
	return &searchResults
}

// Query holds request parameters used by HTTP handlers and search engines.
// Example minimal query: Query{Text: "golang", Limit: 10}.
type Query struct {
	// Text is the search phrase, for example "golang fiber tutorial".
	Text string
	// LangCode is an engine language hint such as "EN", "DE", or "RU".
	LangCode string
	// DateInterval filters by date range in YYYYMMDD..YYYYMMDD format.
	// Example: "20250101..20250331".
	DateInterval string
	// Filetype is a file extension filter, for example "pdf" or "docx".
	Filetype string
	// Site restricts results to a specific domain, for example "github.com".
	Site string
	// Limit is the maximum number of results requested by the client.
	Limit int
	// Start is an engine pagination offset. Values are engine-specific:
	// Google commonly uses 0,10,20 while some engines use page indexes.
	Start int
	// Filter controls duplicate filtering when supported by the engine.
	// For Google, false includes similar results and true hides them.
	Filter bool
	// Answers enables parsing answer modules when supported by the engine.
	// Such entries may be returned with negative rank values.
	Answers bool
	// ProxyURL is a direct proxy URL used by raw HTTP search paths.
	ProxyURL string
	// ProxyOverride is a request-scoped proxy policy override (tag or "direct"),
	// typically parsed from the X-Use-Proxy header.
	ProxyOverride string
	// Insecure enables insecure TLS for request/browser execution.
	Insecure bool
}

// ComputePagination translates an absolute start offset into page index and
// in-page offset for a fixed page size.
func ComputePagination(start int, pageSize int) (int, int, error) {
	if pageSize <= 0 {
		return 0, 0, errors.New("pageSize must be > 0")
	}
	if start < 0 {
		return 0, 0, errors.New("start must be >= 0")
	}
	return start / pageSize, start % pageSize, nil
}

// IsEmpty reports whether query text operators are all absent.
func (q Query) IsEmpty() bool {
	if q.Site == "" && q.Filetype == "" && q.Text == "" {
		return true
	}
	return false
}

// InitFromContext populates Query from HTTP query parameters and request
// headers. It validates numeric/boolean inputs and returns an error for empty
// search expressions.
func (searchQuery *Query) InitFromContext(reqCtx *fiber.Ctx) error {
	searchQuery.Text = reqCtx.Query("text")
	searchQuery.LangCode = reqCtx.Query("lang")
	searchQuery.DateInterval = reqCtx.Query("date")
	searchQuery.Filetype = reqCtx.Query("file")
	searchQuery.Site = reqCtx.Query("site")

	limit, err := strconv.Atoi(reqCtx.Query("limit", "25"))
	if err != nil {
		return err
	}
	searchQuery.Limit = limit

	start, err := strconv.Atoi(reqCtx.Query("start", "0"))
	if err != nil {
		return err
	}
	if start < 0 {
		return errors.New("start must be >= 0")
	}
	searchQuery.Start = start

	searchQuery.Filter, err = strconv.ParseBool(reqCtx.Query("filter", "1"))
	if err != nil {
		return err
	}

	searchQuery.Answers, err = strconv.ParseBool(reqCtx.Query("answers", "0"))
	if err != nil {
		return err
	}

	searchQuery.ProxyOverride, err = NormalizeProxyRequestOverride(reqCtx.Get("X-Use-Proxy"))
	if err != nil {
		return err
	}

	if searchQuery.IsEmpty() {
		return errors.New("Query cannot be empty")
	}
	return nil
}

// SearchEngineOptions controls engine pacing, selector waits, and captcha
// handling behavior shared by browser and raw implementations.
type SearchEngineOptions struct {
	// RateRequests is the allowed number of requests within RateTime seconds.
	RateRequests int `mapstructure:"rate_requests"`
	// RateTime defines the rate-limiting window size in seconds.
	RateTime int64 `mapstructure:"rate_seconds"`
	// RateBurst is the token bucket burst size for short spikes.
	RateBurst int `mapstructure:"rate_burst"`
	// SelectorTimeout is the per-selector wait timeout in seconds.
	SelectorTimeout int64 `mapstructure:"selector_timeout"`
	// IsSolveCaptcha enables automatic captcha solving when engine support and
	// solver credentials are configured.
	IsSolveCaptcha bool `mapstructure:"captcha"`
}

// Init sets default option values when fields are zero.
func (o *SearchEngineOptions) Init() {
	if o.RateRequests == 0 {
		o.RateRequests = 6
	}
	if o.RateTime == 0 {
		o.RateTime = 60
	}
	if o.RateBurst == 0 {
		o.RateBurst = 1
	}
	if o.SelectorTimeout == 0 {
		o.SelectorTimeout = 5
	}
}

// GetRatelimit returns the interval between two allowed requests.
func (o *SearchEngineOptions) GetRatelimit() time.Duration {
	return (time.Duration(o.RateTime) * time.Second) / time.Duration(o.RateRequests)
}

// GetSelectorTimeout returns the selector wait timeout as time.Duration.
func (o *SearchEngineOptions) GetSelectorTimeout() time.Duration {
	return time.Duration(o.SelectorTimeout) * time.Second
}

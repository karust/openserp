package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
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

// ErrProxyConnect is returned when the proxy cannot establish a network
// connection. Proxy health is degraded on this error.
var ErrProxyConnect = errors.New("proxy_connect")

// ErrProxyAuth is returned when proxy credentials are rejected.
// Proxy health is degraded on this error.
var ErrProxyAuth = errors.New("proxy_auth")

// ErrTimeout is returned when a network-level timeout occurs on the proxy path.
// Proxy health is degraded on this error.
var ErrTimeout = errors.New("timeout")

// ErrEmptyResult signals a successful fetch that returned zero organic results.
// It is not a failure; the proxy stays healthy and no credit is charged.
var ErrEmptyResult = errors.New("empty_result")

// ErrBlocked is returned when the search engine blocks the browser request.
var ErrBlocked = errors.New("blocked")

// ErrRateLimited is returned when the search engine returns an HTTP rate limit.
var ErrRateLimited = errors.New("rate_limited")

// IsProxyNetworkError reports whether err is a network-level error that
// indicates a faulty proxy (connect failure, auth rejection, or timeout).
// Parser drift, captcha pages, and engine errors must NOT degrade proxy health.
func IsProxyNetworkError(err error) bool {
	return errors.Is(err, ErrProxyConnect) ||
		errors.Is(err, ErrProxyAuth) ||
		errors.Is(err, ErrTimeout)
}

// classifyProxyNetworkError wraps common transport errors with proxy-health
// sentinels while preserving the original error for callers.
func classifyProxyNetworkError(err error) error {
	if err == nil || IsProxyNetworkError(err) || errors.Is(err, context.Canceled) {
		return err
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "407") || strings.Contains(msg, "proxy authentication") {
		return fmt.Errorf("%w: %w", ErrProxyAuth, err)
	}

	var netErr net.Error
	if (errors.As(err, &netErr) && netErr.Timeout()) ||
		errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") {
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	}

	if strings.Contains(msg, "proxyconnect") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "socks connect") {
		return fmt.Errorf("%w: %w", ErrProxyConnect, err)
	}

	return err
}

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
	// ProxyCountry identifies the proxy market country for cache/error metadata.
	ProxyCountry string
	// ProxyClass identifies the proxy class such as datacenter or residential.
	ProxyClass string
	// ProxyProvider identifies the upstream proxy provider.
	ProxyProvider string
	// ProxySessionID identifies a sticky balancer session/lane.
	ProxySessionID string
	// ProxyOverride is a request-scoped proxy policy override (tag or "direct"),
	// typically parsed from the X-Use-Proxy header.
	ProxyOverride string
	// Insecure enables insecure TLS for request/browser execution.
	Insecure bool
}

// String renders Query for logs with the proxy URL credentials masked. The
// default %+v formatter calls this method, so logging Query through %v/%+v
// never leaks proxy passwords.
func (q Query) String() string {
	maskedProxyURL := ""
	if q.ProxyURL != "" {
		maskedProxyURL = MaskProxyURL(q.ProxyURL)
	}
	return fmt.Sprintf(
		"{Text:%s LangCode:%s DateInterval:%s Filetype:%s Site:%s Limit:%d Start:%d Filter:%t Answers:%t ProxyURL:%s ProxyCountry:%s ProxyClass:%s ProxyProvider:%s ProxySessionID:%s ProxyOverride:%s Insecure:%t}",
		q.Text, q.LangCode, q.DateInterval, q.Filetype, q.Site,
		q.Limit, q.Start, q.Filter, q.Answers,
		maskedProxyURL, q.ProxyCountry, q.ProxyClass, q.ProxyProvider,
		q.ProxySessionID, q.ProxyOverride, q.Insecure,
	)
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

// MaxQueryLimit is the maximum allowed value for the limit parameter.
const MaxQueryLimit = 100

// InitFromContext populates Query from HTTP query parameters and request
// headers. It validates numeric/boolean inputs and returns an *APIError for
// invalid client input (400) or a plain error for internal failures.
func (searchQuery *Query) InitFromContext(reqCtx *fiber.Ctx) error {
	searchQuery.Text = reqCtx.Query("text")
	searchQuery.LangCode = reqCtx.Query("lang")
	searchQuery.DateInterval = reqCtx.Query("date")
	searchQuery.Filetype = reqCtx.Query("file")
	searchQuery.Site = reqCtx.Query("site")

	limitRaw := reqCtx.Query("limit", "25")
	limit, err := strconv.Atoi(limitRaw)
	if err != nil {
		return errInvalidLimit("limit must be an integer")
	}
	if limit < 1 || limit > MaxQueryLimit {
		return errInvalidLimit(fmt.Sprintf("limit must be between 1 and %d", MaxQueryLimit))
	}
	searchQuery.Limit = limit

	startRaw := reqCtx.Query("start", "0")
	start, err := strconv.Atoi(startRaw)
	if err != nil {
		return errInvalidStart("start must be a non-negative integer")
	}
	if start < 0 {
		return errInvalidStart("start must be >= 0")
	}
	searchQuery.Start = start

	searchQuery.Filter, err = strconv.ParseBool(reqCtx.Query("filter", "1"))
	if err != nil {
		return errInvalidParam(fmt.Sprintf("filter: %v", err))
	}

	searchQuery.Answers, err = strconv.ParseBool(reqCtx.Query("answers", "0"))
	if err != nil {
		return errInvalidParam(fmt.Sprintf("answers: %v", err))
	}

	searchQuery.ProxyOverride, err = NormalizeProxyRequestOverride(reqCtx.Get("X-Use-Proxy"))
	if err != nil {
		return errInvalidParam(fmt.Sprintf("X-Use-Proxy: %v", err))
	}
	rawProxyURL := strings.TrimSpace(reqCtx.Get("X-Proxy-URL"))
	if rawProxyURL != "" {
		normalized, err := NormalizeProxyURL(rawProxyURL)
		if err != nil {
			return errInvalidParam(fmt.Sprintf("X-Proxy-URL: %v", err))
		}
		searchQuery.ProxyURL = normalized
	}
	searchQuery.ProxyCountry = strings.ToLower(strings.TrimSpace(reqCtx.Get("X-Proxy-Country")))
	searchQuery.ProxyClass = strings.ToLower(strings.TrimSpace(reqCtx.Get("X-Proxy-Class")))
	searchQuery.ProxyProvider = strings.ToLower(strings.TrimSpace(reqCtx.Get("X-Proxy-Provider")))
	searchQuery.ProxySessionID = strings.TrimSpace(reqCtx.Get("X-Proxy-Session-ID"))

	if searchQuery.IsEmpty() {
		return errEmptyQuery()
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

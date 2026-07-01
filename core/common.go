package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/time/rate"
)

// Extraction depth bounds for the unified extract=N query param. The default
// is 1 (extract=true == extract=1 == "extract one result"); callers raise it up
// to maxExtractTop. These mirror the CLI's --extract flag limits.
const (
	defaultExtractTop = 1
	maxExtractTop     = 5
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

// ErrRateLimited is returned when the search engine returns HTTP 429.
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
		strings.Contains(msg, "err_tunnel_connection_failed") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "socks connect") {
		return fmt.Errorf("%w: %w", ErrProxyConnect, err)
	}

	return err
}

// SearchResult represents one normalized result item returned by any engine.
type SearchResult struct {
	// Rank is the 1-based position within this result type. For SEO callers,
	// organic rank must not be shifted by ads.
	Rank int `json:"rank"`
	// AbsoluteRank is the 1-based position in the mixed SERP stream.
	AbsoluteRank int `json:"absolute_rank,omitempty"`
	// Type is the SERP block type when an engine can classify a non-standard
	// SERP module without changing the public SearchEngine interface.
	Type ResultType `json:"type,omitempty"`
	// URL is the canonical result URL.
	URL string `json:"url"`
	// Title is the result headline shown on the SERP.
	Title string `json:"title"`
	// Description is the snippet text associated with the result.
	Description string `json:"description"`
	// Ad reports whether the result is sponsored.
	Ad bool `json:"ad"`
	// Features carries extracted SERP modules alongside the legacy result stream.
	Features []SerpFeature `json:"-"`
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
		key := resultDedupKey(result)
		if !unique[key] {
			unique[key] = true
			deduped = append(deduped, result)
		}
	}

	sort.Slice(deduped, func(i, j int) bool {
		return resultLess(deduped[i], deduped[j])
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
		return resultLess(searchResults[i], searchResults[j])
	})
	return &searchResults
}

// CountOrganicResults returns the number of non-ad results in a mixed SERP.
func CountOrganicResults(results []SearchResult) int {
	count := 0
	for _, result := range results {
		if !result.Ad {
			count++
		}
	}
	return count
}

// OrganicLimitReached reports whether enough organic results have been
// collected to satisfy limit. A non-positive limit means "no limit", so it
// is never reached and pagination continues until the engine runs out.
func OrganicLimitReached(results []SearchResult, limit int) bool {
	return limit > 0 && CountOrganicResults(results) >= limit
}

// ShouldFetchResultPage reports whether a paginated engine should fetch another
// SERP page. Small/default limits should use the first SERP page as-is instead
// of chasing a target count across multiple page loads.
func ShouldFetchResultPage(collected, limit, pagesFetched int) bool {
	if pagesFetched <= 0 {
		return true
	}
	if limit > 0 && collected >= limit {
		return false
	}
	return limit > defaultQueryLimit
}

// LimitOrganicResults keeps all ads and at most limit non-ad results.
func LimitOrganicResults(results []SearchResult, limit int) []SearchResult {
	if limit <= 0 {
		return results
	}
	out := make([]SearchResult, 0, len(results))
	organicCount := 0
	for _, result := range results {
		if result.Ad {
			out = append(out, result)
			continue
		}
		if organicCount >= limit {
			continue
		}
		organicCount++
		out = append(out, result)
	}
	return out
}

func resultDedupKey(result SearchResult) string {
	resultType := "organic"
	if result.Ad {
		resultType = "ad"
	}
	return resultType + "\x00" + result.URL
}

func resultLess(left, right SearchResult) bool {
	leftPos := resultSortPosition(left)
	rightPos := resultSortPosition(right)
	if leftPos != rightPos {
		return leftPos < rightPos
	}
	if left.Ad != right.Ad {
		return left.Ad
	}
	if left.Rank != right.Rank {
		return left.Rank < right.Rank
	}
	return left.URL < right.URL
}

func resultSortPosition(result SearchResult) int {
	if result.AbsoluteRank > 0 {
		return result.AbsoluteRank
	}
	if result.Rank < 0 {
		return -result.Rank
	}
	if result.Rank > 0 {
		return result.Rank
	}
	return int(^uint(0) >> 1)
}

// Query holds request parameters used by HTTP handlers and search engines.
// Example minimal query: Query{Text: "golang", Limit: 10}.
type Query struct {
	// Text is the search phrase, for example "golang fiber tutorial".
	Text string
	// LangCode is an engine language hint such as "EN", "DE", or "RU".
	LangCode string
	// Region is an engine market/location hint. Yandex accepts numeric lr IDs;
	// global engines accept country-style hints such as "RU" or "en-RU".
	Region string
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
	// Features enables parsing SERP feature modules (AI summaries, answer boxes,
	// people-also-ask, related searches) on the browser Search path when
	// supported by the engine. Such entries may be returned with non-positive
	// internal rank values.
	Features bool
	// Extract fetches and embeds cleaned target-page content for top results.
	Extract bool
	// ExtractTop limits how many top results are enriched when Extract is true.
	ExtractTop int
	// ExtractMode selects auto, fast, or rendered extraction.
	ExtractMode string
	// ExtractMinRunes overrides the auto-mode escalation floor (0 = default).
	ExtractMinRunes int
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
	// GuardPrivateNetworks rejects raw HTTP targets that resolve to private,
	// loopback, link-local, multicast, or otherwise non-public addresses.
	GuardPrivateNetworks bool
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
		"{Text:%s LangCode:%s Region:%s DateInterval:%s Filetype:%s Site:%s Limit:%d Start:%d Filter:%t Features:%t Extract:%t ExtractTop:%d ExtractMode:%s ProxyURL:%s ProxyCountry:%s ProxyClass:%s ProxyProvider:%s ProxySessionID:%s ProxyOverride:%s Insecure:%t}",
		q.Text, q.LangCode, q.Region, q.DateInterval, q.Filetype, q.Site,
		q.Limit, q.Start, q.Filter, q.Features, q.Extract, q.ExtractTop, q.ExtractMode,
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

// defaultQueryLimit is the assumed limit when a request omits it (InitFromContext)
// and the fallback used by pagination math for internally-built queries that
// leave Limit unset.
const defaultQueryLimit = 10

// InitFromContext populates Query from HTTP query parameters and request
// headers. It validates numeric/boolean inputs and returns an *APIError for
// invalid client input (400) or a plain error for internal failures.
func (searchQuery *Query) InitFromContext(reqCtx *fiber.Ctx) error {
	searchQuery.Text = strings.TrimSpace(reqCtx.Query("text"))
	searchQuery.LangCode = strings.TrimSpace(reqCtx.Query("lang"))
	searchQuery.Region = strings.TrimSpace(reqCtx.Query("region"))
	searchQuery.DateInterval = strings.TrimSpace(reqCtx.Query("date"))
	searchQuery.Filetype = strings.TrimSpace(reqCtx.Query("file"))
	searchQuery.Site = strings.TrimSpace(reqCtx.Query("site"))

	limitRaw := reqCtx.Query("limit", strconv.Itoa(defaultQueryLimit))
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

	searchQuery.Features, err = strconv.ParseBool(reqCtx.Query("features", "1"))
	if err != nil {
		return errInvalidParam(fmt.Sprintf("features: %v", err))
	}
	// extract is a unified bool-or-int knob: extract=0/false disables, extract=N
	// (or true/1) extracts the top N results. The tuning params extract_mode and
	// min_runes also imply extraction (extract=0 still overrides them). The
	// default depth is 1 — true == 1 == "extract one result".
	if err := parseExtractParams(reqCtx, searchQuery); err != nil {
		return err
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

// parseExtractParams reads the unified extract knob plus its tuning params onto
// q. The extract param is bool-or-int:
//
//	extract=0 / extract=false  → extraction off
//	extract=true / extract=1   → on, top 1
//	extract=N (1..5)           → on, top N (clamped to maxExtractTop)
//
// extract_mode and min_runes tune how extraction runs and imply extraction when
// present, unless extract is explicitly set (extract=0 wins over them). When
// extraction is on but no depth is given, ExtractTop defaults to 1.
func parseExtractParams(reqCtx *fiber.Ctx, q *Query) error {
	q.ExtractTop = defaultExtractTop

	// extract accepts both bool spellings (true/false/1/0) and an integer depth.
	// Try bool first so legacy true/false keep working, then fall back to int.
	extractExplicit := false
	if raw := strings.TrimSpace(reqCtx.Query("extract")); raw != "" {
		extractExplicit = true
		if b, err := strconv.ParseBool(raw); err == nil {
			q.Extract = b
			if b {
				q.ExtractTop = 1
			}
		} else if n, err := strconv.Atoi(raw); err == nil {
			q.Extract = n > 0
			if n > 0 {
				q.ExtractTop = clampExtractTop(n)
			}
		} else {
			return errInvalidParam("extract must be a boolean or an integer (0 disables, N extracts top N)")
		}
	}

	q.ExtractMode = strings.ToLower(strings.TrimSpace(reqCtx.Query("extract_mode", "auto")))
	switch q.ExtractMode {
	case "auto", "fast", "rendered":
	default:
		return errInvalidParam("extract_mode must be one of auto, fast, rendered")
	}
	if !extractExplicit && strings.TrimSpace(reqCtx.Query("extract_mode")) != "" {
		q.Extract = true
	}

	minRunes, err := parseNonNegativeIntQuery(reqCtx.Query("min_runes"), 0)
	if err != nil {
		return errInvalidParam("min_runes must be a non-negative integer")
	}
	q.ExtractMinRunes = minRunes
	if !extractExplicit && strings.TrimSpace(reqCtx.Query("min_runes")) != "" {
		q.Extract = true
	}

	return nil
}

// clampExtractTop bounds a requested extraction depth to [1, maxExtractTop].
func clampExtractTop(n int) int {
	if n < 1 {
		return 1
	}
	if n > maxExtractTop {
		return maxExtractTop
	}
	return n
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

	limiterState *rateLimiterState
}

type rateLimiterState struct {
	limiter *rate.Limiter
	every   time.Duration
	burst   int
}

var searchEngineOptionsLimiterMu sync.Mutex

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
// Call Init() first so RateRequests / RateTime are non-zero.
func (o *SearchEngineOptions) GetRatelimit() time.Duration {
	return (time.Duration(o.RateTime) * time.Second) / time.Duration(o.RateRequests)
}

// GetRateLimiter returns a cached limiter configured from SearchEngineOptions.
// Call Init() first so RateBurst is non-zero. Do not copy SearchEngineOptions
// after first use; the limiter state is intentionally shared by each engine.
func (o *SearchEngineOptions) GetRateLimiter() *rate.Limiter {
	every := o.GetRatelimit()
	burst := o.RateBurst
	searchEngineOptionsLimiterMu.Lock()
	defer searchEngineOptionsLimiterMu.Unlock()
	if o.limiterState == nil {
		o.limiterState = &rateLimiterState{}
	}
	if o.limiterState.limiter == nil || o.limiterState.every != every || o.limiterState.burst != burst {
		o.limiterState.limiter = rate.NewLimiter(rate.Every(every), burst)
		o.limiterState.every = every
		o.limiterState.burst = burst
	}
	return o.limiterState.limiter
}

// GetSelectorTimeout returns the selector wait timeout as time.Duration.
func (o *SearchEngineOptions) GetSelectorTimeout() time.Duration {
	return time.Duration(o.SelectorTimeout) * time.Second
}

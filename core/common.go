package core

import (
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

var ErrCaptcha = errors.New("captcha detected")
var ErrSearchTimeout = errors.New("timeout. Cannot find element on page")

type SearchResult struct {
	Rank        int    `json:"rank"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Ad          bool   `json:"ad"`
}

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

type Query struct {
	Text         string
	LangCode     string // eg. EN, ES, RU...
	DateInterval string // format: YYYYMMDD..YYYMMDD - 20181010..20231010
	Filetype     string // File extension to search.
	Site         string // Search site
	Limit        int    // Limit the number of results
	Answers      bool   // Include question and answers from SERP page to results with negative indexes
}

func (q Query) IsEmpty() bool {
	if q.Site == "" && q.Filetype == "" && q.Text == "" {
		return true
	}
	return false
}

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

	searchQuery.Answers, err = strconv.ParseBool(reqCtx.Query("answers", "0"))
	if err != nil {
		return err
	}

	if searchQuery.IsEmpty() {
		return errors.New("Query cannot be empty")
	}
	return nil
}

type SearchEngineOptions struct {
	RateRequests    int   `mapstructure:"rate_requests"`
	RateTime        int64 `mapstructure:"rate_seconds"`
	RateBurst       int   `mapstructure:"rate_burst"`
	SelectorTimeout int64 `mapstructure:"selector_timeout"` // CSS selector timeout in seconds
	IsSolveCaptcha  bool  `mapstructure:"captcha"`
}

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

func (o *SearchEngineOptions) GetRatelimit() time.Duration {
	return (time.Duration(o.RateTime) * time.Second) / time.Duration(o.RateRequests)
}

func (o *SearchEngineOptions) GetSelectorTimeout() time.Duration {
	return time.Duration(o.SelectorTimeout) * time.Second
}

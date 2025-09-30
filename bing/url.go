package bing

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func BuildURL(q core.Query) (string, error) {
	base, err := url.Parse("https://www.bing.com")
	if err != nil {
		return "", err
	}

	base.Path += "search"
	params := url.Values{}

	// Set search query text with operators
	if q.Text != "" || q.Site != "" || q.Filetype != "" {
		text := q.Text
		if q.Site != "" {
			text += " site:" + q.Site
		}
		if q.Filetype != "" {
			text += " filetype:" + q.Filetype
		}

		logrus.Tracef("Query text: %s", text)
		params.Add("q", text)
	}

	if len(params.Get("q")) == 0 {
		return "", errors.New("empty query built")
	}

	if q.LangCode != "" {
		params.Add("setlang", strings.ToLower(q.LangCode))
	}

	// Set result offset (pagination) - Bing uses "first" parameter
	if q.Limit > 0 {
		params.Add("count", strconv.Itoa(q.Limit))
	}

	// Set search date range - Bing supports date filtering via query text
	if q.DateInterval != "" {
		intervals := strings.Split(q.DateInterval, "..")
		if len(intervals) != 2 {
			return "", errors.New("incorrect date interval provided, expected format: YYYYMMDD..YYYYMMDD")
		}

		// Convert YYYYMMDD to YYYY-MM-DD format for Bing
		startDate, err := time.Parse("20060102", intervals[0])
		if err != nil {
			return "", errors.New("invalid start date format, expected YYYYMMDD")
		}

		endDate, err := time.Parse("20060102", intervals[1])
		if err != nil {
			return "", errors.New("invalid end date format, expected YYYYMMDD")
		}

		// Add date range to the search query text (Bing supports this format)
		dateRange := fmt.Sprintf(" after:%s before:%s",
			startDate.Format("2006-01-02"),
			endDate.Format("2006-01-02"))

		// Update the query text to include date range
		currentQuery := params.Get("q")
		params.Set("q", currentQuery+dateRange)
	}

	// Bing-specific parameters for consistent results
	params.Add("form", "QBLH")        // Standard search form
	params.Add("qs", "HS")            // Query suggestions
	params.Add("sp", "-1")            // Search provider
	params.Add("pq", params.Get("q")) // Previous query

	base.RawQuery = params.Encode()
	return base.String(), nil
}

func BuildImageURL(q core.Query) (string, error) {
	base, err := url.Parse("https://www.bing.com")
	if err != nil {
		return "", err
	}

	base.Path += "images/search"
	params := url.Values{}

	if q.Text != "" || q.Site != "" {
		text := q.Text
		if q.Site != "" {
			text += " site:" + q.Site
		}
		params.Add("q", text)
	}

	if len(params.Get("q")) == 0 {
		return "", errors.New("empty query built")
	}

	// Add common parameters
	if q.LangCode != "" {
		params.Add("setlang", strings.ToLower(q.LangCode))
	}

	// Image-specific parameters
	params.Add("form", "HDRSC2")
	params.Add("first", "1")
	params.Add("scenario", "ImageBasicHover")

	base.RawQuery = params.Encode()
	return base.String(), nil
}

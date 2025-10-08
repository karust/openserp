package duckduckgo

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/karust/openserp/core"
)

const baseURL = "https://duckduckgo.com"

func BuildURL(q core.Query, page int) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	base.Path += ""
	params := url.Values{}

	// Set request text
	if q.Text != "" || q.Site != "" || q.Filetype != "" {
		text := q.Text
		if q.Site != "" {
			text += " site:" + q.Site
		}
		if q.Filetype != "" {
			text += " filetype:" + q.Filetype
		}

		params.Add("q", text)
	}

	if len(params.Get("q")) == 0 {
		return "", errors.New("empty query built")
	}

	// Set search date range
	if q.DateInterval != "" {
		intervals := strings.Split(q.DateInterval, "..")
		if len(intervals) != 2 {
			return "", errors.New("incorrect date interval provided")
		}

		// Convert from YYYYMMDD to YYYY-MM-DD format (DuckDuckGo requirement)
		startDate, err := time.Parse("20060102", intervals[0])
		if err != nil {
			return "", errors.New("invalid start date format, expected YYYYMMDD")
		}

		endDate, err := time.Parse("20060102", intervals[1])
		if err != nil {
			return "", errors.New("invalid end date format, expected YYYYMMDD")
		}

		// DuckDuckGo uses YYYY-MM-DD..YYYY-MM-DD format
		dateRange := fmt.Sprintf("%s..%s",
			startDate.Format("2006-01-02"),
			endDate.Format("2006-01-02"))
		params.Add("df", dateRange)
	}

	// Set language
	if q.LangCode != "" {
		params.Add("kl", strings.ToLower(q.LangCode))
	}

	// DuckDuckGo specific parameters
	params.Add("t", "h")    // HTML format
	params.Add("ia", "web") // Web search

	// Add pagination parameter if not on first page
	if page > 0 {
		// DuckDuckGo uses 's' parameter for pagination (start offset)
		// Each page has approximately 25-30 results, but we'll use conservative estimate
		offset := page * 25
		params.Add("s", fmt.Sprintf("%d", offset))
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

func BuildImageURL(q core.Query) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	base.Path += ""
	params := url.Values{}
	params.Add("t", "h")        // HTML format
	params.Add("iax", "images") // Image search
	params.Add("ia", "images")

	// Set request text
	if q.Text != "" || q.Site != "" || q.Filetype != "" {
		text := q.Text
		if q.Site != "" {
			text += " site:" + q.Site
		}
		if q.Filetype != "" {
			text += " filetype:" + q.Filetype
		}

		params.Add("q", text)
	}

	if len(params.Get("q")) == 0 {
		return "", errors.New("empty query built")
	}

	// Set search date range
	if q.DateInterval != "" {
		intervals := strings.Split(q.DateInterval, "..")
		if len(intervals) != 2 {
			return "", errors.New("incorrect date interval provided")
		}

		// Convert from YYYYMMDD to YYYY-MM-DD format (DuckDuckGo requirement)
		startDate, err := time.Parse("20060102", intervals[0])
		if err != nil {
			return "", errors.New("invalid start date format, expected YYYYMMDD")
		}

		endDate, err := time.Parse("20060102", intervals[1])
		if err != nil {
			return "", errors.New("invalid end date format, expected YYYYMMDD")
		}

		// DuckDuckGo uses YYYY-MM-DD..YYYY-MM-DD format
		dateRange := fmt.Sprintf("%s..%s",
			startDate.Format("2006-01-02"),
			endDate.Format("2006-01-02"))
		params.Add("df", dateRange)
	}

	// Set language
	if q.LangCode != "" {
		params.Add("kl", strings.ToLower(q.LangCode))
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

package bing

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

var bingDateOperatorRE = regexp.MustCompile(`(?i)\b(after|before):(\d{4}-\d{2}-\d{2})\b`)

// BuildURL builds a Bing web search URL from Query fields.
// It returns an error when query text or date parameters are invalid.
func BuildURL(q core.Query) (string, error) {
	base, err := url.Parse("https://www.bing.com")
	if err != nil {
		return "", err
	}

	base.Path += "search"
	params := url.Values{}

	// Set search query text with operators
	if q.Text != "" || q.Site != "" || q.Filetype != "" {
		text, textDateInterval, err := normalizeBingQueryText(q.Text)
		if err != nil {
			return "", err
		}
		if q.DateInterval == "" {
			q.DateInterval = textDateInterval
		}
		if q.Site != "" {
			text += " site:" + q.Site
		}
		if q.Filetype != "" {
			text += " filetype:" + q.Filetype
		}

		logrus.WithField("query_hash", core.QueryHash(text)).Trace(fmt.Sprintf("Query text: %s", text))
		params.Add("q", text)
	}

	if len(params.Get("q")) == 0 {
		return "", errors.New("empty query built")
	}

	addBingLocale(params, q.LangCode, q.Region)

	// Bing ignores count, so deeper results come from paging. first is a
	// 1-based result index; FORM=PORE is what Bing's own "next" link sends.
	if q.Start < 0 {
		return "", errors.New("incorrect start provided")
	}
	if q.Start > 0 {
		params.Add("first", strconv.Itoa(q.Start+1))
		params.Add("FORM", "PORE")
	}

	if q.DateInterval != "" {
		filter, err := buildBingDateFilter(q.DateInterval)
		if err != nil {
			return "", err
		}
		params.Add("filters", filter)
	}

	// Omit autosuggest state; fresh sessions can reuse stale q otherwise.

	base.RawQuery = params.Encode()
	return base.String(), nil
}

// Bing serves unrelated results when mkt or cc=US/CN/RU differs from the exit IP.
func addBingLocale(params url.Values, langCode, region string) {
	parsed := core.ParseLocale(langCode)
	if parsed.Language != "" {
		params.Add("setlang", parsed.Language)
	}

	country := core.CountryFromRegion(region)
	if country == "" && len(parsed.Country) == 2 {
		country = parsed.Country
	}
	switch country {
	case "", "US", "CN", "RU":
	default:
		params.Add("cc", country)
	}
}

// Search opens the plain query first
// so Bing honors paging and date filters on the next load in the same tab.
func sessionURL(searchURL string) string {
	u, err := url.Parse(searchURL)
	if err != nil {
		return searchURL
	}
	params := u.Query()
	for _, key := range []string{"first", "FORM", "filters"} {
		params.Del(key)
	}
	u.RawQuery = params.Encode()
	return u.String()
}

func normalizeBingQueryText(text string) (string, string, error) {
	matches := bingDateOperatorRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text, "", nil
	}

	var after, before string
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		switch strings.ToLower(match[1]) {
		case "after":
			after = strings.ReplaceAll(match[2], "-", "")
		case "before":
			before = strings.ReplaceAll(match[2], "-", "")
		}
	}

	cleaned := bingDateOperatorRE.ReplaceAllString(text, "")
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	if after == "" && before == "" {
		return cleaned, "", nil
	}
	if after == "" || before == "" {
		return cleaned, "", nil
	}
	if _, err := buildBingDateFilter(after + ".." + before); err != nil {
		return "", "", err
	}
	return cleaned, after + ".." + before, nil
}

func buildBingDateFilter(dateInterval string) (string, error) {
	intervals := strings.Split(dateInterval, "..")
	if len(intervals) != 2 {
		return "", errors.New("incorrect date interval provided, expected format: YYYYMMDD..YYYYMMDD")
	}

	startDate, err := time.Parse("20060102", intervals[0])
	if err != nil {
		return "", errors.New("invalid start date format, expected YYYYMMDD")
	}

	endDate, err := time.Parse("20060102", intervals[1])
	if err != nil {
		return "", errors.New("invalid end date format, expected YYYYMMDD")
	}

	if startDate.After(endDate) {
		return "", errors.New("start date must not be after end date")
	}

	const secondsPerDay = int64(24 * 60 * 60)
	startDay := startDate.Unix() / secondsPerDay
	endDay := endDate.Unix() / secondsPerDay

	return fmt.Sprintf(`ex1:"ez5_%d_%d"`, startDay, endDay), nil
}

// BuildImageURL builds a Bing image search URL from Query fields.
// It returns an error when the resulting query text is empty.
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

	addBingLocale(params, q.LangCode, q.Region)

	// Image-specific parameters
	params.Add("form", "HDRSC2")
	params.Add("first", "1")
	params.Add("scenario", "ImageBasicHover")

	base.RawQuery = params.Encode()
	return base.String(), nil
}

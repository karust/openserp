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

// defaultBingCountryByLanguage maps a language subtag to the country Bing
// pairs with it for the "mkt" parameter when the caller did not specify one.
// Languages outside this map fall back to "US" rather than echoing the
// language code, since Bing rejects unknown markets like "ja-JA".
var defaultBingCountryByLanguage = map[string]string{
	"en": "US",
	"de": "DE",
	"ru": "RU",
	"fr": "FR",
	"es": "ES",
	"it": "IT",
	"pt": "BR",
	"zh": "CN",
	"ja": "JP",
	"ko": "KR",
	"nl": "NL",
	"pl": "PL",
	"tr": "TR",
	"ar": "SA",
}

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

	if locale, ok := bingLocale(q.LangCode, q.Region); ok {
		if locale.market != "" {
			params.Add("mkt", locale.market)
		}
		if locale.language != "" {
			params.Add("setlang", locale.language)
		}
		params.Add("cc", locale.country)
	}

	// Set result offset (pagination) - Bing uses "first" parameter.
	// When first is present, Bing may ignore custom count and return default page size.
	if q.Start < 0 {
		return "", errors.New("incorrect start provided")
	}
	if q.Start > 0 {
		// Bing uses 1-based first-result index for pagination.
		params.Add("first", strconv.Itoa(q.Start+1))
	} else if q.Limit > 10 {
		params.Add("count", strconv.Itoa(q.Limit))
	}

	if q.DateInterval != "" {
		filter, err := buildBingDateFilter(q.DateInterval)
		if err != nil {
			return "", err
		}
		params.Add("filters", filter)
	}

	// Bing-specific parameters for consistent results
	params.Add("form", "QBLH")        // Standard search form
	params.Add("qs", "HS")            // Query suggestions
	params.Add("sp", "-1")            // Search provider
	params.Add("pq", params.Get("q")) // Previous query

	base.RawQuery = params.Encode()
	return base.String(), nil
}

type bingLocaleParams struct {
	language string
	country  string
	market   string
}

// bingLocale resolves a Bing market triplet (language, country, mkt) from a
// caller-supplied language code. It returns ok=false when the input is empty
// so callers can omit Bing's locale parameters entirely instead of forcing a
// default market that biases results toward en-US.
func bingLocale(langCode, region string) (bingLocaleParams, bool) {
	parsed := core.ParseLocale(langCode)
	country := core.CountryFromRegion(region)
	if parsed.Language == "" {
		if country != "" {
			return bingLocaleParams{country: country}, true
		}
		return bingLocaleParams{}, false
	}

	if country == "" {
		country = parsed.Country
		if country == "" {
			country = defaultBingCountry(parsed.Language)
		}
	}
	return bingLocaleParams{
		language: parsed.Language,
		country:  country,
		market:   parsed.Language + "-" + country,
	}, true
}

func defaultBingCountry(language string) string {
	if country, ok := defaultBingCountryByLanguage[language]; ok {
		return country
	}
	return "US"
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

	if locale, ok := bingLocale(q.LangCode, q.Region); ok {
		if locale.market != "" {
			params.Add("mkt", locale.market)
		}
		if locale.language != "" {
			params.Add("setlang", locale.language)
		}
		params.Add("cc", locale.country)
	}

	// Image-specific parameters
	params.Add("form", "HDRSC2")
	params.Add("first", "1")
	params.Add("scenario", "ImageBasicHover")

	base.RawQuery = params.Encode()
	return base.String(), nil
}

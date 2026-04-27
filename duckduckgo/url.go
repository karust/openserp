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

// ddgKLByLocale maps lowercase BCP47 codes ("en", "en-gb", "zh-tw") to
// DuckDuckGo's "kl" parameter, which uses an inverted region-language form
// (e.g. "uk-en"). Lookup is locale-first, then language-only as a fallback.
var ddgKLByLocale = map[string]string{
	"en":    "us-en",
	"en-us": "us-en",
	"en-gb": "uk-en",
	"en-au": "au-en",
	"en-ca": "ca-en",
	"de":    "de-de",
	"de-at": "at-de",
	"de-ch": "ch-de",
	"fr":    "fr-fr",
	"fr-ca": "ca-fr",
	"fr-be": "be-fr",
	"fr-ch": "ch-fr",
	"es":    "es-es",
	"es-mx": "mx-es",
	"es-ar": "ar-es",
	"it":    "it-it",
	"nl":    "nl-nl",
	"nl-be": "be-nl",
	"pt":    "pt-pt",
	"pt-br": "br-pt",
	"ru":    "ru-ru",
	"pl":    "pl-pl",
	"cs":    "cz-cs",
	"sk":    "sk-sk",
	"hu":    "hu-hu",
	"ro":    "ro-ro",
	"da":    "dk-da",
	"sv":    "se-sv",
	"no":    "no-no",
	"fi":    "fi-fi",
	"tr":    "tr-tr",
	"el":    "gr-el",
	"he":    "il-he",
	"ar":    "xa-ar",
	"zh":    "cn-zh",
	"zh-cn": "cn-zh",
	"zh-tw": "tw-zh",
	"ja":    "jp-ja",
	"ko":    "kr-ko",
}

// duckDuckGoKL resolves a DuckDuckGo "kl" value for the supplied language code.
// Returns "" when the input has no known mapping so callers can omit the
// parameter rather than send an unrecognized region.
func duckDuckGoKL(langCode string) string {
	locale := core.ParseLocale(langCode)
	if locale.Language == "" {
		return ""
	}

	if locale.Country != "" {
		key := locale.Language + "-" + strings.ToLower(locale.Country)
		if kl, ok := ddgKLByLocale[key]; ok {
			return kl
		}
	}
	return ddgKLByLocale[locale.Language]
}

// BuildURL builds a DuckDuckGo web search URL for the provided query and page
// index. It returns an error when query text or date parameters are invalid.
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

	if kl := duckDuckGoKL(q.LangCode); kl != "" {
		params.Add("kl", kl)
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

// BuildImageURL builds a DuckDuckGo image search URL from Query fields.
// It returns an error when query text or date parameters are invalid.
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

	if kl := duckDuckGoKL(q.LangCode); kl != "" {
		params.Add("kl", kl)
	}
	base.RawQuery = params.Encode()
	return base.String(), nil
}

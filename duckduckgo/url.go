package duckduckgo

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/karust/openserp/core"
)

const baseURL = "https://duckduckgo.com"

func BuildURL(q core.Query) (string, error) {
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
			return "", errors.New("incorrect data interval provided")
		}
		// DuckDuckGo uses different date format
		params.Add("df", fmt.Sprintf("%s..%s", intervals[0], intervals[1]))
	}

	// Set language
	if q.LangCode != "" {
		params.Add("kl", strings.ToLower(q.LangCode))
	}

	// DuckDuckGo specific parameters
	params.Add("t", "h")    // HTML format
	params.Add("ia", "web") // Web search

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

	// Set language
	if q.LangCode != "" {
		params.Add("kl", strings.ToLower(q.LangCode))
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

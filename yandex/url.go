package yandex

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/karust/openserp/core"
)

const baseURL = "https://www.yandex.com"

// BuildURL builds a Yandex web search URL for the provided query and page
// index. It returns an error when the resulting query text is empty.
func BuildURL(q core.Query, page int) (string, error) {
	base, _ := url.Parse(baseURL)
	base.Path += "search/"

	params := url.Values{}
	if q.Text != "" || q.Site != "" || q.Filetype != "" {
		text := q.Text
		if q.Site != "" {
			text += " site:" + q.Site
		}
		if q.Filetype != "" {
			text += " mime:" + q.Filetype
		}
		if q.DateInterval != "" {
			text += " date:" + q.DateInterval
		}
		if locale := core.ParseLocale(q.LangCode); locale.Language != "" {
			// Yandex's lang: operator accepts the lowercase language subtag
			// only; region modifiers must go through the lr= parameter.
			text += " lang:" + locale.Language
		}

		params.Add("text", text)
		params.Add("p", fmt.Sprint(page))
	}

	if len(params.Get("text")) == 0 {
		return "", errors.New("empty query built")
	}

	if lr := yandexLR(q.Region); lr != "" {
		params.Add("lr", lr)
		params.Add("rstr", "true")
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

// BuildImageURL builds a Yandex image search URL for the provided query and
// page index. It returns an error when the resulting query text is empty.
func BuildImageURL(q core.Query, page int) (string, error) {
	// TODO: Add other parameters
	base, _ := url.Parse(baseURL)
	base.Path += "images/search/"

	params := url.Values{}
	if q.Text != "" {
		text := q.Text

		if q.DateInterval != "" {
			text += " date:" + q.DateInterval
		}

		params.Add("text", text)
		params.Add("p", fmt.Sprint(page))
	}

	if len(params.Get("text")) == 0 {
		return "", errors.New("empty query built")
	}

	if q.Site != "" {
		params.Add("site", q.Site)
	}

	if q.Filetype != "" {
		params.Add("itype", q.Filetype)
	}

	if lr := yandexLR(q.Region); lr != "" {
		params.Add("lr", lr)
		params.Add("rstr", "true")
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

func yandexLR(region string) string {
	return core.ResolveRegion(region).YandexLR
}

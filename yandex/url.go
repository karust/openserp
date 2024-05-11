package yandex

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/karust/openserp/core"
)

const baseURL = "https://www.yandex.com"

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
		if q.LangCode != "" {
			text += " lang:" + q.LangCode
		}

		params.Add("text", text)
		params.Add("p", fmt.Sprint(page))
	}

	if len(params.Get("text")) == 0 {
		return "", errors.New("empty query built")
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

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

	base.RawQuery = params.Encode()
	return base.String(), nil
}

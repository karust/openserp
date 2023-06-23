package yandex

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/karust/openserp/core"
)

func BuildURL(q core.Query, page int) (string, error) {
	base, _ := url.Parse("https://www.yandex.com")
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
		return "", errors.New("Empty query built")
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

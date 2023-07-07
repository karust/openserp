package baidu

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

func dateToTimestamp(date string) (int64, error) {
	layout := "20060201"
	t, err := time.Parse(layout, date)
	if err != nil {
		return 0, err
	}

	return t.Unix(), nil
}

func BuildURL(q core.Query) (string, error) {
	base, _ := url.Parse("https://www.baidu.com/")
	base.Path += "s"

	params := url.Values{}
	if q.Text != "" || q.Site != "" || q.Filetype != "" {
		text := q.Text
		if q.Site != "" {
			text += " site:" + q.Site
		}
		if q.Filetype != "" {
			text += " filetype:" + q.Filetype
		}
		params.Add("wd", text)
	}

	if q.DateInterval != "" {
		dateInterval := strings.Split(q.DateInterval, "..")

		ts1, err := dateToTimestamp(dateInterval[0])
		if err != nil {
			return "", err
		}
		ts2, err := dateToTimestamp(dateInterval[1])
		if err != nil {
			return "", err
		}

		params.Add("gpc", fmt.Sprintf("stf=%d,%d|stftype=2", ts1, ts2))
	}

	if q.LangCode != "" {
		//params.Add("rqlang", q.LangCode)
		logrus.Warn("Baidu's Language specific search not supported yet")
	}

	if q.Filetype != "" {
		//params.Add("ft", q.Filetype)
		logrus.Warn("Baidu's File search not supported yet")
	}

	if q.Limit != 0 {
		params.Add("rn", strconv.Itoa(q.Limit))
	}

	if len(params.Get("wd")) == 0 {
		return "", errors.New("Empty query built")
	}

	params.Add("f", "8")
	params.Add("ie", "utf-8")
	base.RawQuery = params.Encode()
	return base.String(), nil
}

func BuildImageURL(q core.Query, pageNum int) (string, error) {
	base, _ := url.Parse("https://image.baidu.com/")
	base.Path += "search/acjson"

	params := url.Values{}
	params.Add("tn", "resultjson_com")
	params.Add("cl", "2") // Cl = 2 indicates image search

	if q.Text != "" {
		params.Add("word", q.Text)
	}

	if len(params.Get("word")) == 0 {
		return "", errors.New("Empty query built")
	}

	if q.Limit != 0 {
		params.Add("rn", "30")                     // Results per page
		params.Add("pn", strconv.Itoa(pageNum*30)) // Offset
	}

	params.Add("fp", "result")
	params.Add("ipn", "rj")
	params.Add("ie", "utf-8")
	params.Add("oe", "utf-8")
	base.RawQuery = params.Encode()
	return base.String(), nil
}

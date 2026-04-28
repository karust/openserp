package ecosia

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/karust/openserp/core"
)

const (
	baseURL   = "https://www.ecosia.org/search"
	imagesURL = "https://www.ecosia.org/images"
)

// ecosiaFreshness maps a YYYYMMDD..YYYYMMDD DateInterval to Ecosia's freshness
// bucket: <= 1d ->  day, <= 7d -> week, <= 31d -> month. Longer spans are
// dropped (returns "", nil) since Ecosia exposes no finer control. Malformed
// input is rejected so non-spec values do not silently lose the filter.
func ecosiaFreshness(dateInterval string) (string, error) {
	s := strings.TrimSpace(dateInterval)
	if s == "" {
		return "", nil
	}
	parts := strings.Split(s, "..")
	if len(parts) != 2 {
		return "", errors.New("incorrect date interval provided, expected YYYYMMDD..YYYYMMDD")
	}
	start, err := time.Parse("20060102", parts[0])
	if err != nil {
		return "", errors.New("invalid start date format, expected YYYYMMDD")
	}
	end, err := time.Parse("20060102", parts[1])
	if err != nil {
		return "", errors.New("invalid end date format, expected YYYYMMDD")
	}
	span := end.Sub(start)
	if span < 0 {
		return "", errors.New("date interval end is before start")
	}
	switch {
	case span <= 24*time.Hour:
		return "day", nil
	case span <= 7*24*time.Hour:
		return "week", nil
	case span <= 31*24*time.Hour:
		return "month", nil
	default:
		return "", nil
	}
}

// BuildURL builds an Ecosia web search URL for the supplied query and
// 0-based page index. q.LangCode is not encoded — Ecosia takes region from
// the ECFG cookie / Accept-Language, set via the browser profile.
func BuildURL(q core.Query, page int) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	params := url.Values{}
	params.Set("method", "index")

	text := q.Text
	if q.Site != "" {
		text += " site:" + q.Site
	}
	if q.Filetype != "" {
		text += " filetype:" + q.Filetype
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("empty query built")
	}
	params.Set("q", text)

	f, err := ecosiaFreshness(q.DateInterval)
	if err != nil {
		return "", err
	}
	if f != "" {
		params.Set("freshness", f)
	}

	if page > 0 {
		params.Set("p", fmt.Sprintf("%d", page))
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

// validImageTypes are the categories Ecosia exposes on /images via the
// imageType URL param; q.Filetype is matched against this set.
var validImageTypes = map[string]struct{}{
	"clipart":     {},
	"photo":       {},
	"line":        {},
	"animatedgif": {},
	"transparent": {},
}

// BuildImageURL builds an Ecosia image search URL for the supplied query
// and 0-based page index. Image search lives on /images, not /search.
// q.Filetype maps to imageType (clipart, photo, line, animatedgif,
// transparent), not a generic file extension.
//
// Of the six image filters Ecosia exposes (Size, Colour, Type, Time, Layouts,
// License), only Type (q.Filetype -> imageType) and Time (q.DateInterval ->
// freshness) are mapped; size/colour/layouts/license are not mapped currently.
func BuildImageURL(q core.Query, page int) (string, error) {
	base, err := url.Parse(imagesURL)
	if err != nil {
		return "", err
	}

	text := q.Text
	if q.Site != "" {
		text += " site:" + q.Site
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("empty query built")
	}

	params := url.Values{}
	params.Set("q", text)

	f, err := ecosiaFreshness(q.DateInterval)
	if err != nil {
		return "", err
	}
	if f != "" {
		params.Set("freshness", f)
	}

	if q.Filetype != "" {
		v := strings.ToLower(strings.TrimSpace(q.Filetype))
		if _, ok := validImageTypes[v]; !ok {
			return "", fmt.Errorf("unsupported imageType %q (want one of: clipart, photo, line, animatedgif, transparent)", q.Filetype)
		}
		params.Set("imageType", v)
	}

	if page > 0 {
		params.Set("p", fmt.Sprintf("%d", page))
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

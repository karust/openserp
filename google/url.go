package google

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

var GoogleDomains = map[string]string{
	"":    "com",
	"en":  "com",
	"ac":  "ac",
	"ad":  "ad",
	"ae":  "ae",
	"af":  "com.af",
	"ag":  "com.ag",
	"ai":  "com.ai",
	"al":  "al",
	"am":  "am",
	"ao":  "co.ao",
	"ar":  "com.ar",
	"as":  "as",
	"at":  "at",
	"au":  "com.au",
	"az":  "az",
	"ba":  "ba",
	"bd":  "com.bd",
	"be":  "be",
	"bf":  "bf",
	"bg":  "bg",
	"bh":  "com.bh",
	"bi":  "bi",
	"bj":  "bj",
	"bn":  "com.bn",
	"bo":  "com.bo",
	"br":  "com.br",
	"bs":  "bs",
	"bt":  "bt",
	"bw":  "co.bw",
	"by":  "by",
	"bz":  "com.bz",
	"ca":  "ca",
	"kh":  "com.kh",
	"cc":  "cc",
	"cd":  "cd",
	"cf":  "cf",
	"cat": "cat",
	"cg":  "cg",
	"ch":  "ch",
	"ci":  "ci",
	"ck":  "co.ck",
	"cl":  "cl",
	"cm":  "cm",
	"cn":  "cn",
	"co":  "com.co",
	"cr":  "co.cr",
	"cu":  "com.cu",
	"cv":  "cv",
	"cy":  "com.cy",
	"cz":  "cz",
	"de":  "de",
	"dj":  "dj",
	"dk":  "dk",
	"dm":  "dm",
	"do":  "com.do",
	"dz":  "dz",
	"ec":  "com.ec",
	"ee":  "ee",
	"eg":  "com.eg",
	"es":  "es",
	"et":  "com.et",
	"fi":  "fi",
	"fj":  "com.fj",
	"fm":  "fm",
	"fr":  "fr",
	"ga":  "ga",
	"gb":  "co.uk",
	"ge":  "ge",
	"gf":  "gf",
	"gg":  "gg",
	"gh":  "com.gh",
	"gi":  "com.gi",
	"gl":  "gl",
	"gm":  "gm",
	"gp":  "gp",
	"gr":  "gr",
	"gt":  "com.gt",
	"gy":  "gy",
	"hk":  "com.hk",
	"hn":  "hn",
	"hr":  "hr",
	"ht":  "ht",
	"hu":  "hu",
	"id":  "co.id",
	"iq":  "iq",
	"ie":  "ie",
	"il":  "co.il",
	"im":  "im",
	"in":  "co.in",
	"io":  "io",
	"is":  "is",
	"it":  "it",
	"je":  "je",
	"jm":  "com.jm",
	"jo":  "jo",
	"jp":  "co.jp",
	"ke":  "co.ke",
	"ki":  "ki",
	"kg":  "kg",
	"kr":  "co.kr",
	"kw":  "com.kw",
	"kz":  "kz",
	"la":  "la",
	"lb":  "com.lb",
	"lc":  "com.lc",
	"li":  "li",
	"lk":  "lk",
	"ls":  "co.ls",
	"lt":  "lt",
	"lu":  "lu",
	"lv":  "lv",
	"ly":  "com.ly",
	"ma":  "co.ma",
	"md":  "md",
	"me":  "me",
	"mg":  "mg",
	"mk":  "mk",
	"ml":  "ml",
	"mm":  "com.mm",
	"mn":  "mn",
	"ms":  "ms",
	"mt":  "com.mt",
	"mu":  "mu",
	"mv":  "mv",
	"mw":  "mw",
	"mx":  "com.mx",
	"my":  "com.my",
	"mz":  "co.mz",
	"na":  "com.na",
	"ne":  "ne",
	"nf":  "com.nf",
	"ng":  "com.ng",
	"ni":  "com.ni",
	"nl":  "nl",
	"no":  "no",
	"np":  "com.np",
	"nr":  "nr",
	"nu":  "nu",
	"nz":  "co.nz",
	"om":  "com.om",
	"pa":  "com.pa",
	"pe":  "com.pe",
	"ph":  "com.ph",
	"pk":  "com.pk",
	"pl":  "pl",
	"pg":  "com.pg",
	"pn":  "pn",
	"pr":  "com.pr",
	"ps":  "ps",
	"pt":  "pt",
	"py":  "com.py",
	"qa":  "com.qa",
	"ro":  "ro",
	"rs":  "rs",
	"ru":  "ru",
	"rw":  "rw",
	"sa":  "com.sa",
	"sb":  "com.sb",
	"sc":  "sc",
	"se":  "se",
	"sg":  "com.sg",
	"sh":  "sh",
	"si":  "si",
	"sk":  "sk",
	"sl":  "com.sl",
	"sn":  "sn",
	"sm":  "sm",
	"so":  "so",
	"st":  "st",
	"sv":  "com.sv",
	"td":  "td",
	"tg":  "tg",
	"th":  "co.th",
	"tj":  "com.tj",
	"tk":  "tk",
	"tl":  "tl",
	"tm":  "tm",
	"to":  "to",
	"tn":  "tn",
	"tr":  "com.tr",
	"tt":  "tt",
	"tw":  "com.tw",
	"tz":  "co.tz",
	"ua":  "com.ua",
	"ug":  "co.ug",
	"uk":  "co.uk",
	"uy":  "com.uy",
	"uz":  "co.uz",
	"vc":  "com.vc",
	"ve":  "co.ve",
	"vg":  "vg",
	"vi":  "co.vi",
	"vn":  "com.vn",
	"vu":  "vu",
	"ws":  "ws",
	"za":  "co.za",
	"zm":  "co.zm",
	"zw":  "co.zw",
}

// Build Google query URL from Query struct
func BuildURL(q core.Query) (string, error) {
	googleBase := GoogleDomains[strings.ToLower(q.LangCode)]
	base, err := url.Parse(fmt.Sprintf("https://www.google.%s", googleBase))
	if err != nil {
		return "", err
	}

	base.Path += "search"
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

		logrus.Tracef("Query text: %s", text)
		params.Add("q", text)
		params.Add("oq", text)
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

		dataParam := fmt.Sprintf("cdr:1,cd_min:%s,cd_max:%s", intervals[0], intervals[1])
		params.Add("tbs", dataParam)
	}

	// Limit number of results
	if q.Limit != 0 {
		params.Add("num", strconv.Itoa(q.Limit))
	}

	if q.LangCode != "" {
		params.Add("hl", q.LangCode)
		params.Add("lr", "lang_"+strings.ToLower(q.LangCode))
	}

	params.Add("pws", "0")  // Do not personalize earch results
	params.Add("nfpr", "1") // Do not auto correct search queries
	params.Add("sourceid", "chrome")
	params.Add("ie", "UTF-8")

	base.RawQuery = params.Encode()
	return base.String(), nil
}

func BuildImageURL(q core.Query) (string, error) {
	// TODO: Add new params
	googleBase := GoogleDomains[strings.ToLower(q.LangCode)]
	base, err := url.Parse(fmt.Sprintf("https://www.google.%s", googleBase))
	if err != nil {
		return "", err
	}

	base.Path += "search"
	params := url.Values{}
	params.Add("tbm", "isch") // Search images

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
		params.Add("oq", text)
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

		dataParam := fmt.Sprintf("cdr:1,cd_min:%s,cd_max:%s", intervals[0], intervals[1])
		params.Add("tbs", dataParam)
	}

	// Limit number of results
	if q.Limit != 0 {
		params.Add("num", strconv.Itoa(q.Limit))
	}

	if q.LangCode != "" {
		params.Add("hl", q.LangCode)
		params.Add("lr", "lang_"+strings.ToLower(q.LangCode))
	}

	params.Add("pws", "0")  // Do not personalize earch results
	params.Add("nfpr", "1") // Do not auto correct search queries

	base.RawQuery = params.Encode()
	return base.String(), nil
}

type SourceImage struct {
	PageURL     string
	OriginalURL string
	Width       string
	Height      string
}

func parseSourceImageURL(href string) (SourceImage, error) {
	source := SourceImage{}

	href = strings.ReplaceAll(href, ";", "&")
	parsed, err := url.QueryUnescape(href)
	if err != nil {
		return source, err
	}

	u, err := url.Parse(parsed)
	if err != nil {
		return source, err
	}

	queryMap, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return source, err
	}

	val, ok := queryMap["h"]
	if ok && len(val) > 0 {
		source.Height = val[0]
	}

	val, ok = queryMap["w"]
	if ok && len(val) > 0 {
		source.Width = val[0]
	}

	val, ok = queryMap["imgrefurl"]
	if ok && len(val) > 0 {
		source.PageURL = val[0]
	}

	val, ok = queryMap["imgurl"]
	if ok && len(val) > 0 {
		source.OriginalURL = val[0]
	}

	return source, nil
}

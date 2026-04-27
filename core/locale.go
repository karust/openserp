package core

import "strings"

// Locale is a parsed language/region pair derived from a BCP47-style code.
// Language is the lowercase 2-letter language subtag (e.g. "en", "de").
// Country is the uppercase 2-letter region subtag (e.g. "US", "DE"); it may be
// empty when the input had no region and the caller did not request a default.
type Locale struct {
	Language string
	Country  string
}

var defaultLocaleCountryByLanguage = map[string]string{
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

// ParseLocale parses a language code such as "en", "EN-us", or "de_AT" into a
// Locale. Returns the zero value when the input is empty or has no language
// subtag. Country is uppercased; Language is lowercased.
func ParseLocale(code string) Locale {
	code = strings.TrimSpace(code)
	if code == "" {
		return Locale{}
	}
	code = strings.ReplaceAll(code, "_", "-")

	parts := strings.Split(code, "-")
	language := strings.ToLower(strings.TrimSpace(parts[0]))
	if language == "" {
		return Locale{}
	}

	country := ""
	if len(parts) > 1 {
		country = strings.ToUpper(strings.TrimSpace(parts[1]))
	}
	return Locale{Language: language, Country: country}
}

// PrimaryLanguageTag returns the BCP47 primary tag for a lang code, filling in
// a default country for bare languages (e.g. "de" -> "de-DE"). Returns "" when
// the input has no language subtag.
func PrimaryLanguageTag(langCode string) string {
	locale := ParseLocale(langCode)
	if locale.Language == "" {
		return ""
	}
	country := locale.Country
	if country == "" {
		country = defaultLocaleCountryByLanguage[locale.Language]
	}
	if country == "" {
		return locale.Language
	}
	return locale.Language + "-" + country
}

// BuildAcceptLanguageHeader formats an Accept-Language value from a lang code.
// Example: "de" -> "de-DE,de;q=0.9", "en-GB" -> "en-GB,en;q=0.9", "sw" -> "sw".
func BuildAcceptLanguageHeader(langCode string) string {
	primary := PrimaryLanguageTag(langCode)
	if primary == "" {
		return ""
	}
	language := ParseLocale(langCode).Language
	if primary == language {
		return language
	}
	return primary + "," + language + ";q=0.9"
}

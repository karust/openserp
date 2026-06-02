package core

import "github.com/karust/openserp/core/region"

// Locale is a parsed language/region pair derived from a BCP47-style code.
// The canonical definition lives in the dependency-free core/region subpackage;
// this alias preserves the historical core.Locale name for existing callers.
type Locale = region.Locale

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
// Locale. See region.ParseLocale for details.
func ParseLocale(code string) Locale {
	return region.ParseLocale(code)
}

// CountryFromRegion extracts a two-letter country/market code from a region
// hint. See region.CountryFromRegion for details.
func CountryFromRegion(hint string) string {
	return region.CountryFromRegion(hint)
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

var defaultTimezoneByCountry = map[string]string{
	"US": "America/New_York", "GB": "Europe/London", "DE": "Europe/Berlin",
	"FR": "Europe/Paris", "ES": "Europe/Madrid", "IT": "Europe/Rome",
	"RU": "Europe/Moscow", "BR": "America/Sao_Paulo", "JP": "Asia/Tokyo",
	"CN": "Asia/Shanghai", "KR": "Asia/Seoul", "IN": "Asia/Kolkata",
	"AU": "Australia/Sydney", "CA": "America/Toronto",
	"MX": "America/Mexico_City", "PL": "Europe/Warsaw",
	"NL": "Europe/Amsterdam", "TR": "Europe/Istanbul",
	"AR": "America/Argentina/Buenos_Aires", "SA": "Asia/Riyadh",
	"BE": "Europe/Brussels", "KZ": "Asia/Almaty", "UA": "Europe/Kyiv",
}

// TimezoneForLocale returns an IANA timezone for a locale's country, or for
// the default country of the language when country is empty. Returns "" for
// unknown locales — caller should retain the existing profile timezone.
func TimezoneForLocale(loc Locale) string {
	if loc.Country != "" {
		if tz, ok := defaultTimezoneByCountry[loc.Country]; ok {
			return tz
		}
	}
	if loc.Language != "" {
		if country, ok := defaultLocaleCountryByLanguage[loc.Language]; ok {
			if tz, ok2 := defaultTimezoneByCountry[country]; ok2 {
				return tz
			}
		}
	}
	return ""
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

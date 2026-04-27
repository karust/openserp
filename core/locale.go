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

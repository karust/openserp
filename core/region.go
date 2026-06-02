package core

import "strings"

// RegionTarget is the resolved, per-engine targeting for a user-supplied region
// hint. Engines read the field relevant to them: Google uses GoogleCanonical to
// build a UULE, Yandex uses YandexLR. Country is the ISO 3166-1 alpha-2 code
// when one could be derived, useful as a coarse market signal.
//
// A field left empty means "no better signal than the raw input" — callers
// should fall back to their previous behavior (e.g. gl= from locale, or
// dropping the parameter entirely). Resolution never fails: an unrecognized
// region simply yields empty engine fields rather than an error.
type RegionTarget struct {
	// Raw is the trimmed original input, preserved for engines that pass it
	// through (e.g. Yandex numeric lr IDs).
	Raw string
	// Country is the ISO 3166-1 alpha-2 code (uppercase) when derivable, else "".
	Country string
	// GoogleCanonical is the exact Google geotargets canonical location name
	// (e.g. "Berlin,Berlin,Germany") suitable for UULE v1 encoding, else "".
	GoogleCanonical string
	// YandexLR is the Yandex lr region ID (e.g. "213"), else "".
	YandexLR string
}

// yandexLRByCountry maps an ISO country code to a Yandex lr region ID. Yandex
// only exposes a limited set of country-level regions; cities require numeric
// lr IDs passed through verbatim.
var yandexLRByCountry = map[string]string{
	"AT": "113", "AU": "211", "BE": "114", "BR": "94", "CA": "95",
	"CH": "126", "DE": "96", "DK": "203", "ES": "204", "FI": "123",
	"FR": "124", "GB": "102", "IE": "10063", "IN": "994", "IT": "205",
	"JP": "137", "KR": "135", "MX": "20271", "NL": "118", "NO": "119",
	"PL": "120", "RU": "225", "SE": "127", "SG": "10105", "TR": "983",
	"UA": "187", "UK": "102", "US": "84", "ZA": "10021",
}

// cityCanonical maps a normalized bare city name to its exact Google geotargets
// canonical name (used to build a UULE). Only UULE-bearing city targeting needs
// a name table — country/state targeting rides on gl= and never needs one.
//
// The list is deliberately small and hand-curated: bare city names are
// ambiguous (e.g. "London" exists in CA/GB/US), so we only auto-resolve a
// prominence list to its "obvious" match. Canonical names below are copied
// verbatim from Google's geotargets data; any other city can still be targeted
// by passing its full canonical name ("City,Region,Country") directly.
var cityCanonical = map[string]string{
	"amsterdam":      "Amsterdam,North Holland,Netherlands",
	"athens":         "Athens,Athens,Attica,Greece",
	"austin":         "Austin,Texas,United States",
	"bangalore":      "Bengaluru,Karnataka,India",
	"barcelona":      "Barcelona,Barcelona,Catalonia,Spain",
	"beijing":        "Beijing,Beijing,China",
	"berlin":         "Berlin,Berlin,Germany",
	"birmingham":     "Birmingham,West Midlands,England,United Kingdom",
	"boston":         "Boston,Massachusetts,United States",
	"brussels":       "Brussels,Brussels,Belgium",
	"buenos aires":   "Buenos Aires,Buenos Aires,Argentina",
	"cairo":          "Cairo,Cairo Governorate,Egypt",
	"chicago":        "Chicago,Illinois,United States",
	"copenhagen":     "Copenhagen,Capital Region of Denmark,Denmark",
	"dallas":         "Dallas,Texas,United States",
	"delhi":          "Delhi,Delhi,India",
	"dubai":          "Dubai,Dubai,United Arab Emirates",
	"dublin":         "Dublin,County Dublin,Ireland",
	"frankfurt":      "Frankfurt am Main,Hessen,Germany",
	"hamburg":        "Hamburg,Hamburg,Germany",
	"helsinki":       "Helsinki,Helsinki,Uusimaa,Finland",
	"hong kong":      "Hong Kong,Hong Kong",
	"istanbul":       "Istanbul,Istanbul,Turkiye",
	"johannesburg":   "Johannesburg,Gauteng,South Africa",
	"kyiv":           "Kyiv,Kyiv city,Ukraine",
	"lisbon":         "Lisbon,Lisbon,Lisbon,Portugal",
	"london":         "London,England,United Kingdom",
	"los angeles":    "Los Angeles,California,United States",
	"lyon":           "Lyon,Auvergne-Rhone-Alpes,France",
	"madrid":         "Madrid,Community of Madrid,Spain",
	"manchester":     "Manchester,England,United Kingdom",
	"marseille":      "Marseille,Provence-Alpes-Cote d'Azur,France",
	"melbourne":      "Melbourne,Victoria,Australia",
	"mexico city":    "Mexico City,Mexico City,Mexico",
	"miami":          "Miami,Florida,United States",
	"milan":          "Milan,Milan,Lombardy,Italy",
	"montreal":       "Montreal,Montreal,Quebec,Canada",
	"moscow":         "Moscow,Moscow,Russia",
	"mumbai":         "Mumbai,Maharashtra,India",
	"munich":         "Munich,Bavaria,Germany",
	"new york":       "New York,New York,United States",
	"osaka":          "Osaka,Osaka,Japan",
	"oslo":           "Oslo,Oslo,Norway",
	"paris":          "Paris,Paris,Ile-de-France,France",
	"prague":         "Prague,Prague,Czechia",
	"rio de janeiro": "Rio de Janeiro,State of Rio de Janeiro,Brazil",
	"rome":           "Rome,Rome,Lazio,Italy",
	"san francisco":  "San Francisco,California,United States",
	"sao paulo":      "Sao Paulo,State of Sao Paulo,Brazil",
	"seattle":        "Seattle,Washington,United States",
	"seoul":          "Seoul,Seoul,South Korea",
	"shanghai":       "Shanghai,Shanghai,China",
	"singapore":      "Singapore,Singapore",
	"stockholm":      "Stockholm,Stockholm County,Sweden",
	"sydney":         "Sydney,New South Wales,Australia",
	"tokyo":          "Tokyo,Tokyo,Japan",
	"toronto":        "Toronto,Toronto,Ontario,Canada",
	"vancouver":      "Vancouver,British Columbia,Canada",
	"vienna":         "Vienna,Vienna,Vienna,Austria",
	"warsaw":         "Warsaw,Warsaw,Masovian Voivodeship,Poland",
	"washington":     "Washington,District of Columbia,United States",
	"zurich":         "Zurich,Zurich,Switzerland",
}

// ResolveRegion turns a free-text region hint into per-engine targeting. It
// never errors: unrecognized input yields a RegionTarget with empty engine
// fields, leaving callers to fall back to their defaults.
//
// Accepted inputs, in priority order:
//   - Numeric (e.g. "213"): a Yandex-native lr ID. Passed through as YandexLR.
//   - A 2-letter country or BCP47-style locale (e.g. "DE", "en-GB"): resolved to
//     a country and its Yandex lr. No Google canonical — country targeting rides
//     on gl=, not UULE.
//   - A bare curated city name (e.g. "Berlin"): resolved to its canonical name.
//   - A full "City,Region,Country" canonical name typed verbatim (>=2 commas):
//     passed through to Google as-is.
func ResolveRegion(region string) RegionTarget {
	region = strings.TrimSpace(region)
	t := RegionTarget{Raw: region}
	if region == "" {
		return t
	}

	// Yandex-native numeric lr IDs: pass through, nothing else to derive.
	if isDigitsOnly(region) {
		t.YandexLR = region
		return t
	}

	// Country / locale code (e.g. "DE", "en-GB"). Country-level targeting is
	// conveyed via Country (Google uses gl=, Yandex the country lr); we
	// deliberately do NOT emit a Google canonical/UULE for a whole country.
	if cc := CountryFromRegion(region); cc != "" {
		t.Country = cc
		t.YandexLR = yandexLRByCountry[cc]
		return t
	}

	// Bare curated city name (e.g. "Berlin", "New York").
	if c := cityCanonical[strings.ToLower(region)]; c != "" {
		t.GoogleCanonical = c
		return t
	}

	// Looks like a full "City,Region,Country" canonical name the caller typed
	// verbatim (>=2 commas). Pass it through to Google as-is; Google ignores it
	// if it isn't a real canonical name, which is the caller's responsibility.
	if strings.Count(region, ",") >= 2 {
		t.GoogleCanonical = region
		return t
	}

	return t
}

func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

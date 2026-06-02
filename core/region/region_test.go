package region

import "testing"

func TestResolveRegion(t *testing.T) {
	tests := []struct {
		name      string
		region    string
		country   string
		canonical string
		lr        string
	}{
		{
			name:      "bare curated city resolves to canonical name",
			region:    "Berlin",
			canonical: "Berlin,Berlin,Germany",
		},
		{
			name:      "curated city is case-insensitive",
			region:    "  berlin ",
			canonical: "Berlin,Berlin,Germany",
		},
		{
			name:      "ambiguous curated city picks obvious match",
			region:    "London",
			canonical: "London,England,United Kingdom",
		},
		{
			name:      "full canonical name passes through verbatim",
			region:    "Smalltown,Some Region,Faraway",
			canonical: "Smalltown,Some Region,Faraway",
		},
		{
			name:    "bare country code sets country and yandex lr but no uule",
			region:  "DE",
			country: "DE",
			lr:      "96",
		},
		{
			name:    "locale code resolves country",
			region:  "en-GB",
			country: "GB",
			lr:      "102",
		},
		{
			name:   "numeric region is a yandex lr passthrough",
			region: "213",
			lr:     "213",
		},
		{
			name:   "empty region resolves to nothing",
			region: "",
		},
		{
			name:   "unknown bare name resolves to nothing",
			region: "Nowhereville",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRegion(tt.region)
			if got.Country != tt.country {
				t.Errorf("Country = %q, want %q", got.Country, tt.country)
			}
			if got.GoogleCanonical != tt.canonical {
				t.Errorf("GoogleCanonical = %q, want %q", got.GoogleCanonical, tt.canonical)
			}
			if got.YandexLR != tt.lr {
				t.Errorf("YandexLR = %q, want %q", got.YandexLR, tt.lr)
			}
		})
	}
}

func TestGoogleUULE(t *testing.T) {
	// A curated bare city must encode its canonical name, not the raw input —
	// Google ignores a UULE whose decoded value is not a real geotargets name.
	if got := GoogleUULE("Berlin"); got != EncodeGoogleUULE("Berlin,Berlin,Germany") {
		t.Errorf("GoogleUULE(Berlin) = %q, want canonical encoding", got)
	}
	// Country codes and numeric lr IDs ride on gl=/lr=, never UULE.
	if got := GoogleUULE("DE"); got != "" {
		t.Errorf("GoogleUULE(DE) = %q, want empty", got)
	}
	if got := GoogleUULE("213"); got != "" {
		t.Errorf("GoogleUULE(213) = %q, want empty", got)
	}
	if got := GoogleUULE(""); got != "" {
		t.Errorf("GoogleUULE(empty) = %q, want empty", got)
	}
}

func TestYandexLR(t *testing.T) {
	cases := map[string]string{
		"213":   "213",
		" 213 ": "213",
		"RU":    "225",
		"de":    "96",
		"en-RU": "225",
		"en_GB": "102",
		"UK":    "102",
		"ZZ":    "",
		"21a":   "",
		"":      "",
	}
	for in, want := range cases {
		if got := YandexLR(in); got != want {
			t.Errorf("YandexLR(%q) = %q, want %q", in, got, want)
		}
	}
}

package core

import "testing"

// Captured from a live Google SERP.
const (
	liveToken    = "CAESaQHrOzAVaaFtQIQslJ-pmur07oz3mqOf5OtCi0pGqJ23iltGt7yILQWsQqXJN_Xt-EWWHqFTloOPdHaaG6szXX7JLUx2dSFKIfx_JJVZi48C2RHEiDtQt-6_sI-N83fCUd4ymUopTuDsJA"
	liveAbsolute = "https://www.google.com/goto?url=" + liveToken
	liveRelative = "/goto?url=" + liveToken
)

func TestIsUsableResultURL(t *testing.T) {
	unusable := map[string]string{
		// A scheme check alone passes the absolute form; a host check alone
		// passes the relative one, which has no host at all.
		"bare token":     liveToken,
		"absolute /goto": liveAbsolute,
		"relative /goto": liveRelative,
		"legacy /url":    "https://www.google.com/url?q=https%3A%2F%2Fexample.com%2F",
		"ccTLD wrapper":  "https://www.google.de/goto?url=" + liveToken,
		"empty":          "",
		"relative path":  "/search?q=x",
		"scheme-less":    "example.com/page",
		"non-http":       "ftp://files.example/x",
	}
	for name, raw := range unusable {
		if IsUsableResultURL(raw) {
			t.Errorf("%s: want unusable, got usable", name)
		}
	}

	usable := []string{
		"https://www.pcmag.com/picks/the-best-vpn-services",
		"https://vpn.techlore.tech/",
		"http://python.berkeley.edu/resources",
		// Google's own pages are legitimate destinations; only its redirect
		// paths are wrappers.
		"https://developers.google.com/edu/python",
		"https://support.google.com/websearch",
	}
	for _, raw := range usable {
		if !IsUsableResultURL(raw) {
			t.Errorf("want usable: %s", raw)
		}
	}
}

func TestIsWrappedGoogleURLSeparatesWrappedFromBroken(t *testing.T) {
	for _, raw := range []string{liveToken, liveAbsolute, liveRelative} {
		if !IsWrappedGoogleURL(raw) {
			t.Errorf("want wrapped: %.50s", raw)
		}
	}
	// Unusable for other reasons — must not be attributed to Google.
	for _, raw := range []string{"", "example.com/page", "ftp://x.example/f", "/search?q=x"} {
		if IsWrappedGoogleURL(raw) {
			t.Errorf("want not-wrapped: %q", raw)
		}
	}
}

func TestDomainFromAttribution(t *testing.T) {
	cases := []struct {
		name       string
		displayURL string
		sourceName string
		want       string
	}{
		// Breadcrumbs, as Google renders them.
		{"breadcrumb with crumbs", "https://www.pcmag.com › ... › Security › VPN", "PCMag", "pcmag.com"},
		{"breadcrumb bare", "https://vpn.techlore.tech", "Techlore", "vpn.techlore.tech"},
		{"breadcrumb one crumb", "https://thebestvpn.com › cheap-vpn", "TheBestVPN.com", "thebestvpn.com"},
		{"http breadcrumb", "http://python.berkeley.edu › resources", "Berkeley", "python.berkeley.edu"},

		// Reddit and video blocks put a comment count where the breadcrumb
		// would be, so the site name is the only attribution left.
		{"reddit falls back to source", "570+ comments  ·  5 years ago", "Reddit · r/VPN", "reddit.com"},
		{"youtube falls back to source", "161.1K+ views  ·  1 month ago", "YouTube · VPNpro", "youtube.com"},
		{"source name is already a domain", "", "TheBestVPN.com", "thebestvpn.com"},

		// Unknown brand: leaving it empty beats guessing a wrong domain.
		{"unknown brand", "", "Privacy Guides", ""},
		{"nothing at all", "", "", ""},
		{"junk breadcrumb only", "570+ comments", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DomainFromAttribution(tc.displayURL, tc.sourceName); got != tc.want {
				t.Errorf("DomainFromAttribution(%q, %q) = %q, want %q",
					tc.displayURL, tc.sourceName, got, tc.want)
			}
		})
	}
}

// A usable URL must keep deriving identity from the URL, unchanged.
func TestEnrichResultPrefersTheRealURL(t *testing.T) {
	raw := SearchResult{
		Rank:  1,
		URL:   "https://www.techradar.com/vpn/best-vpn",
		Title: "The best VPN service 2026",
		// Attribution present but must not override a real destination.
		DisplayURL: "https://www.techradar.com › VPN › VPN Services",
		SourceName: "TechRadar",
	}
	got := EnrichResult(raw, EnrichContext{Engine: "google"})
	if got.Domain != "techradar.com" {
		t.Errorf("domain = %q, want techradar.com", got.Domain)
	}
	if got.URL != "https://www.techradar.com/vpn/best-vpn" {
		t.Errorf("url = %q, want the original", got.URL)
	}
	if got.ID != buildResultID("google", normalizeURL(raw.URL)) {
		t.Error("id should still be keyed on the URL when the URL is real")
	}
}

// A wrapped URL keeps the raw value (we do not invent a destination) but the
// derived fields come from attribution instead of the wrapper.
func TestEnrichResultFallsBackToAttribution(t *testing.T) {
	raw := SearchResult{
		Rank:       1,
		URL:        liveRelative,
		Title:      "The best VPN service 2026",
		DisplayURL: "https://www.techradar.com › VPN › VPN Services",
		SourceName: "TechRadar",
	}
	got := EnrichResult(raw, EnrichContext{Engine: "google"})
	if got.Domain != "techradar.com" {
		t.Errorf("domain = %q, want techradar.com", got.Domain)
	}
	if got.Favicon != "https://techradar.com/favicon.ico" {
		t.Errorf("favicon = %q", got.Favicon)
	}
	// display_url must read the same as it does for a result with a real URL
	// ("techradar.com › vpn › best-vpn"), not carry Google's scheme and www.
	if got.DisplayURL != "techradar.com › VPN › VPN Services" {
		t.Errorf("display_url = %q, want the reshaped breadcrumb", got.DisplayURL)
	}
}

func TestAttributionDisplayURLMatchesTheNormalShape(t *testing.T) {
	cases := []struct {
		displayURL string
		domain     string
		want       string
	}{
		{"https://www.pcmag.com › ... › Security › VPN", "pcmag.com", "pcmag.com › ... › Security › VPN"},
		{"https://thebestvpn.com › cheap-vpn", "thebestvpn.com", "thebestvpn.com › cheap-vpn"},
		{"https://vpn.techlore.tech", "vpn.techlore.tech", "vpn.techlore.tech"},
		// Reddit-style blocks have no breadcrumb; the domain came from the
		// source name and stands alone.
		{"570+ comments  ·  5 years ago", "reddit.com", "reddit.com"},
		{"", "reddit.com", "reddit.com"},
		{"anything", "", ""},
	}
	for _, c := range cases {
		if got := AttributionDisplayURL(c.displayURL, c.domain); got != c.want {
			t.Errorf("AttributionDisplayURL(%q, %q) = %q, want %q",
				c.displayURL, c.domain, got, c.want)
		}
	}
}

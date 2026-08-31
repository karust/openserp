package core

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// Google serves clients it classifies as automated a SERP whose result links
// are replaced by encrypted tokens only Google can decrypt. Three shapes occur,
// and a check that misses any one of them lets a whole page through as garbage:
//
//	CAESTAHrOzAV...                        bare token (no scheme, no host)
//	https://www.google.com/goto?url=CAES…  absolute wrapper
//	/goto?url=CAES…                        relative wrapper, which is what the
//	                                       goquery parser sees, since it reads
//	                                       the raw href attribute
//
// The middle shape is a syntactically valid https URL, so a scheme check alone
// passes it and every result then reports domain "www.google.com" — worse than
// an obvious failure. The relative shape has no host at all, so a host check
// alone misses it too.
//
// IsUsableResultURL is therefore a positive check — "an absolute http(s) URL
// that is not a Google redirect" — and never depends on the token's byte
// signature, so it keeps working when Google rotates the encoding.

var googleRedirectPaths = []string{"/goto", "/url"}

// IsUsableResultURL reports whether raw can serve as a result's destination.
func IsUsableResultURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if u.Hostname() == "" {
		return false
	}
	return !isGoogleRedirectURL(u)
}

// IsWrappedGoogleURL reports whether raw is recognisably a Google link wrapper.
// Narrower than !IsUsableResultURL — a malformed URL is unusable but not
// wrapped — so callers can tell "Google obfuscated this" from "this is junk".
func IsWrappedGoogleURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if looksLikeGoogleLinkToken(raw) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return isGoogleRedirectURL(u)
}

// isGoogleRedirectURL matches the absolute wrapper (host is a Google domain)
// and the relative one (no host, so the path is all there is to go on).
func isGoogleRedirectURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if host != "" && !isGoogleHost(host) {
		return false
	}
	path := strings.TrimSuffix(strings.ToLower(u.Path), "/")
	for _, p := range googleRedirectPaths {
		if path == p {
			return true
		}
	}
	return false
}

// isGoogleHost matches google.com and its ccTLDs (google.co.uk, google.de …).
func isGoogleHost(host string) bool {
	host = strings.TrimPrefix(host, "www.")
	return host == "google.com" || strings.HasPrefix(host, "google.")
}

// The protobuf header every wrapped token has carried so far: field 1 (varint)
// = 1, field 2 (length-delimited), then a constant five-byte header before the
// ciphertext.
var (
	googleLinkTokenField  = []byte{0x08, 0x01, 0x12}
	googleLinkTokenHeader = []byte{0x01, 0xeb, 0x3b, 0x30, 0x15}
)

// looksLikeGoogleLinkToken reports whether raw is a bare encrypted token. This
// is the one signature-based check here, and therefore the only part Google can
// invalidate by rotating their encoding — which is why nothing load-bearing
// depends on it.
func looksLikeGoogleLinkToken(raw string) bool {
	if len(raw) < 40 || strings.ContainsAny(raw, ":/?#") {
		return false
	}
	decoded, err := base64.URLEncoding.DecodeString(raw[:len(raw)/4*4])
	if err != nil || len(decoded) < 9 {
		return false
	}
	if string(decoded[:3]) != string(googleLinkTokenField) {
		return false
	}
	// decoded[3] is field 2's length varint; the constant header follows it.
	return string(decoded[4:9]) == string(googleLinkTokenHeader)
}

// DomainFromAttribution recovers a result's domain from the visible attribution
// Google renders, used when the href itself is an opaque wrapper.
//
// displayURL is the breadcrumb ("https://www.pcmag.com › ... › VPN"); its first
// token is a real absolute URL. sourceName is the site name beside the favicon
// ("PCMag", "Reddit · r/VPN") and only resolves for sites whose brand maps to a
// known domain — enough to cover the Reddit and video blocks that render a
// comment count in place of a breadcrumb.
func DomainFromAttribution(displayURL, sourceName string) string {
	if domain := domainFromBreadcrumb(displayURL); domain != "" {
		return domain
	}
	return domainFromSourceName(sourceName)
}

// AttributionDisplayURL reshapes an engine breadcrumb into the same form
// buildDisplayURL produces for a real URL ("techradar.com › vpn › best-vpn").
// Google renders its own breadcrumb with a scheme and www ("https://www.
// techradar.com › VPN › VPN Services"); emitting that verbatim would put two
// different display_url shapes in one result list.
func AttributionDisplayURL(displayURL, domain string) string {
	if domain == "" {
		return ""
	}
	crumbs := []string{}
	for i, part := range strings.Split(displayURL, "›") {
		if i == 0 {
			continue // the leading token is the URL, replaced by domain
		}
		if part = strings.TrimSpace(part); part != "" {
			crumbs = append(crumbs, part)
		}
	}
	if len(crumbs) == 0 {
		return domain
	}
	return domain + " › " + strings.Join(crumbs, " › ")
}

// domainFromBreadcrumb takes the leading absolute URL out of a breadcrumb.
func domainFromBreadcrumb(displayURL string) string {
	head := strings.TrimSpace(displayURL)
	if head == "" {
		return ""
	}
	// The breadcrumb is "<url> › crumb › crumb"; cut at the first separator or
	// whitespace, whichever comes first.
	if idx := strings.IndexAny(head, " \t ›"); idx >= 0 {
		head = head[:idx]
	}
	if !strings.HasPrefix(head, "http://") && !strings.HasPrefix(head, "https://") {
		return ""
	}
	return extractDomain(head)
}

// brandDomains maps the site names Google shows for blocks that render no
// breadcrumb. Deliberately short: a wrong guess here would attribute a result
// to a domain it does not belong to, which is worse than leaving it empty.
var brandDomains = map[string]string{
	"reddit":               "reddit.com",
	"youtube":              "youtube.com",
	"linkedin":             "linkedin.com",
	"facebook":             "facebook.com",
	"instagram":            "instagram.com",
	"x (formerly twitter)": "x.com",
	"quora":                "quora.com",
	"medium":               "medium.com",
	"github":               "github.com",
	"wikipedia":            "wikipedia.org",
	"tiktok":               "tiktok.com",
	"pinterest":            "pinterest.com",
	"amazon":               "amazon.com",
	"stack overflow":       "stackoverflow.com",
}

// domainFromSourceName resolves "Reddit · r/VPN" or "YouTube · Channel" to a
// domain. The part before the separator is the site; anything after it is the
// channel or subreddit and is ignored.
func domainFromSourceName(sourceName string) string {
	name := strings.TrimSpace(sourceName)
	if name == "" {
		return ""
	}
	if idx := strings.Index(name, "·"); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if domain, ok := brandDomains[name]; ok {
		return domain
	}
	// Names like "TheBestVPN.com" are already a domain.
	if strings.Contains(name, ".") && !strings.ContainsAny(name, " /") {
		return strings.TrimPrefix(name, "www.")
	}
	return ""
}

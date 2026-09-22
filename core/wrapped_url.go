package core

import (
	"net/url"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var attributionIDNA = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.VerifyDNSLength(true))

// IsUsableResultURL reports whether raw can serve as a result's destination.
func IsUsableResultURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
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

// IsWrappedGoogleURL recognizes redirect paths, not opaque token signatures.
func IsWrappedGoogleURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && isGoogleRedirectURL(u)
}

// isGoogleRedirectURL matches the absolute wrapper (host is a Google domain)
// and the relative one (no host, so the path is all there is to go on).
func isGoogleRedirectURL(u *url.URL) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	path := strings.TrimSuffix(strings.ToLower(u.Path), "/")
	if host == "googleadservices.com" || host == "www.googleadservices.com" {
		return path == "/pagead/aclk"
	}
	if host != "" && !isGoogleHost(host) {
		return false
	}
	switch path {
	case "/goto", "/url", "/aclk", "/pagead/aclk":
		return true
	}
	return false
}

// Require google directly under a public suffix, not google.example.com.
func isGoogleHost(host string) bool {
	host = strings.TrimPrefix(host, "www.")
	suffix, icann := publicsuffix.PublicSuffix(host)
	return icann && host == "google."+suffix
}

// DomainFromAttribution uses the breadcrumb hostname, then the visible site name.
func DomainFromAttribution(displayURL, sourceName string) string {
	if domain := domainFromBreadcrumb(displayURL); domain != "" {
		return domain
	}
	return domainFromSourceName(sourceName)
}

// AttributionDisplayURL formats visible attribution as "domain › path".
func AttributionDisplayURL(displayURL, domain string) string {
	if domain == "" {
		return ""
	}
	head, tail, hasCrumbs := strings.Cut(NormalizeWhitespace(displayURL), "›")
	if !hasCrumbs {
		if domainFromBreadcrumb(head) == "" {
			return domain
		}
		if !strings.Contains(head, "://") {
			head = "//" + strings.TrimPrefix(head, "//")
		}
		return buildDisplayURL(head, domain)
	}
	crumbs := []string{domain}
	for _, part := range strings.Split(tail, "›") {
		if part = strings.TrimSpace(part); part != "" {
			crumbs = append(crumbs, part)
		}
	}
	return strings.Join(crumbs, " › ")
}

// domainFromBreadcrumb takes the leading URL or hostname out of a breadcrumb.
func domainFromBreadcrumb(displayURL string) string {
	head := NormalizeWhitespace(displayURL)
	if idx := strings.IndexAny(head, " ›"); idx >= 0 {
		head = head[:idx]
	}
	return attributionDomain(head)
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
	name, _, _ := strings.Cut(sourceName, "·")
	name = strings.ToLower(NormalizeWhitespace(name))
	if domain, ok := brandDomains[name]; ok {
		return domain
	}
	// Names like "TheBestVPN.com" are already a domain.
	if strings.Contains(name, ".") && !strings.ContainsAny(name, " /") {
		return attributionDomain(name)
	}
	return ""
}

// Attribution is display text, so validate it before using it as a hostname.
func attributionDomain(raw string) string {
	if !strings.Contains(raw, "://") {
		raw = "//" + strings.TrimPrefix(raw, "//")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host, err := attributionIDNA.ToASCII(u.Hostname())
	if err != nil {
		return ""
	}
	host = strings.TrimSuffix(host, ".")
	if !strings.Contains(host, ".") {
		return ""
	}
	// Counts such as "161.1K views" are not domains. Check the TLD only,
	// so hosts on private suffixes such as github.io still work.
	tld := host[strings.LastIndexByte(host, '.')+1:]
	if _, icann := publicsuffix.PublicSuffix(tld); !icann {
		return ""
	}
	return strings.TrimPrefix(host, "www.")
}

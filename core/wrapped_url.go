package core

import (
	"encoding/base64"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var attributionIDNA = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.VerifyDNSLength(true))

// IsUsableResultURL reports whether raw can serve as a result's destination.
func IsUsableResultURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && isUsableResultURL(u)
}

func isUsableResultURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") &&
		u.Hostname() != "" && !isGoogleRedirectURL(u) && !isBingClickURL(u) && !isBingVideoPlayerURL(u)
}

const bingVideoPlayerPath = "/videos/riverview/relatedvideo"

func isBingHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "bing.com" || strings.HasSuffix(host, ".bing.com")
}

func isBingClickURL(u *url.URL) bool {
	return u.Path == "/ck/a" && (u.Hostname() == "" || isBingHost(u.Hostname()))
}

func isBingVideoPlayerURL(u *url.URL) bool {
	return u.Path == bingVideoPlayerPath && isBingHost(u.Hostname())
}

// UnwrapBingURL returns the publisher destination encoded in a Bing click URL.
func UnwrapBingURL(raw string) string {
	wrapper, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	_, publisher := bingResultURL(wrapper)
	return publisher
}

// BingResultURL unwraps a Bing result, including links to its video player.
func BingResultURL(raw string) string {
	link, err := url.Parse(raw)
	if err == nil {
		if target, publisher := bingResultURL(link); target != nil {
			if publisher != "" {
				return publisher
			}
			return target.String()
		}
	}
	return raw
}

func bingResultURL(link *url.URL) (*url.URL, string) {
	target := link
	if isBingClickURL(link) {
		token, ok := strings.CutPrefix(link.Query().Get("u"), "a1")
		if !ok {
			return nil, ""
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
		if err != nil {
			return nil, ""
		}
		publisher := strings.TrimSpace(string(decoded))
		target, err = url.Parse(publisher)
		if err != nil {
			return nil, ""
		}
		if isUsableResultURL(target) {
			return target, publisher
		}
	} else if !isBingVideoPlayerURL(link) {
		return nil, ""
	}

	// Some video rows provide a Bing player as their only click target.
	if target.Path != bingVideoPlayerPath || target.User != nil {
		return nil, ""
	}
	if target.Scheme == "" && target.Host == "" {
		target.Host = link.Hostname()
		if target.Host == "" {
			target.Host = "www.bing.com"
		}
	}
	if !isBingHost(target.Hostname()) || (target.Scheme != "" && target.Scheme != "https" && target.Scheme != "http") {
		return nil, ""
	}
	query := target.Query()
	if query.Get("mid") == "" {
		return nil, ""
	}
	if target.Scheme == "" {
		target.Scheme = "https"
	}
	query.Del("msockid")
	target.RawQuery = query.Encode()
	return target, ""
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

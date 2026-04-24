package core

import (
	"strings"
)

// EnrichDomainInfo derives TLD/category signals from a bare hostname.
func EnrichDomainInfo(domain string) *DomainInfo {
	if domain == "" {
		return nil
	}

	tld, sld := splitDomain(domain)

	info := &DomainInfo{
		TLD:           tld,
		SLD:           sld,
		IsGov:         isGovTLD(domain, tld),
		IsEdu:         isEduTLD(domain, tld),
		IsMil:         isMilTLD(tld),
		IsNews:        newsDomains[domain],
		IsForum:       forumDomains[domain],
		IsMarketplace: marketplaceDomains[domain],
		IsSocial:      socialDomains[domain],
	}
	return info
}

// ClassifyURL returns a rough content-type and source hint derived from the
// URL path alone — no network calls.
func ClassifyURL(rawURL, domain string) *Classification {
	if rawURL == "" && domain == "" {
		return nil
	}

	contentType := classifyContentType(rawURL)
	sourceHint := classifySourceHint(domain)

	return &Classification{
		ContentType: contentType,
		SourceHint:  sourceHint,
	}
}

// splitDomain returns (tld, sld) for a bare hostname.
// Uses a simple heuristic: last label is TLD, second-to-last is SLD.
// For compound TLDs like co.uk the full suffix is returned as TLD.
func splitDomain(domain string) (tld, sld string) {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain, ""
	}
	// Known compound TLDs.
	compoundTLDs := map[string]bool{
		"co.uk": true, "co.jp": true, "co.in": true, "co.nz": true,
		"co.za": true, "com.au": true, "com.br": true, "com.mx": true,
		"gov.uk": true, "ac.uk": true, "edu.au": true, "gov.au": true,
		"or.jp": true, "ne.jp": true,
	}
	if len(parts) >= 3 {
		compound := parts[len(parts)-2] + "." + parts[len(parts)-1]
		if compoundTLDs[compound] {
			return compound, parts[len(parts)-3]
		}
	}
	return parts[len(parts)-1], parts[len(parts)-2]
}

func isGovTLD(domain, tld string) bool {
	if tld == "gov" || tld == "gov.uk" || tld == "gov.au" {
		return true
	}
	return strings.HasSuffix(domain, ".gov") ||
		strings.HasSuffix(domain, ".gov.uk") ||
		strings.HasSuffix(domain, ".gov.au")
}

func isEduTLD(domain, tld string) bool {
	if tld == "edu" || tld == "ac.uk" || tld == "edu.au" {
		return true
	}
	return strings.HasSuffix(domain, ".edu") ||
		strings.HasSuffix(domain, ".ac.uk") ||
		strings.HasSuffix(domain, ".edu.au")
}

func isMilTLD(tld string) bool {
	return tld == "mil"
}

func classifyContentType(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "/wiki/"):
		return "article"
	case strings.HasSuffix(lower, ".pdf") || strings.Contains(lower, ".pdf?"):
		return "document"
	case strings.Contains(lower, "/watch?v=") || strings.Contains(lower, "/video/") ||
		strings.Contains(lower, "/videos/"):
		return "video"
	case strings.Contains(lower, "/forum/") || strings.Contains(lower, "/thread/") ||
		strings.Contains(lower, "/discussion/") || strings.Contains(lower, "/t/") ||
		strings.Contains(lower, "/questions/") || strings.Contains(lower, "/q/"):
		return "forum_thread"
	case strings.Contains(lower, "/blog/") || strings.Contains(lower, "/post/") ||
		strings.Contains(lower, "/article/") || strings.Contains(lower, "/news/"):
		return "article"
	default:
		return "webpage"
	}
}

func classifySourceHint(domain string) string {
	if hint, ok := domainSourceHints[domain]; ok {
		return hint
	}
	return ""
}

// domainSourceHints maps known domains to a descriptive source hint.
var domainSourceHints = map[string]string{
	"wikipedia.org":      "encyclopedia",
	"en.wikipedia.org":   "encyclopedia",
	"github.com":         "code_repository",
	"gitlab.com":         "code_repository",
	"stackoverflow.com":  "qa_forum",
	"stackexchange.com":  "qa_forum",
	"reddit.com":         "social_forum",
	"nytimes.com":        "news",
	"bbc.com":            "news",
	"bbc.co.uk":          "news",
	"reuters.com":        "news",
	"theguardian.com":    "news",
	"washingtonpost.com": "news",
	"forbes.com":         "news",
	"techcrunch.com":     "news",
	"medium.com":         "blog_platform",
	"scholar.google.com": "academic",
	"arxiv.org":          "academic",
	"pubmed.ncbi.nlm.nih.gov": "academic",
	"amazon.com":         "marketplace",
	"ebay.com":           "marketplace",
	"etsy.com":           "marketplace",
	"docs.google.com":    "document",
	"youtube.com":        "video_platform",
	"vimeo.com":          "video_platform",
	"twitter.com":        "social_media",
	"x.com":              "social_media",
	"facebook.com":       "social_media",
	"linkedin.com":       "professional_network",
	"instagram.com":      "social_media",
}

// newsDomains is the set of known news publisher domains.
var newsDomains = map[string]bool{
	"nytimes.com": true, "bbc.com": true, "bbc.co.uk": true,
	"reuters.com": true, "apnews.com": true, "theguardian.com": true,
	"washingtonpost.com": true, "forbes.com": true, "techcrunch.com": true,
	"wired.com": true, "bloomberg.com": true, "cnn.com": true,
	"nbcnews.com": true, "cbsnews.com": true, "abcnews.go.com": true,
	"foxnews.com": true, "theverge.com": true, "engadget.com": true,
	"arstechnica.com": true, "zdnet.com": true, "venturebeat.com": true,
	"axios.com": true, "politico.com": true, "theatlantic.com": true,
	"economist.com": true, "ft.com": true, "wsj.com": true,
	"usatoday.com": true, "latimes.com": true, "nypost.com": true,
}

// forumDomains is the set of known community/forum domains.
var forumDomains = map[string]bool{
	"reddit.com": true, "news.ycombinator.com": true,
	"stackoverflow.com": true, "stackexchange.com": true,
	"superuser.com": true, "serverfault.com": true,
	"quora.com": true, "discourse.org": true,
	"boards.4chan.org": true, "hackernews.com": true,
}

// marketplaceDomains is the set of known e-commerce/marketplace domains.
var marketplaceDomains = map[string]bool{
	"amazon.com": true, "amazon.co.uk": true, "amazon.de": true,
	"ebay.com": true, "etsy.com": true, "walmart.com": true,
	"target.com": true, "bestbuy.com": true, "newegg.com": true,
	"aliexpress.com": true, "alibaba.com": true, "shopify.com": true,
}

// socialDomains is the set of known social media platform domains.
var socialDomains = map[string]bool{
	"twitter.com": true, "x.com": true, "facebook.com": true,
	"instagram.com": true, "tiktok.com": true, "snapchat.com": true,
	"pinterest.com": true, "tumblr.com": true, "linkedin.com": true,
	"youtube.com": true, "twitch.tv": true, "discord.com": true,
}

package core

import (
	_ "embed"
	"os"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"
	"gopkg.in/yaml.v3"
)

//go:embed enrichment_domains.yaml
var defaultEnrichmentDomainsYAML []byte

type enrichmentDomainsFile struct {
	DomainSourceHints  map[string]string `yaml:"domain_source_hints"`
	NewsDomains        []string          `yaml:"news_domains"`
	ForumDomains       []string          `yaml:"forum_domains"`
	MarketplaceDomains []string          `yaml:"marketplace_domains"`
	SocialDomains      []string          `yaml:"social_domains"`
}

type enrichmentDomainsConfig struct {
	DomainSourceHints  map[string]string
	NewsDomains        map[string]bool
	ForumDomains       map[string]bool
	MarketplaceDomains map[string]bool
	SocialDomains      map[string]bool
}

var (
	enrichmentDomainsOnce sync.Once
	enrichmentDomains     enrichmentDomainsConfig
)

// EnrichDomainInfo derives TLD/category signals from a bare hostname.
func EnrichDomainInfo(domain string) *DomainInfo {
	if domain == "" {
		return nil
	}

	domain = normalizeDomain(domain)
	tld, sld := splitDomain(domain)
	cfg := loadEnrichmentDomains()

	info := &DomainInfo{
		TLD:      tld,
		SLD:      sld,
		Category: domainCategory(domain, tld, cfg),
	}
	return info
}

// ClassifyURL returns a rough content-type and source hint derived from the
// URL path alone; no network calls.
func ClassifyURL(rawURL, domain string) *Classification {
	if rawURL == "" && domain == "" {
		return nil
	}

	contentType := classifyContentType(rawURL)
	sourceHint := classifySourceHint(domain)
	if contentType == "webpage" && sourceHint == "" {
		return nil
	}

	return &Classification{
		ContentType: contentType,
		SourceHint:  sourceHint,
	}
}

func domainCategory(domain, tld string, cfg enrichmentDomainsConfig) string {
	switch {
	case isGovTLD(domain, tld):
		return "gov"
	case isEduTLD(domain, tld):
		return "edu"
	case isMilTLD(tld):
		return "mil"
	case cfg.NewsDomains[domain]:
		return "news"
	case cfg.ForumDomains[domain]:
		return "forum"
	case cfg.MarketplaceDomains[domain]:
		return "marketplace"
	case cfg.SocialDomains[domain]:
		return "social"
	default:
		return ""
	}
}

// splitDomain returns (public suffix, registrable domain label).
func splitDomain(domain string) (tld, sld string) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return "", ""
	}

	suffix, icann := publicsuffix.PublicSuffix(domain)
	if suffix == "" || !icann {
		parts := strings.Split(domain, ".")
		if len(parts) < 2 {
			return domain, ""
		}
		return parts[len(parts)-1], parts[len(parts)-2]
	}

	registrable, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		parts := strings.Split(domain, ".")
		if len(parts) < 2 {
			return suffix, ""
		}
		return suffix, parts[len(parts)-2]
	}

	sld = strings.TrimSuffix(registrable, "."+suffix)
	return suffix, sld
}

func isGovTLD(domain, tld string) bool {
	return tld == "gov" || strings.HasSuffix(tld, ".gov") || strings.HasSuffix(domain, ".gov")
}

func isEduTLD(domain, tld string) bool {
	return tld == "edu" || strings.HasSuffix(tld, ".edu") || tld == "ac.uk" ||
		strings.HasSuffix(domain, ".edu") || strings.HasSuffix(domain, ".ac.uk")
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
	cfg := loadEnrichmentDomains()
	if hint, ok := cfg.DomainSourceHints[normalizeDomain(domain)]; ok {
		return hint
	}
	return ""
}

func loadEnrichmentDomains() enrichmentDomainsConfig {
	enrichmentDomainsOnce.Do(func() {
		enrichmentDomains = parseEnrichmentDomains(defaultEnrichmentDomainsYAML)
		if path := strings.TrimSpace(os.Getenv("OPENSERP_ENRICHMENT_DOMAINS_FILE")); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				enrichmentDomains = parseEnrichmentDomains(data)
			}
		}
	})
	return enrichmentDomains
}

func parseEnrichmentDomains(data []byte) enrichmentDomainsConfig {
	cfg := enrichmentDomainsConfig{
		DomainSourceHints:  map[string]string{},
		NewsDomains:        map[string]bool{},
		ForumDomains:       map[string]bool{},
		MarketplaceDomains: map[string]bool{},
		SocialDomains:      map[string]bool{},
	}

	var file enrichmentDomainsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return cfg
	}

	for domain, hint := range file.DomainSourceHints {
		domain = normalizeDomain(domain)
		hint = strings.TrimSpace(hint)
		if domain != "" && hint != "" {
			cfg.DomainSourceHints[domain] = hint
		}
	}
	fillDomainSet(cfg.NewsDomains, file.NewsDomains)
	fillDomainSet(cfg.ForumDomains, file.ForumDomains)
	fillDomainSet(cfg.MarketplaceDomains, file.MarketplaceDomains)
	fillDomainSet(cfg.SocialDomains, file.SocialDomains)

	return cfg
}

func fillDomainSet(dst map[string]bool, domains []string) {
	for _, domain := range domains {
		domain = normalizeDomain(domain)
		if domain != "" {
			dst[domain] = true
		}
	}
}

func normalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, "www.")
	return strings.TrimSuffix(domain, ".")
}

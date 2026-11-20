package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

// URLResolveConfig sets Google redirect resolution defaults and limits.
type URLResolveConfig struct {
	URLs        string        `mapstructure:"resolve_urls"`
	Concurrency int           `mapstructure:"resolve_concurrency"`
	Timeout     time.Duration `mapstructure:"resolve_timeout"`
	Cooldown    time.Duration `mapstructure:"resolve_cooldown"`
}

func DefaultURLResolveConfig() URLResolveConfig {
	return URLResolveConfig{
		URLs: "true",
		// A resolve reads only Google's redirect responses; the destination page
		// is never fetched.
		Concurrency: 4,
		Timeout:     5 * time.Second,
		// The same exit serves the SERPs, so back off once Google pushes back on
		// resolves instead of burning the IP.
		Cooldown: 10 * time.Minute,
	}
}

var errResolveChallenged = errors.New("redirect resolve challenged")

const maxGoogleRedirectRequests = 3
const maxGoogleRedirectBodyBytes = 64 * 1024

// fetchRedirect is swapped in tests.
var fetchRedirect = rawClickRequest

var resolveCooldowns = struct {
	sync.Mutex
	until map[string]time.Time
}{until: map[string]time.Time{}}

// ResolveWrappedResultURLs replaces opaque Google redirects with destinations.
// Failed resolutions keep the wrapper for attribution fallback.
func ResolveWrappedResultURLs(ctx context.Context, results []SearchResult, query Query, pageURL string) {
	if !query.ResolveURLs || len(results) == 0 {
		return
	}
	cfg := DefaultURLResolveConfig()
	if custom := query.ResolveConfig; custom != nil {
		if custom.Concurrency > 0 {
			cfg.Concurrency = custom.Concurrency
		}
		if custom.Timeout > 0 {
			cfg.Timeout = custom.Timeout
		}
		if custom.Cooldown > 0 {
			cfg.Cooldown = custom.Cooldown
		}
	}
	type target struct {
		idx int
		url string
	}
	var targets []target
	organic := 0
	for i, r := range results {
		if r.Ad {
			continue
		}
		if r.Rank > 0 {
			organic++
		}
		if query.ResolveTop > 0 && (r.Rank <= 0 || organic > query.ResolveTop) {
			continue
		}
		if wrapped, ok := resolvableGoogleRedirect(r.URL, pageURL); ok {
			targets = append(targets, target{idx: i, url: wrapped})
		}
	}
	if len(targets) == 0 {
		return
	}

	logger := WithRequest(ctx)
	exit := query.ProxyURL
	if resolveCoolingDown(exit, time.Now()) {
		logger.Debug("Skipping wrapped link resolve: exit is cooling down")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	var (
		wg       sync.WaitGroup
		resolved atomic.Int32
	)
	sem := make(chan struct{}, cfg.Concurrency)
resolveLoop:
	for _, t := range targets {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break resolveLoop
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ownsSlot := true
			defer func() {
				if ownsSlot {
					<-sem
				}
			}()
			wait := func(ctx context.Context, delay time.Duration) error {
				// A refresh wait should not keep other links from starting.
				<-sem
				ownsSlot = false
				if err := SleepContext(ctx, delay); err != nil {
					return err
				}
				select {
				case sem <- struct{}{}:
					ownsSlot = true
				case <-ctx.Done():
				}
				return ctx.Err()
			}

			dest, err := resolveGoogleRedirect(ctx, t.url, query, wait)
			if errors.Is(err, errResolveChallenged) {
				startResolveCooldown(exit, time.Now(), cfg.Cooldown)
				cancel()
				return
			}
			if err != nil {
				logger.WithError(err).Trace("Wrapped link resolve failed")
				return
			}
			results[t.idx].URL = dest
			resolved.Add(1)
		}()
	}
	wg.Wait()

	logger.WithFields(logrus.Fields{
		"resolved": resolved.Load(),
		"wrapped":  len(targets),
	}).Debug("Resolved wrapped result links")
}

// resolvableGoogleRedirect returns the absolute /goto or /url link worth a
// network round trip. Plaintext redirects are unwrapped by normalizeURL, and
// ad clicks are skipped: fetching them bills the advertiser.
func resolvableGoogleRedirect(raw, pageURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !isGoogleResultRedirect(u) {
		return "", false
	}
	if IsUsableResultURL(normalizeURL(raw)) {
		return "", false
	}
	if !u.IsAbs() {
		base, err := validateHTTPURL(pageURL)
		if err != nil || !isGoogleHost(strings.ToLower(base.Hostname())) {
			base, _ = url.Parse("https://www.google.com/")
		}
		u = base.ResolveReference(u)
	}
	if u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return u.String(), true
}

func isGoogleResultRedirect(u *url.URL) bool {
	if !isGoogleRedirectURL(u) {
		return false
	}
	switch strings.TrimSuffix(strings.ToLower(u.Path), "/") {
	case "/goto", "/url":
		return true
	}
	return false
}

func resolveGoogleRedirect(ctx context.Context, wrapped string, query Query, wait func(context.Context, time.Duration) error) (string, error) {
	current := wrapped
	var delay time.Duration
	for range maxGoogleRedirectRequests {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if delay > 0 {
			if deadline, ok := ctx.Deadline(); !ok || delay >= time.Until(deadline) {
				return "", fmt.Errorf("resolve: Google refresh delay %s exceeds remaining budget", delay)
			}
			if err := wait(ctx, delay); err != nil {
				return "", err
			}
		}
		resp, err := fetchRedirect(ctx, current, query)
		if err != nil {
			return "", err
		}

		status := resp.StatusCode
		if status == http.StatusForbidden || status == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			return "", errResolveChallenged
		}
		location := redirectLocation(resp)
		delay = 0
		if status == http.StatusOK {
			location, delay = googleRefreshLocation(resp)
		} else {
			// Keep small redirect connections reusable without draining large errors.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxGoogleRedirectBodyBytes))
		}
		_ = resp.Body.Close()
		if location == "" {
			return "", fmt.Errorf("resolve: HTTP %d without redirect", status)
		}
		u, err := resolveRedirectURL(current, location)
		if err != nil {
			return "", err
		}
		if isGoogleResultRedirect(u) {
			if plain, err := url.Parse(normalizeURL(u.String())); err == nil && isUsableResultURL(plain) {
				u = plain
			}
		}
		if isGoogleInterstitial(u) {
			return "", errResolveChallenged
		}
		dest := u.String()
		if isUsableResultURL(u) {
			return stripTextFragmentDirective(dest), nil
		}

		// Google can answer with an HTML refresh to a fresh wrapper. Only follow
		// Google wrappers; an external destination is returned without fetching it.
		next, ok := resolvableGoogleRedirect(dest, current)
		if !ok {
			return "", fmt.Errorf("resolve: %q is not a destination", dest)
		}
		current = next
	}
	return "", errors.New("resolve: too many Google redirects")
}

func googleRefreshLocation(resp *http.Response) (string, time.Duration) {
	// Stop at the refresh tag; the remaining body may be large or stall.
	z := html.NewTokenizer(io.LimitReader(resp.Body, maxGoogleRedirectBodyBytes))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return "", 0
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data != "meta" {
				continue
			}
			var equiv, content string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "http-equiv":
					equiv = attr.Val
				case "content":
					content = attr.Val
				}
			}
			if strings.EqualFold(strings.TrimSpace(equiv), "refresh") {
				if location, delay := parseMetaRefreshLocation(content); location != "" {
					return location, delay
				}
			}
		}
	}
}

func parseMetaRefreshLocation(content string) (string, time.Duration) {
	target := strings.TrimSpace(content)
	var delay time.Duration
	if seconds, rest, found := strings.Cut(target, ";"); found {
		parsed, err := time.ParseDuration(strings.TrimSpace(seconds) + "s")
		if err != nil || parsed < 0 {
			return "", 0
		}
		delay = parsed
		target = strings.TrimSpace(rest)
	}
	key, value, found := strings.Cut(target, "=")
	if !found || !strings.EqualFold(strings.TrimSpace(key), "url") {
		return "", 0
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') ||
		(value[0] == '"' && value[len(value)-1] == '"')) {
		value = value[1 : len(value)-1]
	}
	return strings.TrimSpace(value), delay
}

// isGoogleInterstitial catches captcha and consent hops, which would otherwise
// pass as Google-owned destinations.
func isGoogleInterstitial(u *url.URL) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if consent, ok := strings.CutPrefix(host, "consent."); ok && isGoogleHost(consent) {
		return true
	}
	return isGoogleHost(host) && strings.HasPrefix(strings.ToLower(u.Path), "/sorry")
}

// Google appends #:~:text= highlight directives; they are not part of the
// address. A real fragment in front of the directive stays.
func stripTextFragmentDirective(raw string) string {
	hash := strings.IndexByte(raw, '#')
	if hash < 0 {
		return raw
	}
	directive := strings.Index(raw[hash:], ":~:")
	if directive < 0 {
		return raw
	}
	if directive == 1 {
		return raw[:hash]
	}
	return raw[:hash+directive]
}

func resolveCoolingDown(exit string, now time.Time) bool {
	resolveCooldowns.Lock()
	defer resolveCooldowns.Unlock()
	for key, until := range resolveCooldowns.until {
		if !now.Before(until) {
			delete(resolveCooldowns.until, key)
		}
	}
	_, cooling := resolveCooldowns.until[exit]
	return cooling
}

func startResolveCooldown(exit string, now time.Time, cooldown time.Duration) {
	resolveCooldowns.Lock()
	_, already := resolveCooldowns.until[exit]
	resolveCooldowns.until[exit] = now.Add(cooldown)
	resolveCooldowns.Unlock()
	if already {
		return
	}
	label := "direct"
	if exit != "" {
		label = MaskProxyURL(exit)
	}
	logrus.WithField("exit", label).Warnf("Google challenged wrapped link resolves, pausing them for %s", cooldown)
}

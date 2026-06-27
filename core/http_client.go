package core

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	browserprofile "github.com/karust/openserp/core/browser"
)

const rawHTTPTimeout = 30 * time.Second
const rawHTTPClientCacheMaxEntries = 64

// fallbackRawUserAgent guards against tls-client's "Go-http-client" UA leaking.
const fallbackRawUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

// rawChromeProfiles pairs each TLS fingerprint with its Chrome major so the UA
// and Sec-CH-UA stay coherent. We round-robin the major here. Add presets as
// tls-client ships them.
var rawChromeProfiles = []struct {
	major int
	tls   profiles.ClientProfile
}{
	{133, profiles.Chrome_133},
	{144, profiles.Chrome_144},
	{146, profiles.Chrome_146},
}

// pickRawChromeProfile hashes salt to a stable but varied fingerprint.
func pickRawChromeProfile(salt string) (int, profiles.ClientProfile) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(salt))
	p := rawChromeProfiles[int(h.Sum32())%len(rawChromeProfiles)]
	return p.major, p.tls
}

// rawHeaderOrder controls request header order; tls-client profiles do not.
var rawHeaderOrder = []string{
	"host",
	"user-agent",
	"accept",
	"accept-language",
	"accept-encoding",
	"upgrade-insecure-requests",
	"sec-ch-ua",
	"sec-ch-ua-mobile",
	"sec-ch-ua-platform",
	"sec-fetch-site",
	"sec-fetch-mode",
	"sec-fetch-user",
	"sec-fetch-dest",
}

var rawHTTPClientCache = struct {
	sync.Mutex
	clients map[rawHTTPClientKey]*rawHTTPClientEntry
}{
	clients: map[rawHTTPClientKey]*rawHTTPClientEntry{},
}

type rawHTTPClientKey struct {
	proxyURL             string
	profile              string
	insecure             bool
	guardPrivateNetworks bool
}

type rawHTTPClientEntry struct {
	client   tlsclient.HttpClient
	lastUsed time.Time
}

type rawRequestProfile struct {
	id             string
	userAgent      string
	acceptLanguage string
	secCHUA        string
	platform       string
	mobile         bool
	tlsProfile     profiles.ClientProfile
}

// DrainAndCloseResponse drains then closes the body so the connection can be reused.
func DrainAndCloseResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// RawSearchRequest executes a raw-mode GET and returns a stdlib response.
func RawSearchRequest(ctx context.Context, searchURL string, query Query) (*http.Response, error) {
	profile := rawRequestProfileFor(ctx, query)
	client, err := cachedRawHTTPClient(query, profile.cacheKey(), profile.tlsProfile)
	if err != nil {
		return nil, err
	}
	SetBrowserProfileID(ctx, profile.id)

	// Guarded path validates every hop, including the first.
	if query.GuardPrivateNetworks {
		return doGuardedRawRequest(ctx, client, searchURL, profile, query)
	}
	return doRawRequest(ctx, client, searchURL, profile, query)
}

// doRawRequest issues one GET and converts the response at the boundary.
func doRawRequest(ctx context.Context, client tlsclient.HttpClient, searchURL string, profile rawRequestProfile, query Query) (*http.Response, error) {
	req, err := fhttp.NewRequestWithContext(ctx, fhttp.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	applyRawRequestHeaders(req, profile)
	return execRawRequest(ctx, client, req, rawRequestUsesProxy(query))
}

// execRawRequest runs the request and converts proxy errors and the response.
func execRawRequest(ctx context.Context, client tlsclient.HttpClient, req *fhttp.Request, proxied bool) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		if proxied {
			return nil, classifyProxyNetworkError(err)
		}
		return nil, err
	}
	return convertRawResponse(ctx, resp), nil
}

const maxGuardedRedirects = 10

// doGuardedRawRequest validates every redirect hop before fetching it.
func doGuardedRawRequest(ctx context.Context, client tlsclient.HttpClient, searchURL string, profile rawRequestProfile, query Query) (*http.Response, error) {
	current := searchURL
	for hop := 0; ; hop++ {
		if err := ValidatePublicHTTPURL(ctx, current); err != nil {
			return nil, err
		}
		resp, err := doRawRequest(ctx, client, current, profile, query)
		if err != nil {
			return nil, err
		}
		location, ok := redirectLocation(resp)
		if !ok {
			return resp, nil
		}
		if hop >= maxGuardedRedirects {
			DrainAndCloseResponse(resp)
			return nil, fmt.Errorf("%w: stopped after %d redirects", ErrEngineInternal, maxGuardedRedirects)
		}
		next, err := resolveRedirectURL(current, location)
		if err != nil {
			DrainAndCloseResponse(resp)
			return nil, err
		}
		DrainAndCloseResponse(resp)
		current = next
	}
}

func redirectLocation(resp *http.Response) (string, bool) {
	if resp == nil {
		return "", false
	}
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location := strings.TrimSpace(resp.Header.Get("Location"))
		return location, location != ""
	default:
		return "", false
	}
}

func resolveRedirectURL(base, location string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	locURL, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(locURL).String(), nil
}

func ReadRawSearchBody(resp *http.Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("%w: nil raw search response", ErrEngineInternal)
	}
	if err := ClassifySearchHTTPStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

func ClassifySearchHTTPStatus(status int) error {
	switch status {
	case 0:
		return nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return ErrBlocked
	case http.StatusTooManyRequests:
		return ErrRateLimited
	}
	if status >= 500 {
		return fmt.Errorf("%w: search engine returned HTTP %d", ErrBlocked, status)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: search engine returned HTTP %d", ErrParser, status)
	}
	return nil
}

// NewRawHTTPClient returns a stdlib client backed by tls-client.
func NewRawHTTPClient(query Query) (*http.Client, error) {
	profile := rawRequestProfileFor(context.Background(), query)
	client, err := cachedRawHTTPClient(query, profile.cacheKey(), profile.tlsProfile)
	if err != nil {
		return nil, err
	}

	stdClient := &http.Client{
		Transport: rawTLSRoundTripper{
			client:               client,
			proxied:              rawRequestUsesProxy(query),
			guardPrivateNetworks: query.GuardPrivateNetworks,
			profile:              profile,
		},
		Timeout: rawHTTPTimeout,
	}
	if query.GuardPrivateNetworks {
		stdClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return ValidatePublicHTTPURL(req.Context(), req.URL.String())
		}
	}
	return stdClient, nil
}

func cachedRawHTTPClient(query Query, profileKey string, tlsProfile profiles.ClientProfile) (tlsclient.HttpClient, error) {
	proxyURL, err := NormalizeProxyURL(query.ProxyURL)
	if err != nil {
		return nil, err
	}

	key := rawHTTPClientKey{
		proxyURL:             proxyURL,
		profile:              profileKey,
		insecure:             query.Insecure,
		guardPrivateNetworks: query.GuardPrivateNetworks,
	}

	rawHTTPClientCache.Lock()
	defer rawHTTPClientCache.Unlock()

	now := time.Now()
	if entry := rawHTTPClientCache.clients[key]; entry != nil {
		entry.lastUsed = now
		return entry.client, nil
	}

	client, err := newRawTLSClient(query, proxyURL, tlsProfile)
	if err != nil {
		return nil, err
	}
	rawHTTPClientCache.clients[key] = &rawHTTPClientEntry{
		client:   client,
		lastUsed: now,
	}
	evictRawHTTPClientCacheLocked()
	return client, nil
}

func rawRequestUsesProxy(query Query) bool {
	return strings.TrimSpace(query.ProxyURL) != ""
}

func evictRawHTTPClientCacheLocked() {
	for len(rawHTTPClientCache.clients) > rawHTTPClientCacheMaxEntries {
		var (
			oldestKey   rawHTTPClientKey
			oldestEntry *rawHTTPClientEntry
		)
		for key, entry := range rawHTTPClientCache.clients {
			if oldestEntry == nil || entry.lastUsed.Before(oldestEntry.lastUsed) {
				oldestKey = key
				oldestEntry = entry
			}
		}
		if oldestEntry == nil {
			return
		}
		delete(rawHTTPClientCache.clients, oldestKey)
		oldestEntry.client.CloseIdleConnections()
	}
}

// newRawTLSClient builds a pooled Chrome-profile transport; proxyURL must be normalized.
func newRawTLSClient(query Query, proxyURL string, tlsProfile profiles.ClientProfile) (tlsclient.HttpClient, error) {
	options := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(tlsProfile),
		tlsclient.WithTimeout(int(rawHTTPTimeout / time.Second)),
		tlsclient.WithNotFollowRedirects(),
	}
	if query.Insecure {
		options = append(options, tlsclient.WithInsecureSkipVerify())
	}

	if proxyURL != "" {
		options = append(options, tlsclient.WithProxyUrl(proxyURL))
	} else if query.GuardPrivateNetworks {
		options = append(options, tlsclient.WithDialContext(GuardedDialContext))
	}

	return tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
}

// convertRawResponse keeps fhttp from leaking past this file.
func convertRawResponse(ctx context.Context, resp *fhttp.Response) *http.Response {
	if resp == nil {
		return nil
	}
	std := &http.Response{
		Status:        resp.Status,
		StatusCode:    resp.StatusCode,
		Proto:         resp.Proto,
		ProtoMajor:    resp.ProtoMajor,
		ProtoMinor:    resp.ProtoMinor,
		Header:        http.Header(resp.Header),
		ContentLength: resp.ContentLength,
		Body:          resp.Body,
	}
	if std.Body == nil {
		std.Body = io.NopCloser(bytes.NewReader(nil))
	}
	std.Body = networkUsageReadCloser{ReadCloser: std.Body, ctx: ctx}
	return std
}

type rawTLSRoundTripper struct {
	client               tlsclient.HttpClient
	proxied              bool
	guardPrivateNetworks bool
	profile              rawRequestProfile
}

func (rt rawTLSRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.guardPrivateNetworks {
		if err := ValidatePublicHTTPURL(req.Context(), req.URL.String()); err != nil {
			return nil, err
		}
	}

	freq, err := fhttp.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, err
	}
	for key, values := range req.Header {
		freq.Header[key] = values
	}
	applyRawRequestHeaders(freq, rt.profile)
	SetBrowserProfileID(req.Context(), rt.profile.id)
	return execRawRequest(req.Context(), rt.client, freq, rt.proxied)
}

func rawRequestProfileFor(ctx context.Context, query Query) rawRequestProfile {
	engine := engineFromContext(ctx)
	region := rawProfileRegion(ctx, query)
	salt := rawProfileSalt(ctx, engine, region)

	profile := browserprofile.Profile{}
	if forcedID := forcedProfileIDFromContext(ctx); forcedID != "" {
		if forced, ok := browserprofile.ProfileByID(forcedID); ok {
			profile = forced
		}
	}
	if strings.TrimSpace(profile.ID) == "" {
		profile = browserprofile.SelectProfileForSession(engine, region, salt)
	}
	profile = applyProfileLanguageHint(profile, region)

	major, tlsProfile := pickRawChromeProfile(salt + "\x00" + strings.TrimSpace(profile.ID))
	profile = applyRawChromeMajor(profile, major)

	userAgent := strings.TrimSpace(profile.UserAgent)
	if userAgent == "" {
		userAgent = fallbackRawUserAgent
	}
	acceptLanguage := strings.TrimSpace(profile.AcceptLanguage)
	if acceptLanguage == "" {
		acceptLanguage = BuildAcceptLanguageHeader(region)
	}
	if acceptLanguage == "" {
		acceptLanguage = BuildAcceptLanguageHeader(query.LangCode)
	}

	return rawRequestProfile{
		id:             strings.TrimSpace(profile.ID),
		userAgent:      userAgent,
		acceptLanguage: acceptLanguage,
		secCHUA:        formatSecCHUA(profile.UACHBrands),
		platform:       strings.TrimSpace(profile.Platform),
		mobile:         profile.Mobile,
		tlsProfile:     tlsProfile,
	}
}

func applyRawChromeMajor(profile browserprofile.Profile, major int) browserprofile.Profile {
	version := strconv.Itoa(major)
	if extractChromeVersion(profile.UserAgent) == "" {
		profile.UserAgent = fallbackRawUserAgent
	}
	profile.UserAgent = replaceChromeUserAgentVersion(profile.UserAgent, version+".0.0.0")
	profile.UACHBrands = patchBrandVersions(profile.UACHBrands, version, false)
	profile.UACHFullVerList = patchBrandVersions(profile.UACHFullVerList, version+".0.0.0", true)
	return profile
}

func rawProfileRegion(ctx context.Context, query Query) string {
	if region := profileRegionFromContext(ctx); region != "" {
		return region
	}
	if query.ProxyCountry != "" {
		return query.ProxyCountry
	}
	return profileRegionHint(query)
}

func rawProfileSalt(ctx context.Context, engine, region string) string {
	if laneKey := proxyLaneKeyFromContext(ctx); !laneKey.Empty() {
		return laneKey.SessionID
	}
	return browserprofile.LaneKey(engine, region)
}

// cacheKey includes all headers that affect the pooled fingerprint.
func (p rawRequestProfile) cacheKey() string {
	return strings.Join([]string{p.id, p.userAgent, p.acceptLanguage, p.secCHUA, p.platform, fmt.Sprint(p.mobile)}, "\x00")
}

// applyRawRequestHeaders sets the Chrome identity headers and order; tls-client
// owns Host and Accept-Encoding.
func applyRawRequestHeaders(req *fhttp.Request, profile rawRequestProfile) {
	if req == nil {
		return
	}
	secCHUAMobile := "?0"
	if profile.mobile {
		secCHUAMobile = "?1"
	}
	platform := ""
	if profile.platform != "" {
		platform = quoteSecCHValue(profile.platform)
	}
	for _, h := range [][2]string{
		{"User-Agent", profile.userAgent},
		{"Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
		{"Accept-Language", profile.acceptLanguage},
		{"Upgrade-Insecure-Requests", "1"},
		{"Sec-CH-UA", profile.secCHUA},
		{"Sec-CH-UA-Mobile", secCHUAMobile},
		{"Sec-CH-UA-Platform", platform},
		{"Sec-Fetch-Site", "none"},
		{"Sec-Fetch-Mode", "navigate"},
		{"Sec-Fetch-User", "?1"},
		{"Sec-Fetch-Dest", "document"},
	} {
		if h[1] != "" {
			req.Header.Set(h[0], h[1])
		}
	}
	req.Header[fhttp.HeaderOrderKey] = rawHeaderOrder
}

func formatSecCHUA(brands []browserprofile.BrandVersion) string {
	parts := make([]string, 0, len(brands))
	for _, brand := range brands {
		name := strings.TrimSpace(brand.Brand)
		version := strings.TrimSpace(brand.Version)
		if name == "" || version == "" {
			continue
		}
		parts = append(parts, quoteSecCHValue(name)+`;v=`+quoteSecCHValue(version))
	}
	return strings.Join(parts, ", ")
}

func quoteSecCHValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

type networkUsageReadCloser struct {
	io.ReadCloser
	ctx context.Context
}

func (r networkUsageReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	AddNetworkBytes(r.ctx, int64(n))
	return n, err
}

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	browserprofile "github.com/karust/openserp/core/browser"
	"github.com/sirupsen/logrus"
	"github.com/ysmood/gson"
)

// BrowserOpts configures Chromium launch and navigation behavior.
type BrowserOpts struct {
	// IsHeadless runs Chromium without visible UI.
	IsHeadless bool
	// IsLeakless forces child browser process cleanup when the parent exits.
	IsLeakless bool
	// Timeout is applied to browser connect and page navigation operations.
	Timeout time.Duration
	// LanguageCode sets Accept-Language for emulated requests.
	LanguageCode string
	// WaitRequests waits for request-idle state after navigation.
	WaitRequests bool
	// LeavePageOpen keeps pages open after search operations.
	LeavePageOpen bool
	// WaitLoadTime is kept for config backwards-compatibility but no longer used;
	// Navigate now calls WaitStable instead.
	WaitLoadTime time.Duration
	// CaptchaSolverApiKey enables 2Captcha integration for supported engines.
	CaptchaSolverApiKey string
	// CaptchaSolverEnabled gates solver invocation regardless of engine flags.
	CaptchaSolverEnabled bool
	// BrowserPath optionally points to a specific browser executable.
	BrowserPath string
	// ProxyURL defines the upstream proxy for browser traffic.
	ProxyURL string
	// Insecure allows invalid TLS certificates for browser requests.
	Insecure bool
	// UserAgent optionally overrides browser-reported user agent during emulation.
	UserAgent string
	// BlockResourceTypes are blocked during page navigation when non-empty.
	// Typical tokens map to these types: image, font, css(stylesheet), js(script), media.
	BlockResourceTypes []proto.NetworkResourceType
	// BlockTrackers toggles static tracker-domain blocking.
	BlockTrackers bool
}

// Check applies default option values when optional fields are unset.
func (o *BrowserOpts) Check() {
	if o.Timeout == 0 {
		o.Timeout = time.Second * 30
	}

	if o.WaitLoadTime == 0 {
		o.WaitLoadTime = time.Second * 2
	}
}

var alwaysBlockedTrackingDomains = []string{
	"google-analytics.com",
	"googletagmanager.com",
	"doubleclick.net",
	"connect.facebook.net",
}

var alwaysBlockedTrackingURLPatterns = buildTrackingDomainURLPatterns(alwaysBlockedTrackingDomains)

var blockedResourceTypeTokenMap = map[string]proto.NetworkResourceType{
	"image": proto.NetworkResourceTypeImage,
	"font":  proto.NetworkResourceTypeFont,
	"media": proto.NetworkResourceTypeMedia,
	"css":   proto.NetworkResourceTypeStylesheet,
	"js":    proto.NetworkResourceTypeScript,
}

// ParseBlockedResourceTypes parses a comma-separated config value into
// NetworkResourceType values accepted by the request blocker.
func ParseBlockedResourceTypes(raw string) ([]proto.NetworkResourceType, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	seen := make(map[proto.NetworkResourceType]struct{}, len(parts))
	out := make([]proto.NetworkResourceType, 0, len(parts))

	for _, part := range parts {
		token := strings.TrimSpace(strings.ToLower(part))
		if token == "" {
			continue
		}

		resourceType, ok := blockedResourceTypeTokenMap[token]
		if !ok {
			return nil, fmt.Errorf("unsupported resource type %q", token)
		}

		if _, exists := seen[resourceType]; exists {
			continue
		}
		seen[resourceType] = struct{}{}
		out = append(out, resourceType)
	}

	return out, nil
}

// MustParseBlockedResourceTypes is like ParseBlockedResourceTypes but panics on error.
// Only call this after the value has already been validated by ParseBlockedResourceTypes.
func MustParseBlockedResourceTypes(raw string) []proto.NetworkResourceType {
	types, err := ParseBlockedResourceTypes(raw)
	if err != nil {
		panic(fmt.Sprintf("MustParseBlockedResourceTypes: %v", err))
	}
	return types
}

func buildTrackingDomainURLPatterns(domains []string) []string {
	patterns := make([]string, 0, len(domains)*2)
	for _, domain := range domains {
		domain = strings.TrimSpace(strings.ToLower(domain))
		if domain == "" {
			continue
		}
		patterns = append(patterns, "*://"+domain+"/*", "*://*."+domain+"/*")
	}
	return patterns
}

func blockedResourceTypeSet(types []proto.NetworkResourceType) map[proto.NetworkResourceType]struct{} {
	out := make(map[proto.NetworkResourceType]struct{}, len(types))
	for _, t := range types {
		if t != "" {
			out[t] = struct{}{}
		}
	}
	return out
}

func (b *Browser) configureRequestBlocking(ctx context.Context, page *rod.Page) error {
	if !b.BlockTrackers && len(b.BlockResourceTypes) == 0 {
		return nil
	}

	if b.BlockTrackers && len(alwaysBlockedTrackingURLPatterns) > 0 {
		if err := (proto.NetworkEnable{}).Call(page); err != nil {
			return fmt.Errorf("enable network domain for tracker blocking: %w", err)
		}
		if err := (proto.NetworkSetBlockedURLs{Urls: alwaysBlockedTrackingURLPatterns}).Call(page); err != nil {
			return fmt.Errorf("set blocked tracking URLs: %w", err)
		}
	}

	if len(b.BlockResourceTypes) == 0 {
		return nil
	}

	blocked := blockedResourceTypeSet(b.BlockResourceTypes)
	router := page.HijackRequests()
	router.MustAdd("*", func(h *rod.Hijack) {
		if _, ok := blocked[h.Request.Type()]; ok {
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}
		h.ContinueRequest(&proto.FetchContinueRequest{})
	})

	go router.Run()

	// Stop the router when the page context is done to avoid goroutine leak.
	go func() {
		<-ctx.Done()
		router.MustStop()
	}()

	return nil
}

// Browser wraps a launched Chromium instance used by engine implementations.
type Browser struct {
	BrowserOpts
	browserAddr   string
	conn          *browserConnection
	CaptchaSolver *CaptchaSolver
}

type browserConnection struct {
	mu           sync.Mutex
	browser      *rod.Browser
	laneProfiles map[string]browserprofile.Profile
}

// NewBrowser launches a new Chromium process via Rod launcher and returns a
// Browser wrapper configured with proxy and captcha solver settings.
func NewBrowser(opts BrowserOpts) (*Browser, error) {
	opts.Check()
	if strings.TrimSpace(opts.UserAgent) != "" {
		logrus.Warn("custom user_agent override can reduce profile coherence; use only for diagnostics")
	}
	logrus.WithField("browser_options", fmt.Sprintf("%+v", opts)).Debug("Browser options")

	path, err := resolveBrowserBinaryPath(opts.BrowserPath, launcher.LookPath)
	if err != nil {
		return nil, err
	}

	// Create launcher.
	// headless=new uses the full Chrome renderer; legacy --headless disables the
	// GPU process entirely, making WebGL context creation fail even with SwiftShader.
	// use-angle=swiftshader-webgl (Chrome ≥112) enables a software WebGL renderer.
	l := launcher.New().Leakless(opts.IsLeakless).
		Set("disable-blink-features", "AutomationControlled").
		Delete("enable-automation").
		Set("use-angle", "swiftshader-webgl").
		Set("ignore-gpu-blocklist")
	if opts.IsHeadless {
		l = l.HeadlessNew(true)
	} else {
		l = l.Headless(false)
	}
	if path != "" {
		logrus.WithField("browser_path", path).Debug("Using browser binary")
		l = l.Bin(path)
	}

	// Configure proxy if specified
	if opts.ProxyURL != "" {
		normalizedProxyURL, err := NormalizeProxyURL(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %v", err)
		}
		opts.ProxyURL = normalizedProxyURL

		proxyUrl, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %v", err)
		}

		// Chrome's proxy-server flag must not contain credentials.
		// Auth (if needed) is handled separately via DevTools auth callbacks.
		proxyStr := proxyURLForBrowserLaunch(proxyUrl)
		logrus.WithField("proxy", MaskProxyURL(proxyStr)).Debug("Setting up proxy")
		l = l.Proxy(proxyStr)

		// Check if proxy has auth credentials
		if proxyUrl.User != nil {
			username := proxyUrl.User.Username()
			logrus.WithFields(logrus.Fields{
				"proxy_scheme":   proxyUrl.Scheme,
				"proxy_username": username,
			}).Debugf("Proxy credentials configured for %s proxy: %s:****", proxyUrl.Scheme, username)
		}
	}

	b := Browser{
		BrowserOpts: opts,
		conn:        &browserConnection{},
	}
	b.browserAddr, err = l.Launch()

	if opts.CaptchaSolverEnabled && opts.CaptchaSolverApiKey != "" {
		b.CaptchaSolver = NewSolver(opts.CaptchaSolverApiKey)
		logrus.Debug("Captcha solver initialized")
	}

	return &b, err
}

func proxyURLForBrowserLaunch(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	// Chrome expects socks5 scheme in --proxy-server; socks5h is not accepted.
	if clone.Scheme == "socks5h" {
		clone.Scheme = "socks5"
	}
	clone.User = nil
	clone.Path = ""
	clone.RawPath = ""
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}

func validateBrowserBinaryPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path points to a directory")
	}
	return nil
}

// resolveBrowserBinaryPath prefers an explicit browser path. If no explicit path is provided,
// it falls back to launcher autodiscovery and lets Rod handle auto-download when no binary is found.
func resolveBrowserBinaryPath(browserPath string, lookPath func() (string, bool)) (string, error) {
	if browserPath != "" {
		if err := validateBrowserBinaryPath(browserPath); err != nil {
			return "", fmt.Errorf("invalid browser_path %q: %w", browserPath, err)
		}
		return browserPath, nil
	}

	path, has := lookPath()
	if has {
		return path, nil
	}

	return "", nil
}

// IsInitialized reports whether the browser launcher has been created.
func (b *Browser) IsInitialized() bool {
	return b.browserAddr != ""
}

func (b *Browser) connectionState() *browserConnection {
	if b.conn == nil {
		b.conn = &browserConnection{}
	}
	return b.conn
}

func (b *Browser) newRodBrowser() *rod.Browser {
	browser := rod.New().NoDefaultDevice().ControlURL(b.browserAddr)
	if b.Timeout > 0 {
		browser = browser.Timeout(b.Timeout)
	}
	return browser
}

func (b *Browser) connectBrowser() (*rod.Browser, error) {
	browser := b.newRodBrowser()
	if err := browser.Connect(); err != nil {
		return nil, err
	}

	// Keep cert handling on the persistent browser session.
	// Proxy runtime can surface MITM certs, and insecure mode is explicit opt-in.
	if b.ProxyURL != "" || b.Insecure {
		if err := browser.IgnoreCertErrors(true); err != nil {
			return nil, err
		}
	}

	return browser, nil
}

func (b *Browser) ensureConnectedBrowser(ctx context.Context, forceReconnect bool) (*rod.Browser, error) {
	if b == nil || b.browserAddr == "" {
		return nil, fmt.Errorf("browser is not initialized")
	}

	state := b.connectionState()
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.browser == nil || forceReconnect {
		connected, err := b.connectBrowser()
		if err != nil {
			return nil, err
		}
		state.browser = connected
		return state.browser, nil
	}

	if _, err := state.browser.Version(); err != nil {
		WithRequest(ctx).WithError(err).Debug("Browser ping failed, reconnecting")
		connected, reconnectErr := b.connectBrowser()
		if reconnectErr != nil {
			return nil, reconnectErr
		}
		state.browser = connected
	}

	return state.browser, nil
}

func createIsolatedPage(browser *rod.Browser) (*rod.Page, proto.BrowserBrowserContextID, error) {
	browserContext, err := (proto.TargetCreateBrowserContext{}).Call(browser)
	if err != nil {
		return nil, "", err
	}

	target, err := (proto.TargetCreateTarget{
		URL:              "about:blank",
		BrowserContextID: browserContext.BrowserContextID,
	}).Call(browser)
	if err != nil {
		_ = disposeBrowserContext(browser, browserContext.BrowserContextID)
		return nil, "", err
	}

	page, err := browser.PageFromTarget(target.TargetID)
	if err != nil {
		_ = disposeBrowserContext(browser, browserContext.BrowserContextID)
		return nil, "", err
	}

	return page, browserContext.BrowserContextID, nil
}

func disposeBrowserContext(browser *rod.Browser, browserContextID proto.BrowserBrowserContextID) error {
	if browser == nil || browserContextID == "" {
		return nil
	}
	return (proto.TargetDisposeBrowserContext{
		BrowserContextID: browserContextID,
	}).Call(browser)
}

func (b *Browser) startProxyAuthHandler(ctx context.Context, browser *rod.Browser) (context.CancelFunc, error) {
	if browser == nil || b.ProxyURL == "" {
		return nil, nil
	}

	proxyURL, err := url.Parse(b.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy URL: %w", err)
	}

	if proxyURL.User == nil {
		return nil, nil
	}

	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
			logrus.Debug("SOCKS proxy credentials are not handled by browser auth callback")
		}
		return nil, nil
	}

	username := proxyURL.User.Username()
	password, _ := proxyURL.User.Password()
	authCtx, cancel := context.WithCancel(EnsureContext(ctx))

	go func() {
		if err := browser.Context(authCtx).HandleAuth(username, password)(); err != nil && !errors.Is(err, context.Canceled) {
			WithRequest(ctx).WithError(err).Debug("Proxy auth handler stopped")
		}
	}()

	return cancel, nil
}

var chromeVersionPattern = regexp.MustCompile(`(?:HeadlessChrome|Chrome)/([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)

func (b *Browser) laneProfile(ctx context.Context, browser *rod.Browser) (browserprofile.Profile, string) {
	engine := engineFromContext(ctx)
	region := profileRegionFromContext(ctx)
	if region == "" {
		region = strings.TrimSpace(b.LanguageCode)
	}
	laneKey := browserprofile.LaneKey(engine, region)

	state := b.connectionState()
	state.mu.Lock()
	if state.laneProfiles == nil {
		state.laneProfiles = make(map[string]browserprofile.Profile)
	}
	if profile, ok := state.laneProfiles[laneKey]; ok {
		state.mu.Unlock()
		return profile, laneKey
	}
	state.mu.Unlock()

	// Resolve profile outside the lock: SelectProfile reads from a separate
	// RWMutex-guarded catalog, and applyRuntimeBrowserVersion makes a CDP
	// round-trip (browser.Version). Holding state.mu over network I/O would
	// serialize all concurrent Navigate calls.
	profile := browserprofile.SelectProfile(engine, region)
	profile = applyRuntimeBrowserVersion(profile, browser)
	profile = applyProfileLanguageHint(profile, region)
	if overrideUA := strings.TrimSpace(b.UserAgent); overrideUA != "" {
		profile.UserAgent = overrideUA
	}

	state.mu.Lock()
	if state.laneProfiles == nil {
		state.laneProfiles = make(map[string]browserprofile.Profile)
	}
	if _, exists := state.laneProfiles[laneKey]; !exists {
		state.laneProfiles[laneKey] = profile
	} else {
		profile = state.laneProfiles[laneKey]
	}
	state.mu.Unlock()

	return profile, laneKey
}

func applyRuntimeBrowserVersion(profile browserprofile.Profile, browser *rod.Browser) browserprofile.Profile {
	fullVersion := ""
	if browser != nil {
		version, err := browser.Version()
		if err == nil && version != nil {
			fullVersion = extractChromeVersion(version.UserAgent)
			if fullVersion == "" {
				fullVersion = extractChromeVersion(version.Product)
			}
		}
	}
	if fullVersion == "" {
		fullVersion = extractChromeVersion(profile.UserAgent)
	}
	if fullVersion == "" {
		return profile
	}

	major := chromeMajorVersion(fullVersion)
	if major == "" {
		return profile
	}

	profile.UserAgent = replaceChromeUserAgentVersion(profile.UserAgent, major+".0.0.0")
	profile.UACHBrands = patchBrandVersions(profile.UACHBrands, major, false)
	profile.UACHFullVerList = patchBrandVersions(profile.UACHFullVerList, fullVersion, true)
	return profile
}

func extractChromeVersion(value string) string {
	matches := chromeVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func chromeMajorVersion(fullVersion string) string {
	fullVersion = strings.TrimSpace(fullVersion)
	if fullVersion == "" {
		return ""
	}
	parts := strings.Split(fullVersion, ".")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func replaceChromeUserAgentVersion(userAgent string, replacement string) string {
	replacement = strings.TrimSpace(replacement)
	if replacement == "" {
		return strings.ReplaceAll(strings.TrimSpace(userAgent), "HeadlessChrome/", "Chrome/")
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(userAgent), "HeadlessChrome/", "Chrome/")
	return chromeVersionPattern.ReplaceAllString(normalized, "Chrome/"+replacement)
}

func patchBrandVersions(values []browserprofile.BrandVersion, version string, patchNotABrand bool) []browserprofile.BrandVersion {
	version = strings.TrimSpace(version)
	if version == "" {
		return values
	}

	out := make([]browserprofile.BrandVersion, 0, len(values))
	for _, value := range values {
		item := value
		brandLower := strings.ToLower(strings.TrimSpace(item.Brand))
		if brandLower == "chromium" || brandLower == "google chrome" {
			item.Version = version
		} else if patchNotABrand && strings.Contains(brandLower, "not_a brand") && strings.Count(version, ".") == 3 {
			item.Version = "24.0.0.0"
		}
		out = append(out, item)
	}
	return out
}

func navigatorPlatformForProfile(profile browserprofile.Profile) string {
	switch strings.ToLower(strings.TrimSpace(profile.Platform)) {
	case "windows":
		return "Win32"
	case "macos":
		return "MacIntel"
	case "linux":
		return "Linux x86_64"
	default:
		return strings.TrimSpace(profile.Platform)
	}
}

// applyProfileLanguageHint overrides the profile's locale-derived fields when
// the requested language differs from the cached profile's. A bare language
// hint that already matches the profile language is treated as a no-op so that
// an explicit profile region (e.g. en-GB) isn't clobbered by a default (en-US).
func applyProfileLanguageHint(profile browserprofile.Profile, langCode string) browserprofile.Profile {
	hint := ParseLocale(langCode)
	if hint.Language == "" {
		return profile
	}

	current := ParseLocale(profile.Locale)
	if current.Language == hint.Language && (hint.Country == "" || current.Country == hint.Country) {
		return profile
	}

	primary := PrimaryLanguageTag(langCode)
	if primary == "" {
		return profile
	}

	profile.AcceptLanguage = BuildAcceptLanguageHeader(langCode)
	profile.NavigatorLangs = []string{primary}
	profile.Locale = primary
	return profile
}

func applyProfile(page *rod.Page, profile browserprofile.Profile) error {
	if page == nil {
		return fmt.Errorf("page is nil")
	}

	navigatorLangs := profileNavigatorLanguages(profile)
	acceptLanguage := strings.TrimSpace(profile.AcceptLanguage)
	if acceptLanguage == "" {
		acceptLanguage = navigatorLangs[0]
	}
	locale := strings.TrimSpace(profile.Locale)
	if locale == "" {
		locale = navigatorLangs[0]
	}

	width := profile.Viewport.Width
	height := profile.Viewport.Height
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}

	metadata := &proto.EmulationUserAgentMetadata{
		Brands:          toProtoBrandVersions(profile.UACHBrands),
		FullVersionList: toProtoBrandVersions(profile.UACHFullVerList),
		Platform:        strings.TrimSpace(profile.Platform),
		PlatformVersion: strings.TrimSpace(profile.PlatformVersion),
		Architecture:    strings.TrimSpace(profile.Architecture),
		Bitness:         strings.TrimSpace(profile.Bitness),
		Mobile:          profile.Mobile,
	}

	if err := (proto.NetworkSetUserAgentOverride{
		UserAgent:         strings.TrimSpace(profile.UserAgent),
		AcceptLanguage:    acceptLanguage,
		Platform:          navigatorPlatformForProfile(profile),
		UserAgentMetadata: metadata,
	}).Call(page); err != nil {
		return fmt.Errorf("set user agent override failed: %w", err)
	}

	if err := (proto.EmulationSetLocaleOverride{
		Locale: locale,
	}).Call(page); err != nil {
		return fmt.Errorf("set locale override failed: %w", err)
	}

	if err := (proto.EmulationSetTimezoneOverride{
		TimezoneID: strings.TrimSpace(profile.Timezone),
	}).Call(page); err != nil {
		return fmt.Errorf("set timezone override failed: %w", err)
	}

	if err := (proto.EmulationSetDeviceMetricsOverride{
		Width:             width,
		Height:            height,
		DeviceScaleFactor: 1,
		Mobile:            profile.Mobile,
		ScreenWidth:       &width,
		ScreenHeight:      &height,
	}).Call(page); err != nil {
		return fmt.Errorf("set device metrics failed: %w", err)
	}

	if err := (proto.NetworkSetExtraHTTPHeaders{
		Headers: proto.NetworkHeaders{
			"Accept-Language": gson.New(acceptLanguage),
		},
	}).Call(page); err != nil {
		return fmt.Errorf("set extra headers failed: %w", err)
	}

	if err := evalPatchScript(page, navigatorLangs, width, height); err != nil {
		return err
	}

	return nil
}

func profileNavigatorLanguages(profile browserprofile.Profile) []string {
	langs := make([]string, 0, len(profile.NavigatorLangs))
	for _, language := range profile.NavigatorLangs {
		trimmed := strings.TrimSpace(language)
		if trimmed == "" {
			continue
		}
		langs = append(langs, trimmed)
	}
	if len(langs) > 0 {
		return langs
	}

	acceptLanguage := strings.TrimSpace(profile.AcceptLanguage)
	if acceptLanguage != "" {
		parts := strings.Split(acceptLanguage, ",")
		langs = make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if idx := strings.Index(part, ";"); idx >= 0 {
				part = strings.TrimSpace(part[:idx])
			}
			if part != "" {
				langs = append(langs, part)
			}
		}
		if len(langs) > 0 {
			return langs
		}
	}

	if locale := strings.TrimSpace(profile.Locale); locale != "" {
		return []string{locale}
	}
	return []string{"en-US"}
}

func toProtoBrandVersions(values []browserprofile.BrandVersion) []*proto.EmulationUserAgentBrandVersion {
	out := make([]*proto.EmulationUserAgentBrandVersion, 0, len(values))
	for _, value := range values {
		brand := strings.TrimSpace(value.Brand)
		version := strings.TrimSpace(value.Version)
		if brand == "" || version == "" {
			continue
		}
		out = append(out, &proto.EmulationUserAgentBrandVersion{
			Brand:   brand,
			Version: version,
		})
	}
	return out
}

func evalPatchScript(page *rod.Page, langs []string, width, height int) error {
	langsJSON, err := json.Marshal(langs)
	if err != nil {
		return fmt.Errorf("marshal navigator languages: %w", err)
	}
	args := fmt.Sprintf("const __langs = %s;\nconst __w = %d;\nconst __h = %d;\n",
		string(langsJSON), width, height)
	_, err = page.EvalOnNewDocument(args + string(browserprofile.PatchJS))
	if err != nil {
		return fmt.Errorf("eval patch script: %w", err)
	}
	return nil
}

// Navigate connects to Chromium, creates a page, applies a coherent profile and
// proxy auth, then navigates to URL. It returns an initialized page ready for
// selector queries, or an error when browser setup/navigation fails.
func (b *Browser) Navigate(ctx context.Context, URL string) (*rod.Page, error) {
	ctx = EnsureContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	WithRequest(ctx).WithField("url", URL).Debug("Navigate")

	browser, err := b.ensureConnectedBrowser(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("browser connect failed: %w", err)
	}

	page, browserContextID, err := createIsolatedPage(browser)
	if err != nil {
		// Single-shot reconnect for stale websocket sessions.
		browser, err = b.ensureConnectedBrowser(ctx, true)
		if err != nil {
			return nil, fmt.Errorf("create isolated page failed, reconnect also failed: %w", err)
		}
		page, browserContextID, err = createIsolatedPage(browser)
		if err != nil {
			return nil, fmt.Errorf("create isolated page failed after reconnect: %w", err)
		}
	}

	// closeOnErr closes page then disposes context. Order matters: disposing the
	// context first causes Chrome to kill the page target before our Close call,
	// producing a spurious "target closed" error on the page.Close() that follows.
	closeOnErr := func() {
		if cerr := page.Close(); cerr != nil && !isBrowserClosedError(cerr) {
			WithRequest(ctx).WithError(cerr).Debug("Close page after navigate error failed")
		}
		if derr := disposeBrowserContext(browser, browserContextID); derr != nil && !isBrowserClosedError(derr) {
			WithRequest(ctx).WithError(derr).Debug("Dispose browser context after navigate error failed")
		}
	}

	cancelProxyAuth, err := b.startProxyAuthHandler(ctx, browser)
	if err != nil {
		closeOnErr()
		return nil, fmt.Errorf("proxy auth setup failed: %w", err)
	}
	if cancelProxyAuth != nil {
		defer cancelProxyAuth()
	}

	profile, laneKey := b.laneProfile(ctx, browser)
	if err := applyProfile(page, profile); err != nil {
		closeOnErr()
		return nil, fmt.Errorf("apply profile %s (%s) failed: %w", profile.ID, laneKey, err)
	}

	page = page.Context(ctx)
	if err := b.configureRequestBlocking(ctx, page); err != nil {
		closeOnErr()
		return nil, fmt.Errorf("configure request blocking failed: %w", err)
	}
	timedPage := page.Timeout(b.Timeout)

	if err := timedPage.Navigate(URL); err != nil {
		closeOnErr()
		return nil, err
	}

	// Avoid panics from MustWaitLoad when the target navigates/closes mid-wait
	if werr := timedPage.WaitLoad(); werr != nil {
		if errors.Is(werr, context.DeadlineExceeded) {
			// Some engines keep loading background resources while the DOM is already usable.
			// Treat load timeout as non-fatal and let engine-specific selector timeouts decide.
			WithRequest(ctx).WithField("timeout", b.Timeout.String()).Debug(
				fmt.Sprintf("WaitLoad timed out after %s; continuing with partial page state", b.Timeout),
			)
		} else {
			WithRequest(ctx).WithError(werr).Debug("WaitLoad returned early")
		}
	}

	// may cause bugs with google
	if b.WaitRequests {
		wait := timedPage.WaitRequestIdle(300*time.Millisecond, nil, nil, nil)
		wait()
	}

	// WaitStable blocks until no layout changes occur for 800 ms, or falls
	// through on error so engine-specific selector timeouts handle the result.
	if err := page.Context(ctx).WaitStable(800 * time.Millisecond); err != nil {
		WithRequest(ctx).WithError(err).Debug("WaitStable returned early; continuing")
	}
	return page, nil
}

// Close closes the active browser connection.
func (b *Browser) Close() error {
	if b == nil || b.browserAddr == "" {
		return nil
	}

	state := b.connectionState()
	state.mu.Lock()
	defer state.mu.Unlock()

	browser := state.browser
	if browser == nil {
		browser = b.newRodBrowser()
		if err := browser.Connect(); err != nil {
			if isBrowserClosedError(err) {
				return nil
			}
			return err
		}
	}

	state.browser = nil
	state.laneProfiles = nil
	if err := browser.Close(); err != nil && !isBrowserClosedError(err) {
		return err
	}
	return nil
}

func isBrowserClosedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "closed network connection") ||
		strings.Contains(msg, "target closed") ||
		strings.Contains(msg, "eof")
}

// ClosePageWithTimeout bounds page close calls so shutdown paths don't hang.
func ClosePageWithTimeout(ctx context.Context, page *rod.Page, timeout time.Duration) error {
	if page == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = time.Second
	}
	baseCtx := EnsureContext(ctx)
	if baseCtx.Err() != nil {
		baseCtx = context.Background()
	}
	closeCtx, cancel := context.WithTimeout(baseCtx, timeout)
	defer cancel()

	pageWithTimeout := page.Context(closeCtx)
	info, _ := pageWithTimeout.Info()

	if err := pageWithTimeout.Close(); err != nil && !isBrowserClosedError(err) {
		return err
	}

	if info != nil && info.BrowserContextID != "" {
		if derr := disposeBrowserContext(page.Browser().Context(closeCtx), info.BrowserContextID); derr != nil && !isBrowserClosedError(derr) {
			return derr
		}
	}
	return nil
}

// RecoverEnginePanic converts recovered panics to a typed engine error and
// logs stack trace with engine context.
func RecoverEnginePanic(engine string, recovered interface{}, logger *EngineLogger) error {
	return RecoverEnginePanicWithContext(context.TODO(), engine, recovered, logger)
}

func RecoverEnginePanicWithContext(ctx context.Context, engine string, recovered interface{}, logger *EngineLogger) error {
	stack := debug.Stack()
	if logger != nil {
		logger.Error("Recovered panic in %s Search: panic=%v\n%s", engine, recovered, string(stack))
	} else {
		WithRequestEngine(ctx, engine).Errorf("Recovered panic in %s Search: panic=%v\n%s", engine, recovered, string(stack))
	}
	return fmt.Errorf("%w: %s", ErrEngineInternal, engine)
}

// IsRodObjectNotFound reports element/object lookup misses across rod error
// variants used by selector calls.
func IsRodObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	var objectErr *rod.ObjectNotFoundError
	if errors.As(err, &objectErr) {
		return true
	}
	var elementErr *rod.ElementNotFoundError
	return errors.As(err, &elementErr)
}

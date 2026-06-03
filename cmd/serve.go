package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/karust/openserp/baidu"
	"github.com/karust/openserp/bing"
	"github.com/karust/openserp/core"
	"github.com/karust/openserp/duckduckgo"
	"github.com/karust/openserp/ecosia"
	"github.com/karust/openserp/google"
	"github.com/karust/openserp/yandex"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

// rawEngine implements SearchEngine interface for raw HTTP requests
type rawEngine struct {
	name string
}

func (r *rawEngine) Search(ctx context.Context, q core.Query) ([]core.SearchResult, error) {
	q.Insecure = config.Server.Insecure

	switch r.name {
	case "google":
		return google.Search(ctx, q)
	case "yandex":
		return yandex.Search(ctx, q)
	case "baidu":
		return baidu.Search(ctx, q)
	case "ecosia":
		return ecosia.Search(ctx, q)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", r.name)
	}
}

func (r *rawEngine) SearchImage(_ context.Context, _ core.Query) ([]core.SearchResult, error) {
	return nil, fmt.Errorf("image search is not supported in raw mode for %s", r.name)
}

func (r *rawEngine) Name() string {
	return r.name
}

func (r *rawEngine) IsInitialized() bool {
	return true
}

func (r *rawEngine) GetRateLimiter() *rate.Limiter {
	// Use default rate limiter for raw requests
	return rate.NewLimiter(rate.Every(time.Second), 5)
}

var serveCMD = &cobra.Command{
	Use:     "serve",
	Aliases: []string{"listen"},
	Short:   "Start HTTP server, to provide search engine results via API",
	Args:    cobra.MatchAll(cobra.NoArgs),
	Run:     serve,
}

func serve(cmd *cobra.Command, args []string) {
	corsCfg := core.DefaultCORSConfig()
	corsCfg.AllowOrigins = config.CORS.AllowOrigins
	corsCfg.AllowMethods = config.CORS.AllowMethods
	corsCfg.AllowHeaders = config.CORS.AllowHeaders
	corsCfg.MaxAge = config.CORS.MaxAge

	captchaSolverEnabled, captchaSolverAPIKey, err := resolveCaptchaSolverConfig()
	if err != nil {
		logrus.Error(err)
		os.Exit(1)
	}

	proxyRuntime := core.ProxyRuntimeBrowser
	if config.Server.IsRawRequests {
		proxyRuntime = core.ProxyRuntimeRaw
	}

	proxyCfg, err := buildNormalizedProxyConfig(proxyRuntime)
	if err != nil {
		logrus.WithError(err).Error(fmt.Sprintf("invalid proxy configuration: %v", err))
		return
	}

	fingerprintBrowserOpts := buildFingerprintBrowserOptions()

	if config.Server.IsRawRequests {
		logrus.Warn("Browserless results are very inconsistent or may not even work!")
		serverOpts := buildServerOptions(corsCfg, proxyCfg, fingerprintBrowserOpts)
		serv := core.NewServerWithOptions(config.Server.Host, config.Server.Port, serverOpts,
			&rawEngine{name: "google"},
			&rawEngine{name: "yandex"},
			&rawEngine{name: "baidu"},
			&rawEngine{name: "ecosia"},
		)
		if err := listenWithGracefulShutdown(serv, nil); err != nil {
			logrus.Error(err)
		}
		return
	}

	baseOpts := fingerprintBrowserOpts
	baseOpts.LeavePageOpen = config.App.IsLeaveHead
	baseOpts.CaptchaSolverEnabled = captchaSolverEnabled
	baseOpts.CaptchaSolverApiKey = captchaSolverAPIKey

	engines, closeBrowsers, browserResolver, err := buildBrowserEngines(baseOpts, proxyCfg)
	if err != nil {
		logrus.Error(err)
		return
	}

	serverOpts := buildServerOptions(corsCfg, proxyCfg, fingerprintBrowserOpts)
	serverOpts.BrowserResolver = browserResolver
	serv := core.NewServerWithOptions(config.Server.Host, config.Server.Port, serverOpts, engines...)
	if err := listenWithGracefulShutdown(serv, closeBrowsers); err != nil {
		logrus.Error(err)
	}
}

func buildFingerprintBrowserOptions() core.BrowserOpts {
	blockedResourceTypes := core.MustParseBlockedResourceTypes(config.App.BlockResources)

	opts := core.BrowserOpts{
		IsHeadless:         !config.App.IsBrowserHead,
		IsLeakless:         config.App.IsLeakless,
		Timeout:            time.Second * time.Duration(config.App.Timeout),
		BrowserPath:        config.App.BrowserPath,
		Insecure:           config.Server.Insecure,
		BlockResourceTypes: blockedResourceTypes,
		BlockTrackers:      config.App.BlockTrackers,
	}
	if config.Server.IsDebug {
		opts.IsHeadless = false
	}
	return opts
}

func buildServerOptions(corsCfg core.CORSConfig, proxyCfg core.ProxyConfig, fingerprintBrowserOpts core.BrowserOpts) core.ServerOptions {
	return core.ServerOptions{
		CacheTTL:               time.Duration(config.Cache.TTLSeconds) * time.Second,
		CacheMaxSize:           config.Cache.MaxSize,
		EnableCORS:             config.CORS.Enabled,
		CORS:                   corsCfg,
		AllowEndpointFallback:  config.Resilience.AllowEndpointFallback,
		EnableDebugEndpoints:   config.App.DebugEndpoints,
		FingerprintArtifactDir: core.DefaultFingerprintArtifactDir,
		FingerprintBrowserOpts: fingerprintBrowserOpts,
		MegaTimeout:            config.App.MegaTimeout,
		Extract:                config.Extract,
		Resilience: core.ResilientConfig{
			Retry: core.RetryConfig{
				MaxRetries:     config.Resilience.MaxRetries,
				InitialBackoff: 1 * time.Second,
				MaxBackoff:     30 * time.Second,
				BackoffFactor:  2.0,
			},
			CircuitBreaker: core.CircuitBreakerConfig{
				FailureThreshold: config.CircuitBreaker.Failures,
				RecoveryTimeout:  time.Duration(config.CircuitBreaker.RecoverySeconds) * time.Second,
				SuccessThreshold: config.CircuitBreaker.Successes,
			},
			Proxy: proxyCfg,
		},
	}
}

const gracefulShutdownTimeout = 30 * time.Second

func listenWithGracefulShutdown(serv *core.Server, onShutdown func() error) error {
	listenErrCh := make(chan error, 1)
	go func() {
		listenErrCh <- serv.Listen()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-listenErrCh:
		return err
	case sig := <-sigCh:
		logrus.WithField("signal", sig.String()).Info("Shutdown signal received, draining traffic")
	}

	serv.SetDraining(true)

	shutdownErr := serv.ShutdownWithTimeout(gracefulShutdownTimeout)
	if isServerNotRunningError(shutdownErr) {
		shutdownErr = nil
	}

	if onShutdown != nil {
		resourceErr := onShutdown()
		if resourceErr != nil {
			shutdownErr = errors.Join(shutdownErr, resourceErr)
		}
	}

	if listenErr := waitForListenExit(listenErrCh); listenErr != nil && !isExpectedListenShutdownError(listenErr) {
		shutdownErr = errors.Join(shutdownErr, listenErr)
	}

	return shutdownErr
}

func waitForListenExit(listenErrCh <-chan error) error {
	select {
	case err := <-listenErrCh:
		return err
	case <-time.After(time.Second):
		return nil
	}
}

func isExpectedListenShutdownError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "server closed") ||
		strings.Contains(msg, "closed network connection")
}

func isServerNotRunningError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "server is not running")
}

// pooledBrowser is one Chrome process in the pool, dedicated to a single proxy
// auth identity (or to the shared no-auth/unauth path when launchProxyURL=="").
type pooledBrowser struct {
	browser        *core.Browser
	launchProxyURL string
	lastUsedAt     time.Time
}

// browserPool keeps a bounded set of Chrome processes keyed by proxy auth
// identity (scheme+host+port+username). Each entry was launched with its own
// `l.Proxy(...)` so Chrome handles 407 natively for the main document AND all
// subresources. Direct and unauthenticated proxies share one entry whose Chrome
// was launched without a process-level proxy; per-BrowserContext ProxyServer is
// applied at request time for unauthenticated request-URL proxies.
type browserPool struct {
	mu        sync.Mutex
	base      core.BrowserOpts
	laneStore *core.LaneStore

	maxProcesses int
	idleTTL      time.Duration

	browsers map[string]*pooledBrowser

	evictedLRU  int
	evictedIdle int

	stopSweeper chan struct{}
	sweeperDone chan struct{}
}

const directBrowserKey = "direct"

func newBrowserPool(base core.BrowserOpts, defaultLaunchProxyURL string, laneStore *core.LaneStore, maxProcesses int, idleTTL time.Duration) *browserPool {
	base.ProxyLaneStore = laneStore
	if maxProcesses <= 0 {
		maxProcesses = 4
	}
	pool := &browserPool{
		base:         base,
		laneStore:    laneStore,
		maxProcesses: maxProcesses,
		idleTTL:      idleTTL,
		browsers:     map[string]*pooledBrowser{},
		stopSweeper:  make(chan struct{}),
		sweeperDone:  make(chan struct{}),
	}
	// A configured global proxy (legacy) becomes a pre-bound entry on the
	// shared "direct" key so requests without a per-request proxy still use it.
	if launchURL := strings.TrimSpace(defaultLaunchProxyURL); launchURL != "" {
		pool.browsers[directBrowserKey] = &pooledBrowser{
			launchProxyURL: launchURL,
			lastUsedAt:     time.Now(),
		}
	}
	if idleTTL > 0 {
		go pool.sweepIdle()
	} else {
		close(pool.sweeperDone)
	}
	return pool
}

// browserPoolKey derives the pool key from a request's proxy URL. Authenticated
// HTTP/HTTPS proxies get their own Chrome keyed by scheme+host+port+username.
// Empty/unauthenticated/SOCKS request URLs fall through to the shared
// "direct" Chrome.
func browserPoolKey(requestProxyURL string) string {
	requestProxyURL = strings.TrimSpace(requestProxyURL)
	if requestProxyURL == "" {
		return directBrowserKey
	}
	normalized, err := core.NormalizeProxyURL(requestProxyURL)
	if err != nil || normalized == "" {
		return directBrowserKey
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return directBrowserKey
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		// Authenticated SOCKS is rejected upstream; unauthenticated SOCKS goes
		// through the per-context proxy path on the shared Chrome.
		return directBrowserKey
	}
	if parsed.User == nil {
		return directBrowserKey
	}
	username := parsed.User.Username()
	return fmt.Sprintf("%s|%s|%s", parsed.Scheme, parsed.Host, username)
}

// browserLaunchURL returns the URL to pass to launcher.Proxy for a given
// request URL, or "" when the launch should be unproxied (direct + unauth).
func browserLaunchURL(requestProxyURL string) string {
	requestProxyURL = strings.TrimSpace(requestProxyURL)
	if requestProxyURL == "" {
		return ""
	}
	normalized, err := core.NormalizeProxyURL(requestProxyURL)
	if err != nil || normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.User == nil {
		return ""
	}
	return normalized
}

// get returns a Chrome that can route the supplied request URL. For
// authenticated HTTP(S) proxies it returns the dedicated Chrome (launching one
// if needed). For everything else it returns the shared "direct" Chrome.
func (p *browserPool) get(requestProxyURL string) (*core.Browser, error) {
	key := browserPoolKey(requestProxyURL)
	launchURL := ""
	if key != directBrowserKey {
		launchURL = browserLaunchURL(requestProxyURL)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.browsers[key]; ok && entry.browser != nil {
		entry.lastUsedAt = time.Now()
		return entry.browser, nil
	}

	// Use any pre-bound launchProxyURL on the existing entry (e.g. legacy
	// global proxy) when the caller didn't supply one.
	if entry, ok := p.browsers[key]; ok && entry.browser == nil {
		if launchURL == "" {
			launchURL = entry.launchProxyURL
		}
	}

	opts := p.base
	opts.ProxyURL = launchURL
	browser, err := core.NewBrowser(opts)
	if err != nil {
		return nil, err
	}

	p.browsers[key] = &pooledBrowser{
		browser:        browser,
		launchProxyURL: launchURL,
		lastUsedAt:     time.Now(),
	}
	p.evictLRULocked()
	return browser, nil
}

func (p *browserPool) evictLRULocked() {
	for len(p.browsers) > p.maxProcesses {
		var (
			oldestKey string
			oldest    time.Time
			found     bool
		)
		for key, entry := range p.browsers {
			if !found || entry.lastUsedAt.Before(oldest) {
				oldestKey = key
				oldest = entry.lastUsedAt
				found = true
			}
		}
		if !found {
			return
		}
		entry := p.browsers[oldestKey]
		delete(p.browsers, oldestKey)
		p.evictedLRU++
		go closePooledBrowser(entry, "lru")
	}
}

func (p *browserPool) sweepIdle() {
	defer close(p.sweeperDone)
	interval := p.idleTTL / 4
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopSweeper:
			return
		case now := <-ticker.C:
			p.mu.Lock()
			for key, entry := range p.browsers {
				if entry.browser == nil {
					continue
				}
				if now.Sub(entry.lastUsedAt) < p.idleTTL {
					continue
				}
				delete(p.browsers, key)
				p.evictedIdle++
				go closePooledBrowser(entry, "idle")
			}
			p.mu.Unlock()
		}
	}
}

func closePooledBrowser(entry *pooledBrowser, reason string) {
	if entry == nil || entry.browser == nil {
		return
	}
	if err := entry.browser.Close(); err != nil {
		logrus.WithError(err).WithField("evict_reason", reason).Debug("Browser pool: close evicted browser failed")
	}
}

func (p *browserPool) dropLaneCookies(ctx context.Context, engineName string, q core.Query) {
	if p == nil || p.laneStore == nil {
		return
	}
	laneKey := core.ProxyLaneKeyForTenant(engineName, core.TenantFromContext(ctx), q, q.ProxyURL)
	p.laneStore.DropCookies(laneKey)
}

func (p *browserPool) laneStats() core.LaneStats {
	if p == nil || p.laneStore == nil {
		return core.LaneStats{}
	}
	return p.laneStore.Stats()
}

func (p *browserPool) browserStats() core.BrowserPoolStats {
	if p == nil {
		return core.BrowserPoolStats{}
	}
	p.mu.Lock()
	active := 0
	for _, entry := range p.browsers {
		if entry.browser != nil {
			active++
		}
	}
	stats := core.BrowserPoolStats{
		Active:      active,
		Max:         p.maxProcesses,
		EvictedLRU:  p.evictedLRU,
		EvictedIdle: p.evictedIdle,
	}
	p.mu.Unlock()
	return stats
}

func (p *browserPool) close() error {
	if p == nil {
		return nil
	}
	close(p.stopSweeper)
	<-p.sweeperDone

	p.mu.Lock()
	entries := make([]*pooledBrowser, 0, len(p.browsers))
	for key, entry := range p.browsers {
		entries = append(entries, entry)
		delete(p.browsers, key)
	}
	p.mu.Unlock()

	var closeErr error
	for _, entry := range entries {
		if entry == nil || entry.browser == nil {
			continue
		}
		if err := entry.browser.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

type pooledBrowserEngine struct {
	name    string
	limiter *rate.Limiter
	opts    core.SearchEngineOptions
	factory func(core.Browser, core.SearchEngineOptions) core.SearchEngine
	pool    *browserPool

	reportLaneStats bool
}

// parsableEngine wraps pooledBrowserEngine and additionally satisfies
// core.HTMLParser for engines that have a stateless HTML parse function.
type parsableEngine struct {
	*pooledBrowserEngine
	parseHTMLFn func(io.Reader) ([]core.SearchResult, error)
}

func (e *parsableEngine) ParseHTML(r io.Reader) ([]core.SearchResult, error) {
	return e.parseHTMLFn(r)
}

func (e *pooledBrowserEngine) Search(ctx context.Context, q core.Query) ([]core.SearchResult, error) {
	engine, err := e.resolveEngine(q)
	if err != nil {
		return nil, err
	}
	return engine.Search(ctx, q)
}

func (e *pooledBrowserEngine) SearchImage(ctx context.Context, q core.Query) ([]core.SearchResult, error) {
	engine, err := e.resolveEngine(q)
	if err != nil {
		return nil, err
	}
	return engine.SearchImage(ctx, q)
}

func (e *pooledBrowserEngine) IsInitialized() bool {
	return true
}

func (e *pooledBrowserEngine) Name() string {
	return e.name
}

func (e *pooledBrowserEngine) GetRateLimiter() *rate.Limiter {
	return e.limiter
}

func (e *pooledBrowserEngine) DropProxyLaneCookies(ctx context.Context, q core.Query) {
	e.pool.dropLaneCookies(ctx, e.name, q)
}

func (e *pooledBrowserEngine) ProxyLaneStats() core.LaneStats {
	if !e.reportLaneStats {
		return core.LaneStats{}
	}
	return e.pool.laneStats()
}

func (e *pooledBrowserEngine) BrowserPoolStats() core.BrowserPoolStats {
	if !e.reportLaneStats {
		return core.BrowserPoolStats{}
	}
	return e.pool.browserStats()
}

// resolveEngine builds a fresh engine wrapper around the pool-resolved Browser.
// The wrapper is intentionally not cached: pool eviction can replace the Chrome
// behind a key, and a cached engine would carry a stale Browser value (closed
// connection, dead browserAddr). Engines are thin wrappers, so per-call
// construction is cheap.
func (e *pooledBrowserEngine) resolveEngine(q core.Query) (core.SearchEngine, error) {
	browser, err := e.pool.get(q.ProxyURL)
	if err != nil {
		return nil, err
	}
	return e.factory(*browser, e.opts), nil
}

type browserEngineSpec struct {
	name        string
	opts        core.SearchEngineOptions
	factory     func(core.Browser, core.SearchEngineOptions) core.SearchEngine
	parseHTMLFn func(io.Reader) ([]core.SearchResult, error)
}

func browserEngineSpecs() []browserEngineSpec {
	return []browserEngineSpec{
		{
			name: "google",
			opts: config.GoogleConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return google.New(browser, opts)
			},
			parseHTMLFn: google.ParseHTML,
		},
		{
			name: "yandex",
			opts: config.YandexConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return yandex.New(browser, opts)
			},
			parseHTMLFn: yandex.ParseHTML,
		},
		{
			name: "baidu",
			opts: config.BaiduConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return baidu.New(browser, opts)
			},
			parseHTMLFn: baidu.ParseHTML,
		},
		{
			name: "bing",
			opts: config.BingConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return bing.New(browser, opts)
			},
			parseHTMLFn: bing.ParseHTML,
		},
		{
			name: "duckduckgo",
			opts: config.DuckDuckGoConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return duckduckgo.New(browser, opts)
			},
			parseHTMLFn: duckduckgo.ParseHTML,
		},
		{
			name: "ecosia",
			opts: config.EcosiaConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return ecosia.New(browser, opts)
			},
			parseHTMLFn: ecosia.ParseHTML,
		},
	}
}

func buildBrowserEngines(baseOpts core.BrowserOpts, proxyCfg core.ProxyConfig) ([]core.SearchEngine, func() error, core.BrowserResolver, error) {
	launchProxyURL := ""
	if strings.TrimSpace(proxyCfg.Proxies.Global) != "" && !proxyCfg.Proxies.AllowRequestProxyURL {
		launchProxyURL = proxyCfg.Proxies.Global
	}
	var laneStore *core.LaneStore
	if proxyCfg.Proxies.Lanes.Enabled {
		laneStore = core.NewLaneStore(proxyCfg.Proxies.Lanes.MaxLanes)
	}
	maxProcesses := config.App.MaxProcesses
	if maxProcesses <= 0 {
		maxProcesses = 4
	}
	idleTTL := config.App.IdleTTL
	if idleTTL < 0 {
		idleTTL = 0
	}
	pool := newBrowserPool(baseOpts, launchProxyURL, laneStore, maxProcesses, idleTTL)
	specs := browserEngineSpecs()

	engines := make([]core.SearchEngine, 0, len(specs))
	for idx, spec := range specs {
		policy := resolveEngineProxyPolicy(proxyCfg, spec.name)
		if err := validateBrowserProxyPolicy(proxyCfg, policy); err != nil {
			return nil, nil, nil, fmt.Errorf("browser proxy validation failed for engine %s: %w", spec.name, err)
		}

		opts := spec.opts
		opts.Init()
		base := &pooledBrowserEngine{
			name:            spec.name,
			limiter:         rate.NewLimiter(rate.Every(opts.GetRatelimit()), opts.RateBurst),
			opts:            opts,
			factory:         spec.factory,
			pool:            pool,
			reportLaneStats: idx == 0,
		}
		if spec.parseHTMLFn != nil {
			engines = append(engines, &parsableEngine{pooledBrowserEngine: base, parseHTMLFn: spec.parseHTMLFn})
		} else {
			engines = append(engines, base)
		}
	}

	return engines, pool.close, pool.get, nil
}

func validateBrowserProxyPolicy(proxyCfg core.ProxyConfig, policy core.ProxyPolicy) error {
	if policy.Mode != core.ProxyModeTagPool {
		return nil
	}

	proxyURL := strings.TrimSpace(proxyCfg.Proxies.Global)
	if proxyURL != "" {
		return validateBrowserProxyURL(proxyURL)
	}

	for _, entry := range proxyCfg.Proxies.Entries {
		if !entryHasTag(entry, policy.Tag) {
			continue
		}
		if err := validateBrowserProxyURL(entry.URL); err != nil {
			return err
		}
	}

	return nil
}

func validateBrowserProxyURL(proxyURL string) error {
	// Browser startup must stop immediately on authenticated SOCKS because Chrome
	// cannot use that proxy shape reliably and retrying a different proxy hides the misconfiguration.
	if core.IsAuthenticatedSocksProxyURL(proxyURL) {
		return fmt.Errorf(
			"%w: browser runtime does not support authenticated SOCKS proxy %s",
			core.ErrProxyUnavailable,
			core.MaskProxyURL(proxyURL),
		)
	}
	return nil
}

func entryHasTag(entry core.ProxyEntryConfig, tag string) bool {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" {
		return false
	}
	for _, entryTag := range entry.Tags {
		if strings.TrimSpace(strings.ToLower(entryTag)) == tag {
			return true
		}
	}
	return false
}

func init() {
	RootCmd.AddCommand(serveCMD)
}

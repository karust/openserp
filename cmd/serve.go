package cmd

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/karust/openserp/baidu"
	"github.com/karust/openserp/bing"
	"github.com/karust/openserp/core"
	"github.com/karust/openserp/duckduckgo"
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

func (r *rawEngine) Search(q core.Query) ([]core.SearchResult, error) {
	q.Insecure = config.Server.Insecure

	switch r.name {
	case "google":
		return google.Search(q)
	case "yandex":
		return yandex.Search(q)
	case "baidu":
		return baidu.Search(q)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", r.name)
	}
}

func (r *rawEngine) SearchImage(q core.Query) ([]core.SearchResult, error) {
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

	proxyRuntime := core.ProxyRuntimeBrowser
	if config.Server.IsRawRequests {
		proxyRuntime = core.ProxyRuntimeRaw
	}

	proxyCfg, err := buildNormalizedProxyConfig(proxyRuntime)
	if err != nil {
		logrus.Errorf("invalid proxy configuration: %v", err)
		return
	}

	if config.Server.IsRawRequests {
		logrus.Warn("Browserless results are very inconsistent or may not even work!")
		serverOpts := buildServerOptions(corsCfg, proxyCfg)
		serv := core.NewServerWithOptions(config.Server.Host, config.Server.Port, serverOpts,
			&rawEngine{name: "google"},
			&rawEngine{name: "yandex"},
			&rawEngine{name: "baidu"},
		)
		if err := serv.Listen(); err != nil {
			logrus.Error(err)
		}
		return
	}

	baseOpts := core.BrowserOpts{
		IsHeadless:          !config.App.IsBrowserHead,
		IsLeakless:          config.App.IsLeakless,
		Timeout:             time.Second * time.Duration(config.App.Timeout),
		LeavePageOpen:       config.App.IsLeaveHead,
		CaptchaSolverApiKey: config.Config2Capcha.ApiKey,
		BrowserPath:         config.App.BrowserPath,
		Insecure:            config.Server.Insecure,
		UseStealth:          config.App.IsStealth,
	}
	if config.Server.IsDebug {
		baseOpts.IsHeadless = false
	}

	engines, err := buildBrowserEngines(baseOpts, proxyCfg)
	if err != nil {
		logrus.Error(err)
		return
	}

	serverOpts := buildServerOptions(corsCfg, proxyCfg)
	serv := core.NewServerWithOptions(config.Server.Host, config.Server.Port, serverOpts, engines...)
	if err := serv.Listen(); err != nil {
		logrus.Error(err)
	}
}

func buildServerOptions(corsCfg core.CORSConfig, proxyCfg core.ProxyConfig) core.ServerOptions {
	return core.ServerOptions{
		CacheTTL:              time.Duration(config.Cache.TTLSeconds) * time.Second,
		CacheMaxSize:          config.Cache.MaxSize,
		EnableCORS:            config.CORS.Enabled,
		CORS:                  corsCfg,
		AllowEndpointFallback: config.Resilience.AllowEndpointFallback,
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

type browserPool struct {
	mu      sync.Mutex
	base    core.BrowserOpts
	browser map[string]*core.Browser
}

func newBrowserPool(base core.BrowserOpts) *browserPool {
	return &browserPool{
		base:    base,
		browser: map[string]*core.Browser{},
	}
}

func (p *browserPool) get(proxyURL string) (*core.Browser, error) {
	key := strings.TrimSpace(proxyURL)
	if key == "" {
		key = "direct"
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if b, ok := p.browser[key]; ok {
		return b, nil
	}

	opts := p.base
	opts.ProxyURL = proxyURL
	b, err := core.NewBrowser(opts)
	if err != nil {
		return nil, err
	}

	// Reuse one launched browser per unique effective proxy so startup stays lazy
	// and engines with identical proxy policy don't spawn duplicate browser processes.
	p.browser[key] = b
	return b, nil
}

type pooledBrowserEngine struct {
	name    string
	limiter *rate.Limiter
	opts    core.SearchEngineOptions
	factory func(core.Browser, core.SearchEngineOptions) core.SearchEngine
	pool    *browserPool

	mu      sync.Mutex
	engines map[string]core.SearchEngine
}

func (e *pooledBrowserEngine) Search(q core.Query) ([]core.SearchResult, error) {
	engine, err := e.getOrCreate(q.ProxyURL)
	if err != nil {
		return nil, err
	}
	return engine.Search(q)
}

func (e *pooledBrowserEngine) SearchImage(q core.Query) ([]core.SearchResult, error) {
	engine, err := e.getOrCreate(q.ProxyURL)
	if err != nil {
		return nil, err
	}
	return engine.SearchImage(q)
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

func (e *pooledBrowserEngine) getOrCreate(proxyURL string) (core.SearchEngine, error) {
	key := strings.TrimSpace(proxyURL)
	if key == "" {
		key = "direct"
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if engine, ok := e.engines[key]; ok {
		return engine, nil
	}

	browser, err := e.pool.get(proxyURL)
	if err != nil {
		return nil, err
	}

	engine := e.factory(*browser, e.opts)
	e.engines[key] = engine
	return engine, nil
}

type browserEngineSpec struct {
	name    string
	opts    core.SearchEngineOptions
	factory func(core.Browser, core.SearchEngineOptions) core.SearchEngine
}

func browserEngineSpecs() []browserEngineSpec {
	return []browserEngineSpec{
		{
			name: "google",
			opts: config.GoogleConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return google.New(browser, opts)
			},
		},
		{
			name: "yandex",
			opts: config.YandexConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return yandex.New(browser, opts)
			},
		},
		{
			name: "baidu",
			opts: config.BaiduConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return baidu.New(browser, opts)
			},
		},
		{
			name: "bing",
			opts: config.BingConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return bing.New(browser, opts)
			},
		},
		{
			name: "duckduckgo",
			opts: config.DuckDuckGoConfig.SearchEngineOptions,
			factory: func(browser core.Browser, opts core.SearchEngineOptions) core.SearchEngine {
				return duckduckgo.New(browser, opts)
			},
		},
	}
}

func buildBrowserEngines(baseOpts core.BrowserOpts, proxyCfg core.ProxyConfig) ([]core.SearchEngine, error) {
	pool := newBrowserPool(baseOpts)
	specs := browserEngineSpecs()

	engines := make([]core.SearchEngine, 0, len(specs))
	for _, spec := range specs {
		policy := resolveEngineProxyPolicy(proxyCfg, spec.name)
		if err := validateBrowserProxyPolicy(proxyCfg, policy); err != nil {
			return nil, fmt.Errorf("browser proxy validation failed for engine %s: %w", spec.name, err)
		}

		opts := spec.opts
		opts.Init()
		engines = append(engines, &pooledBrowserEngine{
			name:    spec.name,
			limiter: rate.NewLimiter(rate.Every(opts.GetRatelimit()), opts.RateBurst),
			opts:    opts,
			factory: spec.factory,
			pool:    pool,
			engines: map[string]core.SearchEngine{},
		})
	}

	return engines, nil
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

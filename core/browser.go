package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
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
	// WaitLoadTime is an additional fixed wait after load/idle checks.
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
	// UseStealth enables go-rod stealth page creation.
	UseStealth bool
	// UserAgent optionally overrides browser-reported user agent during emulation.
	UserAgent string
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

// Browser wraps a launched Chromium instance used by engine implementations.
type Browser struct {
	BrowserOpts
	browserAddr   string
	conn          *browserConnection
	CaptchaSolver *CaptchaSolver
}

type browserConnection struct {
	mu              sync.Mutex
	browser         *rod.Browser
	cachedUserAgent string
}

// NewBrowser launches a new Chromium process via Rod launcher and returns a
// Browser wrapper configured with proxy and captcha solver settings.
func NewBrowser(opts BrowserOpts) (*Browser, error) {
	opts.Check()
	logrus.WithField("browser_options", fmt.Sprintf("%+v", opts)).Debug("Browser options")

	path, err := resolveBrowserBinaryPath(opts.BrowserPath, launcher.LookPath)
	if err != nil {
		return nil, err
	}

	// Create launcher
	l := launcher.New().Leakless(opts.IsLeakless).Headless(opts.IsHeadless).Set("disable-blink-features", "AutomationControlled").
		Delete("enable-automation")
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

func (b *Browser) connectBrowser() (*rod.Browser, string, error) {
	browser := b.newRodBrowser()
	if err := browser.Connect(); err != nil {
		return nil, "", err
	}

	// Keep cert handling on the persistent browser session.
	// Proxy runtime can surface MITM certs, and insecure mode is explicit opt-in.
	if b.ProxyURL != "" || b.Insecure {
		if err := browser.IgnoreCertErrors(true); err != nil {
			return nil, "", err
		}
	}

	version, err := browser.Version()
	if err != nil {
		return nil, "", fmt.Errorf("read browser version: %w", err)
	}
	ua := strings.ReplaceAll(version.UserAgent, "HeadlessChrome/", "Chrome/")
	if overrideUA := strings.TrimSpace(b.UserAgent); overrideUA != "" {
		ua = overrideUA
	}

	return browser, ua, nil
}

func (b *Browser) ensureConnectedBrowser(ctx context.Context, forceReconnect bool) (*rod.Browser, string, error) {
	if b == nil || b.browserAddr == "" {
		return nil, "", fmt.Errorf("browser is not initialized")
	}

	state := b.connectionState()
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.browser == nil || forceReconnect {
		connected, ua, err := b.connectBrowser()
		if err != nil {
			return nil, "", err
		}
		state.browser = connected
		state.cachedUserAgent = ua
		return state.browser, ua, nil
	}

	if _, err := state.browser.Version(); err != nil {
		WithRequest(ctx).WithError(err).Debug("Browser ping failed, reconnecting")
		connected, ua, reconnectErr := b.connectBrowser()
		if reconnectErr != nil {
			return nil, "", reconnectErr
		}
		state.browser = connected
		state.cachedUserAgent = ua
	}

	return state.browser, state.cachedUserAgent, nil
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

// Navigate connects to Chromium, creates a page, applies stealth/emulation and
// proxy auth, then navigates to URL. It returns an initialized page ready for
// selector queries, or an error when browser setup/navigation fails.
func (b *Browser) Navigate(ctx context.Context, URL string) (*rod.Page, error) {
	ctx = EnsureContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	WithRequest(ctx).WithField("url", URL).Debug("Navigate")

	browser, ua, err := b.ensureConnectedBrowser(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("browser connect failed: %w", err)
	}

	page, browserContextID, err := createIsolatedPage(browser)
	if err != nil {
		// Single-shot reconnect for stale websocket sessions.
		browser, ua, err = b.ensureConnectedBrowser(ctx, true)
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

	if b.UseStealth {
		if _, err := page.EvalOnNewDocument(stealth.JS); err != nil {
			closeOnErr()
			return nil, fmt.Errorf("create stealth page failed: %w", err)
		}
	}

	if err := page.Emulate(devices.Device{
		AcceptLanguage: b.LanguageCode,
		UserAgent:      ua,
	}); err != nil {
		closeOnErr()
		return nil, fmt.Errorf("emulate page failed: %w", err)
	}

	if !b.UseStealth {
		if err := (proto.EmulationSetDeviceMetricsOverride{
			Width:             1920,
			Height:            1080,
			DeviceScaleFactor: 1,
			Mobile:            false,
			ScreenWidth:       &[]int{1920}[0],
			ScreenHeight:      &[]int{1080}[0],
		}).Call(page); err != nil {
			closeOnErr()
			return nil, fmt.Errorf("set device metrics failed: %w", err)
		}
	}

	page = page.Context(ctx)
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

	if err := SleepContext(ctx, b.WaitLoadTime); err != nil {
		closeOnErr()
		return nil, err
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

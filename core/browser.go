package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
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
	browser       *rod.Browser
	CaptchaSolver *CaptchaSolver
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

	b := Browser{BrowserOpts: opts}
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

// Navigate connects to Chromium, creates a page, applies stealth/emulation and
// proxy auth, then navigates to URL. It returns an initialized page ready for
// selector queries, or an error when browser setup/navigation fails.
func (b *Browser) Navigate(ctx context.Context, URL string) (*rod.Page, error) {
	ctx = EnsureContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	WithRequest(ctx).WithField("url", URL).Debug("Navigate to")

	browser := rod.New().ControlURL(b.browserAddr).Timeout(b.Timeout)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("browser connect failed: %w", err)
	}
	b.browser = browser
	if err := b.browser.SetCookies(nil); err != nil {
		return nil, fmt.Errorf("browser cookie reset failed: %w", err)
	}

	// Handle proxy authentication before any navigations
	if b.ProxyURL != "" {
		proxyUrl, _ := url.Parse(b.ProxyURL)

		// Always ignore certificate errors when using proxies
		// This fixes the ERR_CERT_AUTHORITY_INVALID error for SOCKS5 proxies
		if err := b.browser.IgnoreCertErrors(true); err != nil {
			return nil, fmt.Errorf("configure proxy cert handling failed: %w", err)
		}

		if proxyUrl.User != nil && (proxyUrl.Scheme == "http" || proxyUrl.Scheme == "https") {
			username := proxyUrl.User.Username()
			password, _ := proxyUrl.User.Password()
			// Launch auth handler before any navigation occurs
			go func() {
				if err := b.browser.HandleAuth(username, password)(); err != nil {
					WithRequest(ctx).WithError(err).Debug("Proxy auth handler stopped")
				}
			}()
		} else if proxyUrl.User != nil && (proxyUrl.Scheme == "socks5" || proxyUrl.Scheme == "socks5h") {
			// This callback handles HTTP proxy auth challenges; it doesn't authenticate SOCKS proxies.
			logrus.Debug("SOCKS proxy credentials are not handled by browser auth callback")
		}
	} else if b.Insecure {
		// Still respect the insecure flag if no proxy is used
		if err := b.browser.IgnoreCertErrors(true); err != nil {
			return nil, fmt.Errorf("configure insecure mode failed: %w", err)
		}
	}

	version, err := b.browser.Version()
	if err != nil {
		return nil, fmt.Errorf("read browser version failed: %w", err)
	}
	ua := strings.ReplaceAll(version.UserAgent, "HeadlessChrome/", "Chrome/")

	var page *rod.Page

	if b.UseStealth {
		page, err = stealth.Page(b.browser)
		if err != nil {
			return nil, fmt.Errorf("create stealth page failed: %w", err)
		}
	} else {
		page, err = b.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return nil, fmt.Errorf("create page failed: %w", err)
		}
	}

	// From here on, any error path must close the page to avoid leaking tabs
	// when the caller context is canceled or navigation fails.
	closeOnErr := func() {
		if cerr := page.Close(); cerr != nil {
			WithRequest(ctx).WithError(cerr).Debug("Close page after navigate error failed")
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

	browser := b.browser
	if browser == nil {
		browser = rod.New().ControlURL(b.browserAddr)
		if b.Timeout > 0 {
			browser = browser.Timeout(b.Timeout)
		}
		if err := browser.Connect(); err != nil {
			if isBrowserClosedError(err) {
				return nil
			}
			return err
		}
	}

	b.browser = nil
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
	closeCtx, cancel := context.WithTimeout(EnsureContext(ctx), timeout)
	defer cancel()
	return page.Context(closeCtx).Close()
}

// RecoverEnginePanic converts recovered panics to a typed engine error and
// logs stack trace with engine context.
func RecoverEnginePanic(engine string, recovered interface{}, logger *EngineLogger) error {
	return RecoverEnginePanicWithContext(nil, engine, recovered, logger)
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

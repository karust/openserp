package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
)

type BrowserOpts struct {
	IsHeadless          bool          // Use browser interface
	IsLeakless          bool          // Force to kill browser
	Timeout             time.Duration // Timeout
	LanguageCode        string
	WaitRequests        bool          // Wait requests to complete after navigation
	LeavePageOpen       bool          // Leave pages and browser open
	WaitLoadTime        time.Duration // Time to wait till page loads
	CaptchaSolverApiKey string        // 2Captcha api key
	BrowserPath         string        // Explicit browser executable path
	ProxyURL            string        // Proxy URL
	Insecure            bool          // Allow insecure TLS connections
	UseStealth          bool          // Use go-rod stealth plugin

}

// Initialize browser parameters with default values if they are not set
func (o *BrowserOpts) Check() {
	if o.Timeout == 0 {
		o.Timeout = time.Second * 30
	}

	if o.WaitLoadTime == 0 {
		o.WaitLoadTime = time.Second * 2
	}
}

type Browser struct {
	BrowserOpts
	browserAddr   string
	browser       *rod.Browser
	CaptchaSolver *CaptchaSolver
}

func NewBrowser(opts BrowserOpts) (*Browser, error) {
	opts.Check()
	logrus.Debugf("Browser options: %+v", opts)

	path, err := resolveBrowserBinaryPath(opts.BrowserPath, launcher.LookPath)
	if err != nil {
		return nil, err
	}

	// Create launcher
	l := launcher.New().Leakless(opts.IsLeakless).Headless(opts.IsHeadless).Set("disable-blink-features", "AutomationControlled").
		Delete("enable-automation")
	if path != "" {
		logrus.Debugf("Using browser binary: %s", path)
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
		logrus.Debugf("Setting up proxy: %s", MaskProxyURL(proxyStr))
		l = l.Proxy(proxyStr)

		// Check if proxy has auth credentials
		if proxyUrl.User != nil {
			username := proxyUrl.User.Username()
			logrus.Debugf("Proxy credentials configured for %s proxy: %s:****", proxyUrl.Scheme, username)
		}
	}

	b := Browser{BrowserOpts: opts}
	b.browserAddr, err = l.Launch()

	if opts.CaptchaSolverApiKey != "" {
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

// Check whether browser instance is already created
func (b *Browser) IsInitialized() bool {
	if b.browserAddr != "" {
		return true
	} else {
		return false
	}
}

// Open URL
func (b *Browser) Navigate(URL string) (*rod.Page, error) {
	logrus.Debug("Navigate to: ", URL)

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
					logrus.Debugf("Proxy auth handler stopped: %v", err)
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
		err = page.Emulate(devices.Device{
			AcceptLanguage: b.LanguageCode,
			UserAgent:      ua,
		})
		if err != nil {
			return nil, fmt.Errorf("emulate stealth page failed: %w", err)
		}

	} else {
		page, err = b.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return nil, fmt.Errorf("create page failed: %w", err)
		}

		err = page.Emulate(devices.Device{
			AcceptLanguage: b.LanguageCode,
			UserAgent:      ua,
		})
		if err != nil {
			return nil, fmt.Errorf("emulate page failed: %w", err)
		}

		err = proto.EmulationSetDeviceMetricsOverride{
			Width:             1920,
			Height:            1080,
			DeviceScaleFactor: 1,
			Mobile:            false,
			ScreenWidth:       &[]int{1920}[0],
			ScreenHeight:      &[]int{1080}[0],
		}.Call(page)
		if err != nil {
			return nil, fmt.Errorf("set device metrics failed: %w", err)
		}
	}
	//EnableCustomStealth(page)

	timedPage := page.Timeout(b.Timeout)

	err = timedPage.Navigate(URL)
	if err != nil {
		return nil, err
	}

	// Avoid panics from MustWaitLoad when the target navigates/closes mid-wait
	if werr := timedPage.WaitLoad(); werr != nil {
		if errors.Is(werr, context.DeadlineExceeded) {
			// Some engines keep loading background resources while the DOM is already usable.
			// Treat load timeout as non-fatal and let engine-specific selector timeouts decide.
			logrus.Debugf("WaitLoad timed out after %s; continuing with partial page state", b.Timeout)
		} else {
			logrus.Debugf("WaitLoad returned early: %v", werr)
		}
	}

	// may cause bugs with google
	if b.WaitRequests {
		wait := timedPage.WaitRequestIdle(300*time.Millisecond, nil, nil, nil)
		wait()
	}

	time.Sleep(b.WaitLoadTime)
	return page, nil
}

func (b *Browser) Close() error {
	return b.browser.Close()
}

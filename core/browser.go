package core

import (
	"fmt"
	"net/url"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
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
	ProxyURL            string        // Proxy URL
	Insecure            bool          // Allow insecure TLS connections

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

	path, has := launcher.LookPath()
	logrus.Debug("Browser found: ", has)

	// Create launcher
	l := launcher.New().Bin(path).Leakless(opts.IsLeakless).Headless(opts.IsHeadless)

	// Configure proxy if specified
	if opts.ProxyURL != "" {
		proxyUrl, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %v", err)
		}

		// Make sure the proxy URL includes the scheme when passed to launcher
		// This ensures proper handling of SOCKS5 proxies
		proxyStr := proxyUrl.String()
		logrus.Debugf("Setting up proxy: %s", proxyStr)
		l = l.Proxy(proxyStr)

		// Check if proxy has auth credentials
		if proxyUrl.User != nil {
			username := proxyUrl.User.Username()
			logrus.Debugf("Using proxy authentication: %s:****", username)
			// We'll handle auth in the Navigate method
		}
	}

	var err error
	b := Browser{BrowserOpts: opts}
	b.browserAddr, err = l.Launch()

	if opts.CaptchaSolverApiKey != "" {
		b.CaptchaSolver = NewSolver(opts.CaptchaSolverApiKey)
		logrus.Debug("Captcha solver initialized")
	}

	return &b, err
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

	b.browser = rod.New().ControlURL(b.browserAddr)
	b.browser.MustConnect()
	b.browser.SetCookies(nil)

	// Handle proxy authentication before any navigations
	if b.ProxyURL != "" {
		proxyUrl, _ := url.Parse(b.ProxyURL)

		// Always ignore certificate errors when using proxies
		// This fixes the ERR_CERT_AUTHORITY_INVALID error for SOCKS5 proxies
		b.browser.MustIgnoreCertErrors(true)

		if proxyUrl.User != nil {
			username := proxyUrl.User.Username()
			password, _ := proxyUrl.User.Password()
			// Launch auth handler before any navigation occurs
			go b.browser.MustHandleAuth(username, password)()
		}
	} else if b.Insecure {
		// Still respect the insecure flag if no proxy is used
		b.browser.MustIgnoreCertErrors(true)
	}

	page := stealth.MustPage(b.browser)
	page.MustEmulate(devices.Device{
		AcceptLanguage: b.LanguageCode,
	})

	err := page.Navigate(URL)
	if err != nil {
		return nil, err
	}

	wait := page.MustWaitRequestIdle()
	// may cause bugs with google
	if b.WaitRequests {
		wait()
	}

	return page, nil
}

func (b *Browser) Close() error {
	return b.browser.Close()
}

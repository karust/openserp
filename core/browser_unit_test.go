package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	browserprofile "github.com/karust/openserp/core/browser"
)

func TestResolveBrowserBinaryPathPrefersExplicit(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "chromium")
	if err := os.WriteFile(bin, []byte("test"), 0o755); err != nil {
		t.Fatalf("write temp browser binary: %v", err)
	}

	path, err := resolveBrowserBinaryPath(bin, func() (string, bool) {
		return "/should/not/be/used", true
	})
	if err != nil {
		t.Fatalf("resolve browser path: %v", err)
	}
	if path != bin {
		t.Fatalf("expected explicit browser path %q, got %q", bin, path)
	}
}

func TestResolveBrowserBinaryPathFallsBackToLookPath(t *testing.T) {
	want := "/usr/bin/chromium"
	path, err := resolveBrowserBinaryPath("", func() (string, bool) {
		return want, true
	})
	if err != nil {
		t.Fatalf("resolve browser path: %v", err)
	}
	if path != want {
		t.Fatalf("expected lookPath result %q, got %q", want, path)
	}
}

func TestResolveBrowserBinaryPathReturnsEmptyWhenNothingResolved(t *testing.T) {
	path, err := resolveBrowserBinaryPath("", func() (string, bool) {
		return "", false
	})
	if err != nil {
		t.Fatalf("resolve browser path: %v", err)
	}
	if path != "" {
		t.Fatalf("expected empty path, got %q", path)
	}
}

func TestResolveBrowserBinaryPathRejectsInvalidExplicit(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveBrowserBinaryPath(dir, func() (string, bool) {
		return "", false
	}); err == nil {
		t.Fatalf("expected error when explicit browser_path points to a directory")
	}
}

func TestBrowserLaunchLanguageIsProcessStable(t *testing.T) {
	tests := []struct {
		name string
		opts BrowserOpts
		want string
	}{
		{name: "default locale", opts: BrowserOpts{}, want: "en-US"},
		{name: "request hint does not change process locale", opts: BrowserOpts{LanguageCode: "de"}, want: "en-US"},
		{name: "regional hint does not change process locale", opts: BrowserOpts{LanguageCode: "en-GB"}, want: "en-US"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := browserLaunchLanguage(tt.opts); got != tt.want {
				t.Fatalf("browserLaunchLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProfileNavigatorLanguagesStripsHeaderWeights(t *testing.T) {
	profile := browserprofile.Profile{
		AcceptLanguage: "en-US,en;q=0.9",
		NavigatorLangs: []string{"en-US"},
	}

	got := profileNavigatorLanguages(profile)
	want := []string{"en-US", "en"}
	if len(got) != len(want) {
		t.Fatalf("profileNavigatorLanguages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("profileNavigatorLanguages() = %v, want %v", got, want)
		}
	}
}

func TestProfileNavigatorLanguagesForRuntime(t *testing.T) {
	profile := browserprofile.Profile{
		AcceptLanguage: "en-US,en;q=0.9",
		NavigatorLangs: []string{"en-US"},
	}

	linuxHeadless := profileNavigatorLanguagesForRuntime(profile, "linux", true)
	if len(linuxHeadless) != 1 || linuxHeadless[0] != "en-US" {
		t.Fatalf("linux headless languages = %v, want [en-US]", linuxHeadless)
	}

	windowsHeadless := profileNavigatorLanguagesForRuntime(profile, "windows", true)
	if len(windowsHeadless) != 2 || windowsHeadless[0] != "en-US" || windowsHeadless[1] != "en" {
		t.Fatalf("windows headless languages = %v, want [en-US en]", windowsHeadless)
	}
}

func TestMinPositiveDuration(t *testing.T) {
	tests := []struct {
		name string
		a    time.Duration
		b    time.Duration
		want time.Duration
	}{
		{name: "both positive", a: 30 * time.Second, b: 2 * time.Second, want: 2 * time.Second},
		{name: "first unset", a: 0, b: 2 * time.Second, want: 2 * time.Second},
		{name: "second unset", a: 30 * time.Second, b: 0, want: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minPositiveDuration(tt.a, tt.b); got != tt.want {
				t.Fatalf("minPositiveDuration() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestApplyProfileLanguageHintRewritesTimezone(t *testing.T) {
	profile := browserprofile.Profile{
		AcceptLanguage: "ru-RU,ru;q=0.9",
		NavigatorLangs: []string{"ru-RU"},
		Locale:         "ru-RU",
		Timezone:       "Europe/Moscow",
	}
	got := applyProfileLanguageHint(profile, "de-DE")
	if got.Timezone != "Europe/Berlin" {
		t.Fatalf("expected timezone Europe/Berlin, got %q", got.Timezone)
	}
}

func TestRemoveChromeBrand(t *testing.T) {
	profile := browserprofile.Profile{
		UACHBrands: []browserprofile.BrandVersion{
			{Brand: "Not_A Brand", Version: "24"},
			{Brand: "Chromium", Version: "136"},
			{Brand: "Google Chrome", Version: "136"},
		},
		UACHFullVerList: []browserprofile.BrandVersion{
			{Brand: "Chromium", Version: "136.0.0.0"},
			{Brand: "Google Chrome", Version: "136.0.0.0"},
		},
	}

	got := removeChromeBrand(profile)
	for _, brand := range got.UACHBrands {
		if brand.Brand == "Google Chrome" {
			t.Fatal("expected Google Chrome brand to be removed from UACHBrands")
		}
	}
	for _, brand := range got.UACHFullVerList {
		if brand.Brand == "Google Chrome" {
			t.Fatal("expected Google Chrome brand to be removed from UACHFullVerList")
		}
	}
	if len(got.UACHBrands) != 2 || len(got.UACHFullVerList) != 1 {
		t.Fatalf("unexpected brand counts: brands=%d fullList=%d", len(got.UACHBrands), len(got.UACHFullVerList))
	}
}

func TestApplyProfileLanguageHint(t *testing.T) {
	base := browserprofile.Profile{
		AcceptLanguage: "en-US,en;q=0.9",
		NavigatorLangs: []string{"en-US"},
		Locale:         "en-US",
	}

	tests := []struct {
		name   string
		lang   string
		wantAL string
		wantL  string
	}{
		{
			name:   "empty hint keeps profile",
			lang:   "",
			wantAL: "en-US,en;q=0.9",
			wantL:  "en-US",
		},
		{
			name:   "same language without region keeps profile",
			lang:   "en",
			wantAL: "en-US,en;q=0.9",
			wantL:  "en-US",
		},
		{
			name:   "new language overrides locale headers",
			lang:   "de",
			wantAL: "de-DE,de;q=0.9",
			wantL:  "de-DE",
		},
		{
			name:   "explicit region overrides locale headers",
			lang:   "en-GB",
			wantAL: "en-GB,en;q=0.9",
			wantL:  "en-GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyProfileLanguageHint(base, tt.lang)
			if got.AcceptLanguage != tt.wantAL {
				t.Fatalf("AcceptLanguage = %q, want %q", got.AcceptLanguage, tt.wantAL)
			}
			if got.Locale != tt.wantL {
				t.Fatalf("Locale = %q, want %q", got.Locale, tt.wantL)
			}
			if len(got.NavigatorLangs) != 1 || got.NavigatorLangs[0] != tt.wantL {
				t.Fatalf("NavigatorLangs = %v, want [%q]", got.NavigatorLangs, tt.wantL)
			}
		})
	}
}

func TestEnsureConnectedBrowserRelaunchesUnreachableChrome(t *testing.T) {
	for _, closed := range []bool{false, true} {
		launches := 0
		b := &Browser{
			browserAddr: "ws://127.0.0.1:1/devtools/browser/dead",
			conn:        &browserConnection{closed: closed},
			launch: func() (string, error) {
				launches++
				return "", errors.New("launch failed")
			},
		}
		if _, err := b.ensureConnectedBrowser(context.Background(), false); err == nil {
			t.Fatalf("closed=%v: expected connect error", closed)
		}
		want := 1
		if closed {
			want = 0
		}
		if launches != want {
			t.Fatalf("closed=%v: expected %d launches, got %d", closed, want, launches)
		}
	}
}

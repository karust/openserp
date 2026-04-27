package core

import (
	"os"
	"path/filepath"
	"testing"

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

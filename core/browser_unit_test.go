package core

import (
	"os"
	"path/filepath"
	"testing"
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

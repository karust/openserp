package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func mkProfile(t *testing.T, root, name string, age time.Duration, lock string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if lock != "" {
		if err := os.Symlink(lock, filepath.Join(dir, "SingletonLock")); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestNewUserDataDirCreatesUniqueDirsInRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "profiles")
	a, err := newUserDataDir(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newUserDataDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("expected distinct profile dirs, got %q twice", a)
	}
	for _, dir := range []string{a, b} {
		if filepath.Dir(dir) != root || !isOpenSERPUserDataDirName(filepath.Base(dir)) || !exists(dir) {
			t.Fatalf("unexpected profile dir %q", dir)
		}
	}
}

func TestNewUserDataDirFailsWhenRootUnwritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newUserDataDir(filepath.Join(file, "sub")); err == nil {
		t.Fatal("expected error when user_data_dir cannot be created")
	}
}

func TestCleanStaleUserDataDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stale profile cleanup is disabled on Windows")
	}
	host, _ := os.Hostname()
	root := t.TempDir()
	live := mkProfile(t, root, "openserp-live", time.Hour, host+"-"+strconv.Itoa(os.Getpid()))
	foreignHost := mkProfile(t, root, "openserp-foreign", time.Hour, "other-host-"+strconv.Itoa(os.Getpid()))
	deadPID := mkProfile(t, root, "openserp-dead", time.Hour, host+"-2147483646")
	noLock := mkProfile(t, root, "openserp-nolock", time.Hour, "")
	legacy := mkProfile(t, root, "0123456789abcdef", time.Hour, "")
	starting := mkProfile(t, root, "openserp-starting", time.Second, "")
	unrelated := mkProfile(t, root, "documents", time.Hour, "")
	plainFile := filepath.Join(root, "openserp-file")
	if err := os.WriteFile(plainFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := CleanStaleUserDataDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 4 {
		t.Fatalf("removed = %d, want 4", removed)
	}
	for _, gone := range []string{foreignHost, deadPID, noLock, legacy} {
		if exists(gone) {
			t.Errorf("%s should have been removed", gone)
		}
	}
	for _, kept := range []string{live, starting, unrelated, plainFile} {
		if !exists(kept) {
			t.Errorf("%s should have been kept", kept)
		}
	}
}

func TestCleanStaleUserDataDirsMissingRoot(t *testing.T) {
	removed, err := CleanStaleUserDataDirs(filepath.Join(t.TempDir(), "missing"))
	if err != nil || removed != 0 {
		t.Fatalf("got (%d, %v), want (0, nil)", removed, err)
	}
}

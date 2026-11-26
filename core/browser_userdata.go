package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/sirupsen/logrus"
)

const (
	// userDataDirPrefix names the per-launch Chromium profile directories
	// created by OpenSERP, so a sweep never touches unrelated folders.
	userDataDirPrefix = "openserp-"
	// staleUserDataMinAge protects a profile whose Chrome has not written its
	// singleton lock yet (a browser that is still starting up).
	staleUserDataMinAge = time.Minute
)

// legacyUserDataDirName matches the 16-hex-char names Rod picked for profile
// directories before OpenSERP started naming them itself.
var legacyUserDataDirName = regexp.MustCompile(`^[0-9a-f]{16}$`)

// readlink is replaced in tests to simulate lock inspection failures.
var readlink = os.Readlink

// userDataRoot returns the directory that holds per-launch Chromium profiles.
func userDataRoot(configured string) string {
	if root := strings.TrimSpace(configured); root != "" {
		return root
	}
	return launcher.DefaultUserDataDirPrefix
}

// newUserDataDir creates a fresh profile directory below root. Failing here,
// instead of inside Chrome, surfaces an unwritable user_data_dir as a clear
// startup error.
func newUserDataDir(root string) (string, error) {
	root = userDataRoot(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create user_data_dir %q: %w", root, err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve user_data_dir %q: %w", root, err)
	}
	dir, err := os.MkdirTemp(abs, userDataDirPrefix)
	if err != nil {
		return "", fmt.Errorf("create browser profile in user_data_dir %q: %w", root, err)
	}
	return dir, nil
}

// CheckUserDataDir reports whether a browser profile can be created below root,
// for callers that launch Chrome lazily. An empty root selects Rod's default
// location.
func CheckUserDataDir(root string) error {
	dir, err := newUserDataDir(root)
	if err != nil {
		return err
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("remove probe profile in user_data_dir %q: %w", userDataRoot(root), err)
	}
	return nil
}

// CleanStaleUserDataDirs removes browser profiles that earlier OpenSERP runs
// left below root (crash, SIGKILL, container stop). Profiles whose Chrome is
// still running, or is locked by another host, are kept. An empty root selects
// Rod's default location.
// It returns how many directories were removed.
func CleanStaleUserDataDirs(root string) (int, error) {
	// Chrome on Windows has no SingletonLock symlink to prove a profile is idle.
	if runtime.GOOS == "windows" {
		return 0, nil
	}
	root = userDataRoot(root)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read user_data_dir %q: %w", root, err)
	}

	hostname, _ := os.Hostname()
	removed := 0
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !isOpenSERPUserDataDirName(name) {
			continue
		}
		dir := filepath.Join(root, name)
		if userDataDirInUse(dir, hostname, time.Now()) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		logrus.WithFields(logrus.Fields{"dir": root, "removed": removed}).Info("Removed stale browser profiles")
	}
	return removed, errors.Join(errs...)
}

func isOpenSERPUserDataDirName(name string) bool {
	return strings.HasPrefix(name, userDataDirPrefix) || legacyUserDataDirName.MatchString(name)
}

// userDataDirInUse reports whether dir may belong to a live Chrome. Chrome
// records "<hostname>-<pid>" in the SingletonLock symlink while it runs.
func userDataDirInUse(dir, hostname string, now time.Time) bool {
	target, err := readlink(filepath.Join(dir, "SingletonLock"))
	if err != nil {
		// Only a missing or non-symlink lock proves Chrome is not running.
		// Other errors (for example I/O) leave the profile's state unknown.
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.EINVAL) {
			return true
		}
		if info, statErr := os.Stat(dir); statErr == nil {
			return now.Sub(info.ModTime()) < staleUserDataMinAge
		}
		return false
	}
	idx := strings.LastIndex(target, "-")
	if idx < 0 {
		return false
	}
	pid, err := strconv.Atoi(target[idx+1:])
	if err != nil || pid <= 0 {
		return false
	}
	// The PID of another host (for example a container sharing the volume)
	// cannot be checked from here, so its Chrome may still be running.
	if target[:idx] != hostname {
		return true
	}
	return processAlive(pid)
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseListenAddr(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		port        int
		wantNetwork string
		wantAddr    string
	}{
		{name: "tcp ipv4", host: "127.0.0.1", port: 7000, wantNetwork: "tcp", wantAddr: "127.0.0.1:7000"},
		{name: "tcp all interfaces", host: "0.0.0.0", port: 7070, wantNetwork: "tcp", wantAddr: "0.0.0.0:7070"},
		{name: "tcp empty host", host: "", port: 7000, wantNetwork: "tcp", wantAddr: ":7000"},
		{name: "unix absolute", host: "unix:/tmp/openserp.sock", port: 7000, wantNetwork: "unix", wantAddr: "/tmp/openserp.sock"},
		{name: "unix relative", host: "unix:run/openserp.sock", port: 0, wantNetwork: "unix", wantAddr: "run/openserp.sock"},
		{name: "unix missing suffix", host: "unix:/tmp/openserp"},
		{name: "unix empty path", host: "unix:"},
		{name: "unix bare suffix", host: "unix:.sock"},
		{name: "unix abstract", host: "unix:@openserp.sock"},
		{name: "unix NUL", host: "unix:/tmp/open\x00serp.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, addr, err := parseListenAddr(tt.host, tt.port)
			wantErr := tt.wantNetwork == ""
			if (err != nil) != wantErr {
				t.Fatalf("parseListenAddr(%q): err = %v, wantErr %t", tt.host, err, wantErr)
			}
			if network != tt.wantNetwork || addr != tt.wantAddr {
				t.Fatalf("got (%q, %q, %v), want (%q, %q)", network, addr, err, tt.wantNetwork, tt.wantAddr)
			}
		})
	}
}

// t.TempDir can exceed the socket path length limit for long test names.
func tempSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "osp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "openserp.sock")
}

func TestServerListenUnixSocketLifecycle(t *testing.T) {
	// Set a permissive umask in a child so the test cannot affect other tests.
	if runtime.GOOS == "linux" && os.Getenv("OPENSERP_TEST_SOCKET_UMASK") == "" {
		t.Setenv("OPENSERP_TEST_SOCKET_UMASK", "000")
		cmd := exec.CommandContext(t.Context(), "sh", "-c", `umask 000; exec "$@"`, "sh", os.Args[0], "-test.run=^"+t.Name()+"$")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("socket lifecycle under umask 000: %v\n%s", err, out)
		}
		return
	}
	sockPath := tempSocketPath(t)

	// Simulate a socket file left behind by a crashed previous run.
	stale, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()
	if _, err := os.Lstat(sockPath); err != nil {
		t.Fatalf("stale socket missing: %v", err)
	}

	srv, listenErrCh := startUnixTestServer(t, sockPath, 0)
	waitForUnixHealth(t, sockPath, listenErrCh)
	if runtime.GOOS == "linux" {
		info, err := os.Stat(sockPath)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode&0o022 != 0 {
			t.Fatalf("default socket is writable by another user: %04o", mode)
		}
	}
	_, duplicate := startUnixTestServer(t, sockPath, 0)
	select {
	case err := <-duplicate:
		if !errors.Is(err, errUnixSocketInUse) {
			t.Fatalf("expected socket already in use, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("duplicate listener did not exit")
	}
	waitForUnixHealth(t, sockPath, listenErrCh)
	if err := srv.ShutdownWithTimeout(time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-listenErrCh; err != nil {
		t.Fatalf("listen returned error: %v", err)
	}
	if _, err := os.Lstat(sockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected socket file removed after shutdown, stat err=%v", err)
	}
	_, restart := startUnixTestServer(t, sockPath, 0)
	waitForUnixHealth(t, sockPath, restart)
}

func startUnixTestServer(t *testing.T, sockPath string, mode os.FileMode) (*Server, <-chan error) {
	t.Helper()
	opts := DefaultServerOptions()
	opts.SocketMode = mode
	srv := NewServerWithOptions("unix:"+sockPath, 0, opts, &engineMock{name: "google", initialized: true})
	t.Cleanup(func() { _ = srv.ShutdownWithTimeout(time.Second) })
	listenErrCh := make(chan error, 1)
	go func() { listenErrCh <- srv.Listen() }()
	return srv, listenErrCh
}

func waitForUnixHealth(t *testing.T, sockPath string, listenErrCh <-chan error) {
	t.Helper()
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sockPath)
			},
		},
	}
	defer client.CloseIdleConnections()

	deadline := time.After(5 * time.Second)
	for {
		resp, err := client.Get("http://unix/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 from /health, got %d", resp.StatusCode)
			}
			return
		}
		select {
		case listenErr := <-listenErrCh:
			t.Fatalf("listen exited early: %v", listenErr)
		case <-deadline:
			t.Fatalf("server did not respond on unix socket: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestRemoveStaleSocketKeepsActiveSocket(t *testing.T) {
	sockPath := tempSocketPath(t)
	active, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = active.Close() }()

	if err := removeStaleSocket(sockPath); !errors.Is(err, errUnixSocketInUse) {
		t.Fatalf("expected socket already in use, got %v", err)
	}
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		t.Fatalf("active socket became unreachable: %v", err)
	}
	_ = conn.Close()
}

func TestServerListenUnixSocketKeepsRegularFile(t *testing.T) {
	sockPath := tempSocketPath(t)
	if err := os.WriteFile(sockPath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := NewServerWithOptions("unix:"+sockPath, 0, DefaultServerOptions(), &engineMock{name: "google", initialized: true})
	if err := srv.Listen(); err == nil {
		t.Fatal("expected error when socket path is a regular file")
	}
	if data, err := os.ReadFile(sockPath); err != nil || string(data) != "data" {
		t.Fatalf("regular file was modified: data=%q err=%v", data, err)
	}
}

func TestParseSocketMode(t *testing.T) {
	tests := []struct {
		in      string
		want    os.FileMode
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "0777", want: 0o777},
		{in: "660", want: 0o660},
		{in: "0o660", wantErr: true},
		{in: "0", wantErr: true},
		{in: "1777", wantErr: true},
		{in: "rw", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseSocketMode(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseSocketMode(%q) = %o, %v; want %o, wantErr %t", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestServerListenUnixSocketAppliesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX socket permissions")
	}
	sockPath := tempSocketPath(t)
	const mode = 0o777
	_, listenErrCh := startUnixTestServer(t, sockPath, mode)
	waitForUnixHealth(t, sockPath, listenErrCh)
	info, err := os.Lstat(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Fatalf("socket mode = %04o, want %04o", got, mode)
	}
}

package core

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
		wantErr     bool
	}{
		{name: "tcp ipv4", host: "127.0.0.1", port: 7000, wantNetwork: "tcp", wantAddr: "127.0.0.1:7000"},
		{name: "tcp all interfaces", host: "0.0.0.0", port: 7070, wantNetwork: "tcp", wantAddr: "0.0.0.0:7070"},
		{name: "tcp empty host", host: "", port: 7000, wantNetwork: "tcp", wantAddr: ":7000"},
		{name: "unix absolute", host: "unix:/tmp/openserp.sock", port: 7000, wantNetwork: "unix", wantAddr: "/tmp/openserp.sock"},
		{name: "unix relative", host: "unix:run/openserp.sock", port: 0, wantNetwork: "unix", wantAddr: "run/openserp.sock"},
		{name: "unix missing suffix", host: "unix:/tmp/openserp", wantErr: true},
		{name: "unix empty path", host: "unix:", wantErr: true},
		{name: "unix bare suffix", host: "unix:.sock", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, addr, err := parseListenAddr(tt.host, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got network=%q addr=%q", network, addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if network != tt.wantNetwork || addr != tt.wantAddr {
				t.Fatalf("got (%q, %q), want (%q, %q)", network, addr, tt.wantNetwork, tt.wantAddr)
			}
		})
	}
}

// shortTempDir keeps socket paths under the ~108 byte sun_path limit, which
// t.TempDir can exceed for long test names.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "osp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestServerListenUnixSocketReplacesStaleSocket(t *testing.T) {
	sockPath := filepath.Join(shortTempDir(t), "openserp.sock")

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

	engine := &engineMock{name: "google", initialized: true}
	srv := NewServerWithOptions("unix:"+sockPath, 0, DefaultServerOptions(), engine)

	listenErrCh := make(chan error, 1)
	go func() { listenErrCh <- srv.Listen() }()

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sockPath)
			},
		},
	}

	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err = client.Get("http://unix/health")
		if err == nil {
			break
		}
		select {
		case listenErr := <-listenErrCh:
			t.Fatalf("listen exited early: %v", listenErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not respond on unix socket: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /health, got %d", resp.StatusCode)
	}

	if err := srv.ShutdownWithTimeout(time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-listenErrCh; err != nil {
		t.Fatalf("listen returned error: %v", err)
	}
	if _, err := os.Lstat(sockPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected socket file removed after shutdown, stat err=%v", err)
	}
}

func TestServerListenUnixSocketKeepsRegularFile(t *testing.T) {
	sockPath := filepath.Join(shortTempDir(t), "openserp.sock")
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

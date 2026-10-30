package core

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetCustomDNSServer(t *testing.T) {
	cases := []struct {
		name, raw, want string
		wantErr         bool
	}{
		{name: "empty clears", raw: "", want: ""},
		{name: "ipv4", raw: "8.8.8.8:53", want: "8.8.8.8:53"},
		{name: "whitespace trimmed", raw: "  1.1.1.1:53\n", want: "1.1.1.1:53"},
		{name: "ipv6", raw: "[::1]:53", want: "[::1]:53"},
		{name: "missing port", raw: "8.8.8.8", wantErr: true},
		{name: "hostname rejected", raw: "dns.example.com:53", wantErr: true},
		{name: "missing host", raw: ":53", wantErr: true},
		{name: "port zero", raw: "8.8.8.8:0", wantErr: true},
		{name: "port too large", raw: "8.8.8.8:99999", wantErr: true},
		{name: "garbage", raw: "bogus", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCustomDNS(t)
			if err := SetCustomDNSServer(tc.raw); tc.wantErr != (err != nil) {
				t.Fatalf("SetCustomDNSServer(%q) err = %v", tc.raw, err)
			}
			if !tc.wantErr && CustomDNSServer() != tc.want {
				t.Fatalf("CustomDNSServer() = %q, want %q", CustomDNSServer(), tc.want)
			}
		})
	}
}

// .invalid never resolves via real DNS, so a result proves the stub was used.
func TestCustomDNSResolvesThroughConfiguredServer(t *testing.T) {
	resetCustomDNS(t)
	stub := startStubDNSServer(t, net.ParseIP("127.0.0.1"))
	if err := SetCustomDNSServer(stub.addr); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	public, blocked, err := resolveHostIPs(ctx, "search.invalid")
	if err != nil {
		t.Fatalf("resolve via stub DNS: %v", err)
	}
	if len(public) != 0 || len(blocked) != 1 || blocked[0].String() != "127.0.0.1" {
		t.Fatalf("public=%v blocked=%v, want blocked=[127.0.0.1]", public, blocked)
	}
	if stub.queries() == 0 {
		t.Fatal("stub DNS server was never queried")
	}
}

func TestCustomDNSDialContextDialsResolvedIP(t *testing.T) {
	resetCustomDNS(t)
	if _, err := CustomDNSDialContext(context.Background(), "tcp", "search.invalid:80"); err == nil {
		t.Fatal("expected error without configured DNS server")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		if c != nil {
			_ = c.Close()
		}
	}()

	stub := startStubDNSServer(t, net.ParseIP("127.0.0.1"))
	if err := SetCustomDNSServer(stub.addr); err != nil {
		t.Fatal(err)
	}

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := CustomDNSDialContext(ctx, "tcp", net.JoinHostPort("search.invalid", port))
	if err != nil {
		t.Fatalf("dial via stub DNS: %v", err)
	}
	_ = conn.Close()
	if conn.RemoteAddr().String() != ln.Addr().String() {
		t.Fatalf("dialed %s, want %s", conn.RemoteAddr(), ln.Addr())
	}
}

func resetCustomDNS(t *testing.T) {
	t.Helper()
	_ = SetCustomDNSServer("")
	t.Cleanup(func() { _ = SetCustomDNSServer("") })
}

type stubDNSServer struct {
	addr string
	n    atomic.Int32
}

func (s *stubDNSServer) queries() int32 { return s.n.Load() }

func startStubDNSServer(t *testing.T, ip net.IP) *stubDNSServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	srv := &stubDNSServer{addr: conn.LocalAddr().String()}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			srv.n.Add(1)
			if resp := buildStubDNSResponse(buf[:n], ip); resp != nil {
				_, _ = conn.WriteToUDP(resp, addr)
			}
		}
	}()
	return srv
}

func buildStubDNSResponse(req []byte, ip net.IP) []byte {
	v4 := ip.To4()
	end := stubQuestionEnd(req)
	if end < 0 || v4 == nil {
		return nil
	}
	resp := make([]byte, 0, end+16)
	resp = append(resp, req[0], req[1], 0x81, 0x80, 0, 1, 0, 1, 0, 0, 0, 0)
	resp = append(resp, req[12:end]...)
	resp = append(resp, 0xC0, 0x0C, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
	resp = append(resp, v4...)
	return resp
}

// stubQuestionEnd finds the end of the question section so the trailing EDNS
// OPT record Go appends is not echoed back with ARCOUNT=0.
func stubQuestionEnd(req []byte) int {
	off := 12
	for {
		if off >= len(req) {
			return -1
		}
		length := int(req[off])
		off++
		if length == 0 {
			break
		}
		if length&0xC0 != 0 {
			return -1
		}
		off += length
	}
	if off+4 > len(req) {
		return -1
	}
	return off + 4
}

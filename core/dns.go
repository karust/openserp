package core

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
)

var customDNSMu sync.RWMutex
var customDNSAddr string
var customDNSResolver *net.Resolver

// SetCustomDNSServer overrides the resolver used for direct lookups when the
// system DNS filters search domains. addr must be "IP:port" (e.g.
// "8.8.8.8:53"); a hostname would need system DNS to resolve, defeating the
// purpose. Empty clears the override. Safe for concurrent use.
func SetCustomDNSServer(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		customDNSMu.Lock()
		customDNSAddr = ""
		customDNSResolver = nil
		customDNSMu.Unlock()
		return nil
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid dns server %q: expected \"IP:port\" (e.g. \"8.8.8.8:53\")", addr)
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return fmt.Errorf("invalid dns server %q: host must be an IP literal", addr)
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid dns server %q: port must be 1-65535", addr)
	}
	server := net.JoinHostPort(ip.Unmap().String(), strconv.Itoa(port))
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, server)
		},
	}

	customDNSMu.Lock()
	customDNSAddr = server
	customDNSResolver = resolver
	customDNSMu.Unlock()
	return nil
}

// CustomDNSServer returns the configured "IP:port" override, or "".
func CustomDNSServer() string {
	customDNSMu.RLock()
	defer customDNSMu.RUnlock()
	return customDNSAddr
}

func activeResolver() *net.Resolver {
	customDNSMu.RLock()
	defer customDNSMu.RUnlock()
	if customDNSResolver != nil {
		return customDNSResolver
	}
	return net.DefaultResolver
}

// CustomDNSDialContext resolves via the configured DNS server and dials the
// first reachable address, mirroring system-dial behavior without public-IP
// filtering. It is only wired into direct clients when an override is set.
func CustomDNSDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := customLookupNetIP(ctx, host)
	if err != nil {
		return nil, err
	}
	return dialFirstReachable(ctx, network, port, ips, host)
}

func customLookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}
	customDNSMu.RLock()
	resolver := customDNSResolver
	customDNSMu.RUnlock()
	if resolver == nil {
		return nil, fmt.Errorf("custom DNS server is not configured")
	}
	ips, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for i, ip := range ips {
		ips[i] = ip.Unmap()
	}
	return ips, nil
}

func dialFirstReachable(ctx context.Context, network, port string, ips []netip.Addr, host string) (net.Conn, error) {
	dialer := &net.Dialer{}
	var lastErr error
	for _, ip := range ips {
		if !ipMatchesNetwork(ip, network) {
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no addresses available for %s", host)
}

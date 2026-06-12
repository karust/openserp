package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// ErrTargetNotAllowed marks URL-guard policy rejections (bad scheme, missing
// host, or a target resolving to a non-public IP). Handlers match it with
// errors.Is to report a client error instead of an upstream failure.
var ErrTargetNotAllowed = errors.New("target not allowed")

var carrierGradeNATPrefix = netip.MustParsePrefix("100.64.0.0/10")

// GuardedDialContext dials only public IP targets. Hostnames are resolved first
// and the returned connection is made to the vetted IP, so DNS rebinding cannot
// swap in a private address between validation and dial.
func GuardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := resolvePublicDialTargets(ctx, host)
	if err != nil {
		return nil, err
	}

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
	return nil, fmt.Errorf("no public IPs available for %s", host)
}

func ValidatePublicHTTPURL(ctx context.Context, rawURL string) error {
	parsed, err := validateHTTPURL(rawURL)
	if err != nil {
		return err
	}
	return validatePublicHost(ctx, parsed.Hostname())
}

func validateHTTPURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid URL: %v", ErrTargetNotAllowed, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: unsupported URL scheme %q: only http and https are allowed", ErrTargetNotAllowed, parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("%w: URL host is required", ErrTargetNotAllowed)
	}
	return parsed, nil
}

// resolveHostIPs resolves host (or parses a literal IP) and partitions the
// addresses by the public-IP policy.
func resolveHostIPs(ctx context.Context, host string) (public, blocked []netip.Addr, err error) {
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		ip = ip.Unmap()
		if isPublicIP(ip) {
			return []netip.Addr{ip}, nil, nil
		}
		return nil, []netip.Addr{ip}, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, nil, err
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if isPublicIP(ip) {
			public = append(public, ip)
		} else {
			blocked = append(blocked, ip)
		}
	}
	return public, blocked, nil
}

func resolvePublicDialTargets(ctx context.Context, host string) ([]netip.Addr, error) {
	public, blocked, err := resolveHostIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(public) == 0 {
		if len(blocked) == 0 {
			return nil, fmt.Errorf("%w: target host %q resolved to no IP addresses", ErrTargetNotAllowed, host)
		}
		return nil, fmt.Errorf("%w: target host %q resolves only to non-public IPs: %s", ErrTargetNotAllowed, host, joinAddrs(blocked))
	}
	return public, nil
}

// validatePublicHost rejects a host when any of its addresses is non-public —
// stricter than the dial guard on purpose. The rendered path hands the URL to
// Chrome, which resolves DNS on its own, so a mixed public/private record set
// must fail closed here rather than rely on dial-time pinning.
func validatePublicHost(ctx context.Context, host string) error {
	public, blocked, err := resolveHostIPs(ctx, host)
	if err != nil {
		return err
	}
	if len(blocked) > 0 {
		return fmt.Errorf("%w: target host %q resolves to non-public IPs: %s", ErrTargetNotAllowed, host, joinAddrs(blocked))
	}
	if len(public) == 0 {
		return fmt.Errorf("%w: target host %q resolved to no IP addresses", ErrTargetNotAllowed, host)
	}
	return nil
}

func joinAddrs(addrs []netip.Addr) string {
	parts := make([]string, len(addrs))
	for i, addr := range addrs {
		parts[i] = addr.String()
	}
	return strings.Join(parts, ", ")
}

func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() &&
		!ip.IsUnspecified() &&
		!ip.IsLoopback() &&
		!ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() &&
		!carrierGradeNATPrefix.Contains(ip)
}

func ipMatchesNetwork(ip netip.Addr, network string) bool {
	switch network {
	case "tcp4":
		return ip.Is4()
	case "tcp6":
		return ip.Is6()
	default:
		return true
	}
}

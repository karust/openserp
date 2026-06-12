package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/corpix/uarand"
	utls "github.com/refraction-networking/utls"
)

const rawHTTPTimeout = 30 * time.Second

// SetAcceptLanguageHeader sets the Accept-Language header from a lang code.
// No-op when the code has no language subtag.
func SetAcceptLanguageHeader(req *http.Request, langCode string) {
	if req == nil {
		return
	}
	if value := BuildAcceptLanguageHeader(langCode); value != "" {
		req.Header.Set("Accept-Language", value)
	}
}

// DrainAndCloseResponse drains unread bytes before closing so HTTP transports
// can safely reuse connections when callers don't consume the full body.
func DrainAndCloseResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// RawSearchRequest builds and executes a raw-mode SERP HTTP GET. It uses the
// shared raw HTTP client (TLS fingerprinting, network usage tracking, proxy
// support), randomizes the User-Agent, and applies the Accept-Language header
// derived from the query locale. The caller owns the returned response and
// must drain/close it (see DrainAndCloseResponse).
func RawSearchRequest(ctx context.Context, searchURL string, query Query) (*http.Response, error) {
	if query.GuardPrivateNetworks {
		if err := ValidatePublicHTTPURL(ctx, searchURL); err != nil {
			return nil, err
		}
	}
	client, err := NewRawHTTPClient(query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uarand.GetRandom())
	SetAcceptLanguageHeader(req, query.LangCode)
	return client.Do(req)
}

func ReadRawSearchBody(resp *http.Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("%w: nil raw search response", ErrEngineInternal)
	}
	if err := ClassifySearchHTTPStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

func ClassifySearchHTTPStatus(status int) error {
	switch status {
	case 0:
		return nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return ErrBlocked
	case http.StatusTooManyRequests:
		return ErrRateLimited
	}
	if status >= 500 {
		return fmt.Errorf("%w: search engine returned HTTP %d", ErrBlocked, status)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: search engine returned HTTP %d", ErrParser, status)
	}
	return nil
}

func NewRawHTTPClient(query Query) (*http.Client, error) {
	transport, err := newRawTransport(query)
	if err != nil {
		return nil, err
	}
	roundTripper := http.RoundTripper(transport)
	if transport.Proxy != nil {
		roundTripper = proxyErrorTransport{base: roundTripper}
	}

	client := &http.Client{
		Transport: roundTripper,
		Timeout:   rawHTTPTimeout,
	}
	if query.GuardPrivateNetworks {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return ValidatePublicHTTPURL(req.Context(), req.URL.String())
		}
	}
	return client, nil
}

func newRawTransport(query Query) (*http.Transport, error) {
	dialContext := dialNetworkUsageConn
	if query.GuardPrivateNetworks {
		dialContext = guardedDialNetworkUsageConn
	}

	transport := &http.Transport{
		DialContext: dialContext,
	}
	if query.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	proxyURL, err := NormalizeProxyURL(query.ProxyURL)
	if err != nil {
		return nil, err
	}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}

		// Keep proxied requests on the standard transport path so SOCKS5/SOCKS5H
		// resolution and routing are handled by the configured proxy correctly.
		// The extract SSRF guard validates the target URL before the request and
		// on redirects; the proxy address itself may legitimately be local.
		transport.DialContext = dialNetworkUsageConn
		transport.Proxy = http.ProxyURL(parsed)
		return transport, nil
	}

	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		rawConn, err := dialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		hostname := strings.Split(addr, ":")[0]
		config := &utls.Config{
			ServerName:         hostname,
			InsecureSkipVerify: query.Insecure,
			NextProtos:         []string{"http/1.1"},
		}

		uconn := utls.UClient(rawConn, config, utls.HelloChrome_Auto)
		if err := uconn.BuildHandshakeState(); err != nil {
			rawConn.Close()
			return nil, err
		}
		forceHTTP1ALPN(uconn)
		if err := uconn.Handshake(); err != nil {
			rawConn.Close()
			return nil, err
		}

		return uconn, nil
	}

	return transport, nil
}

func forceHTTP1ALPN(conn *utls.UConn) {
	for _, ext := range conn.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
			return
		}
	}
	conn.Extensions = append(conn.Extensions, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
}

func dialNetworkUsageConn(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return networkUsageConn{Conn: conn, ctx: ctx}, nil
}

func guardedDialNetworkUsageConn(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := GuardedDialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return networkUsageConn{Conn: conn, ctx: ctx}, nil
}

type networkUsageConn struct {
	net.Conn
	ctx context.Context
}

func (c networkUsageConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	AddNetworkBytes(c.ctx, int64(n))
	return n, err
}

type proxyErrorTransport struct {
	base http.RoundTripper
}

func (t proxyErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, classifyProxyNetworkError(err)
	}
	if resp != nil && resp.StatusCode == http.StatusProxyAuthRequired {
		DrainAndCloseResponse(resp)
		return nil, classifyProxyNetworkError(ErrProxyAuth)
	}
	return resp, nil
}

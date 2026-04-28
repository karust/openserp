package core

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

const rawHTTPTimeout = 10 * time.Second

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

func NewRawHTTPClient(query Query) (*http.Client, error) {
	transport, err := newRawTransport(query)
	if err != nil {
		return nil, err
	}
	roundTripper := http.RoundTripper(transport)
	if transport.Proxy != nil {
		roundTripper = proxyErrorTransport{base: roundTripper}
	}

	return &http.Client{
		Transport: roundTripper,
		Timeout:   rawHTTPTimeout,
	}, nil
}

func newRawTransport(query Query) (*http.Transport, error) {
	transport := &http.Transport{
		DialContext: dialNetworkUsageConn,
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
		transport.Proxy = http.ProxyURL(parsed)
		return transport, nil
	}

	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		rawConn, err := dialNetworkUsageConn(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		hostname := strings.Split(addr, ":")[0]
		config := &utls.Config{
			ServerName:         hostname,
			InsecureSkipVerify: query.Insecure,
		}

		uconn := utls.UClient(rawConn, config, utls.HelloChrome_Auto)
		if err := uconn.Handshake(); err != nil {
			rawConn.Close()
			return nil, err
		}

		return uconn, nil
	}

	return transport, nil
}

func dialNetworkUsageConn(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, addr)
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

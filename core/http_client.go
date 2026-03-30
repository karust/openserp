package core

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

const rawHTTPTimeout = 10 * time.Second

func NewRawHTTPClient(query Query) (*http.Client, error) {
	transport, err := newRawTransport(query)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: transport,
		Timeout:   rawHTTPTimeout,
	}, nil
}

func newRawTransport(query Query) (*http.Transport, error) {
	transport := &http.Transport{}
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
		dialer := &net.Dialer{}
		rawConn, err := dialer.DialContext(ctx, network, addr)
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

package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestStatusText(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{400, "bad_request"},
		{404, "not_found"},
		{429, "rate_limited"},
		{503, "service_unavailable"},
		{401, "client_error"},
		{500, "server_error"},
		{200, "error"},
	}

	for _, tt := range tests {
		result := statusText(tt.code)
		if result != tt.expected {
			t.Errorf("statusText(%d) = %s, want %s", tt.code, result, tt.expected)
		}
	}
}

// Middleware unit tests validate CORS behavior itself (header composition and preflight semantics).
func TestCORSMiddleware_UsesConfiguredHeaders(t *testing.T) {
	app := fiber.New()
	app.Use(CORSMiddleware(CORSConfig{
		AllowOrigins: "https://example.com",
		AllowMethods: "GET,OPTIONS",
		AllowHeaders: "Authorization,Content-Type",
		MaxAge:       1200,
	}))
	app.Get("/ping", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "GET,OPTIONS" {
		t.Fatalf("unexpected allow-methods: %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "Authorization,Content-Type" {
		t.Fatalf("unexpected allow-headers: %q", got)
	}
	if got := resp.Header.Get("Access-Control-Max-Age"); got != "1200" {
		t.Fatalf("unexpected max-age: %q", got)
	}
	if got := resp.Header.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-Network-Bytes") || !strings.Contains(got, "X-Browser-Profile-Id") {
		t.Fatalf("expected exposed headers to include network and browser profile headers, got %q", got)
	}
}

func TestCORSMiddleware_OPTIONSReturnsNoContent(t *testing.T) {
	app := fiber.New()
	app.Use(CORSMiddleware(DefaultCORSConfig()))
	app.Get("/ping", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", resp.StatusCode)
	}
}

func TestNormalizeCORSConfig_FillsMissingValues(t *testing.T) {
	cfg := normalizeCORSConfig(CORSConfig{
		AllowOrigins: "https://example.com",
	})
	def := DefaultCORSConfig()

	if cfg.AllowOrigins != "https://example.com" {
		t.Fatalf("expected custom allow_origins preserved, got %q", cfg.AllowOrigins)
	}
	if cfg.AllowMethods != def.AllowMethods {
		t.Fatalf("expected default allow_methods, got %q", cfg.AllowMethods)
	}
	if cfg.AllowHeaders != def.AllowHeaders {
		t.Fatalf("expected default allow_headers, got %q", cfg.AllowHeaders)
	}
	if cfg.MaxAge != def.MaxAge {
		t.Fatalf("expected default max_age, got %d", cfg.MaxAge)
	}
}

func TestDefaultCORSConfig_IncludesProxyOverrideHeader(t *testing.T) {
	cfg := DefaultCORSConfig()
	if got := cfg.AllowHeaders; !strings.Contains(got, "X-Use-Proxy") {
		t.Fatalf("expected allow_headers to include X-Use-Proxy, got %q", got)
	}
}

func TestRequestContextMiddleware_EchoesProvidedRequestID(t *testing.T) {
	app := fiber.New()
	app.Use(RequestContextMiddleware())
	app.Get("/id", func(c *fiber.Ctx) error {
		return c.SendString(RequestIDFromContext(c.UserContext()))
	})

	req := httptest.NewRequest(http.MethodGet, "/id", nil)
	req.Header.Set("X-Request-ID", "foo")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if got := resp.Header.Get("X-Request-ID"); got != "foo" {
		t.Fatalf("expected response X-Request-ID=foo, got %q", got)
	}
	if got := readBody(t, resp); got != "foo" {
		t.Fatalf("expected context request id to be echoed, got %q", got)
	}
}

func TestRequestContextMiddleware_GeneratesRequestID(t *testing.T) {
	app := fiber.New()
	app.Use(RequestContextMiddleware())
	app.Get("/id", func(c *fiber.Ctx) error {
		return c.SendString(RequestIDFromContext(c.UserContext()))
	})

	req := httptest.NewRequest(http.MethodGet, "/id", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("expected generated X-Request-ID")
	}
	if got := readBody(t, resp); got != requestID {
		t.Fatalf("expected request id in context to match header: body=%q header=%q", got, requestID)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	return string(body)
}

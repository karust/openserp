package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type JSONErrorResponse struct {
	Error     string                 `json:"error"`
	Code      int                    `json:"code"`
	RequestID string                 `json:"request_id,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Reason    string                 `json:"reason,omitempty"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

type CORSConfig struct {
	AllowOrigins string
	AllowMethods string
	AllowHeaders string
	MaxAge       int
}

const browserProfileIDHeader = "X-Browser-Profile-Id"
const useProfileHeader = "X-Use-Profile"

const exposedResponseHeaders = "X-Request-ID, X-Cache, X-Fallback-Engine, X-Proxy-Mode, X-Proxy-Tag, X-Proxy-Used, X-Network-Bytes, " + browserProfileIDHeader

func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins: "*",
		AllowMethods: "GET, POST, OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Use-Proxy, X-Proxy-URL, X-Proxy-Country, X-Proxy-Class, X-Proxy-Provider, X-Proxy-Session-ID, X-Request-ID, X-Tenant, X-Use-Profile",
		MaxAge:       86400,
	}
}

func RequestContextMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		requestID := strings.TrimSpace(c.Get("X-Request-ID"))
		if requestID == "" {
			id, err := uuid.NewV7()
			if err != nil {
				requestID = uuid.NewString()
			} else {
				requestID = id.String()
			}
		}

		requestCtx := WithRequestID(c.UserContext(), requestID)
		requestCtx = WithTenant(requestCtx, strings.TrimSpace(c.Get("X-Tenant")))
		requestCtx = WithQueryHash(requestCtx, QueryHash(c.Query("text")))
		c.SetUserContext(requestCtx)

		c.Set("X-Request-ID", requestID)
		return c.Next()
	}
}

// RequestTimeoutMiddleware bounds wall-clock time per request by attaching a
// deadline to the user context, which fasthttp never cancels on client
// disconnect. /mega/* (MegaTimeout) and /extract (batch budget) are exempt.
func RequestTimeoutMiddleware(timeout time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/mega/") || c.Path() == "/extract" {
			return c.Next()
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), timeout)
		defer cancel()
		c.SetUserContext(ctx)
		return c.Next()
	}
}

func CORSMiddleware(cfg CORSConfig) fiber.Handler {
	cfg = normalizeCORSConfig(cfg)

	return func(c *fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", cfg.AllowOrigins)
		c.Set("Access-Control-Allow-Methods", cfg.AllowMethods)
		c.Set("Access-Control-Allow-Headers", cfg.AllowHeaders)
		c.Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
		c.Set("Access-Control-Expose-Headers", exposedResponseHeaders)

		if c.Method() == "OPTIONS" {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Next()
	}
}

// normalizeCORSConfig keeps CORS behavior predictable when config provides partial values.
func normalizeCORSConfig(cfg CORSConfig) CORSConfig {
	defaults := DefaultCORSConfig()

	if strings.TrimSpace(cfg.AllowOrigins) == "" {
		cfg.AllowOrigins = defaults.AllowOrigins
	}
	if strings.TrimSpace(cfg.AllowMethods) == "" {
		cfg.AllowMethods = defaults.AllowMethods
	}
	if strings.TrimSpace(cfg.AllowHeaders) == "" {
		cfg.AllowHeaders = defaults.AllowHeaders
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = defaults.MaxAge
	}

	return cfg
}

func RequestLoggerMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		latency := time.Since(start)
		status := c.Response().StatusCode()
		if err != nil {
			if e, ok := err.(*fiber.Error); ok {
				status = e.Code
			} else if apiErr, ok := err.(*APIError); ok {
				status = apiErr.HTTPStatus
			} else {
				status = fiber.StatusInternalServerError
			}
		}

		logFields := logrus.Fields{
			"method": c.Method(),
			"path":   c.Path(),
			"status": status,
			"ip":     c.IP(),
		}
		logFields["latency_ms"] = latency.Milliseconds()
		if query := c.Query("text"); query != "" {
			logFields["query_hash"] = QueryHash(query)
		}
		addProxyLogFields(c, logFields)

		entry := WithRequest(c.UserContext()).WithFields(logFields)
		if status >= 500 {
			entry.Error("request failed")
		} else if status >= 400 {
			entry.Warn("request error")
		} else {
			entry.Info("request completed")
		}

		return err
	}
}

func addProxyLogFields(c *fiber.Ctx, fields logrus.Fields) {
	if country := strings.ToLower(strings.TrimSpace(c.Get("X-Proxy-Country"))); country != "" {
		fields["proxy_country"] = country
	}
	if class := strings.ToLower(strings.TrimSpace(c.Get("X-Proxy-Class"))); class != "" {
		fields["proxy_class"] = class
	}
	if provider := strings.ToLower(strings.TrimSpace(c.Get("X-Proxy-Provider"))); provider != "" {
		fields["proxy_provider"] = provider
	}

	if sessionID := strings.TrimSpace(c.Get("X-Proxy-Session-ID")); sessionID != "" {
		fields["proxy_session_id"] = sessionID
	}

	if proxyURL := strings.TrimSpace(c.Get("X-Proxy-URL")); proxyURL != "" {
		fields["proxy_used"] = MaskProxyURL(proxyURL)
	}

	if laneKey := proxyLaneKeyFromContext(c.UserContext()); !laneKey.Empty() {
		fields["lane_id"] = laneKey.ID()
	}
}

func JSONErrorMiddleware() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		errorCode := ""
		reason := ""
		var meta map[string]interface{}

		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}
		if apiErr, ok := err.(*APIError); ok {
			code = apiErr.HTTPStatus
			errorCode = apiErr.ErrorCode
			reason = apiErr.Reason
			meta = apiErr.Meta
		}
		if errorCode == "" {
			errorCode = statusText(code)
		}
		requestID := RequestIDFromContext(c.UserContext())
		if requestID == "" {
			requestID = strings.TrimSpace(c.Get("X-Request-ID"))
		}

		resp := JSONErrorResponse{
			Error:     errorCode,
			Code:      code,
			RequestID: requestID,
			Message:   err.Error(),
			Reason:    reason,
			Meta:      meta,
		}

		c.Set("Content-Type", "application/json")
		return c.Status(code).JSON(resp)
	}
}

func statusText(code int) string {
	switch {
	case code == 400:
		return "bad_request"
	case code == 404:
		return "not_found"
	case code == 429:
		return "rate_limited"
	case code == 503:
		return "service_unavailable"
	case code >= 400 && code < 500:
		return "client_error"
	case code >= 500:
		return "server_error"
	default:
		return "error"
	}
}

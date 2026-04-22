package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type JSONErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type CORSConfig struct {
	AllowOrigins string
	AllowMethods string
	AllowHeaders string
	MaxAge       int
}

func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins: "*",
		AllowMethods: "GET, POST, OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Use-Proxy, X-Request-ID, X-Tenant",
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

func CORSMiddleware(cfg CORSConfig) fiber.Handler {
	cfg = normalizeCORSConfig(cfg)

	return func(c *fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", cfg.AllowOrigins)
		c.Set("Access-Control-Allow-Methods", cfg.AllowMethods)
		c.Set("Access-Control-Allow-Headers", cfg.AllowHeaders)
		c.Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))

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

func JSONErrorMiddleware() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		resp := JSONErrorResponse{
			Error:   statusText(code),
			Code:    code,
			Message: err.Error(),
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

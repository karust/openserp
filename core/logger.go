package core

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type loggerContextKey string

const (
	requestIDContextKey loggerContextKey = "request_id"
	tenantContextKey    loggerContextKey = "tenant"
	engineContextKey    loggerContextKey = "engine"
	queryHashContextKey loggerContextKey = "query_hash"

	LogFormatJSON = "json"
	LogFormatText = "text"
)

func NormalizeLogFormat(raw string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(raw))
	if format == "" {
		return LogFormatText, nil
	}
	switch format {
	case LogFormatJSON, LogFormatText:
		return format, nil
	default:
		return "", fmt.Errorf("invalid logging.format %q: expected json or text", raw)
	}
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), requestIDContextKey, requestID)
}

func WithTenant(ctx context.Context, tenant string) context.Context {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), tenantContextKey, tenant)
}

func WithEngine(ctx context.Context, engine string) context.Context {
	engine = strings.TrimSpace(engine)
	if engine == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), engineContextKey, engine)
}

func WithQueryHash(ctx context.Context, queryHash string) context.Context {
	queryHash = strings.TrimSpace(queryHash)
	if queryHash == "" {
		return EnsureContext(ctx)
	}
	return context.WithValue(EnsureContext(ctx), queryHashContextKey, queryHash)
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := EnsureContext(ctx).Value(requestIDContextKey).(string)
	return strings.TrimSpace(value)
}

func WithRequest(ctx context.Context) *logrus.Entry {
	ctx = EnsureContext(ctx)
	fields := logrus.Fields{}

	if requestID, ok := ctx.Value(requestIDContextKey).(string); ok && strings.TrimSpace(requestID) != "" {
		fields["request_id"] = strings.TrimSpace(requestID)
	}
	if tenant, ok := ctx.Value(tenantContextKey).(string); ok && strings.TrimSpace(tenant) != "" {
		fields["tenant"] = strings.TrimSpace(tenant)
	}
	if engine, ok := ctx.Value(engineContextKey).(string); ok && strings.TrimSpace(engine) != "" {
		fields["engine"] = strings.TrimSpace(engine)
	}
	if queryHash, ok := ctx.Value(queryHashContextKey).(string); ok && strings.TrimSpace(queryHash) != "" {
		fields["query_hash"] = strings.TrimSpace(queryHash)
	}

	return logrus.WithFields(fields)
}

func WithRequestEngine(ctx context.Context, engine string) *logrus.Entry {
	return WithRequest(WithEngine(ctx, engine))
}

func QueryHash(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	if normalized == "" {
		return ""
	}
	hash := md5.Sum([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func QueryHashFromQuery(q Query) string {
	raw := strings.Join([]string{
		q.Text,
		q.Site,
		q.Filetype,
		q.LangCode,
		q.DateInterval,
	}, "|")
	return QueryHash(raw)
}

func formatMessage(message string, args ...any) string {
	if len(args) == 0 {
		return message
	}
	return fmt.Sprintf(message, args...)
}

// EngineLogger provides structured logging for search engines with a fixed engine field.
type EngineLogger struct {
	engine string
	entry  *logrus.Entry
}

func NewEngineLogger(engine string) *EngineLogger {
	engine = strings.ToLower(strings.TrimSpace(engine))
	return &EngineLogger{engine: engine, entry: logrus.WithField("engine", engine)}
}

func (el *EngineLogger) WithRequest(ctx context.Context) *EngineLogger {
	return &EngineLogger{engine: el.engine, entry: WithRequestEngine(ctx, el.engine)}
}

// Fields returns a new EngineLogger with additional structured fields merged in.
func (el *EngineLogger) Fields(fields logrus.Fields) *EngineLogger {
	return &EngineLogger{engine: el.engine, entry: el.entry.WithFields(fields)}
}

func (el *EngineLogger) Debug(message string, args ...any) {
	el.entry.Debug(formatMessage(message, args...))
}

func (el *EngineLogger) Info(message string, args ...any) {
	el.entry.Info(formatMessage(message, args...))
}

func (el *EngineLogger) Warn(message string, args ...any) {
	el.entry.Warn(formatMessage(message, args...))
}

func (el *EngineLogger) Error(message string, args ...any) {
	el.entry.Error(formatMessage(message, args...))
}

func (el *EngineLogger) Fatal(message string, args ...any) {
	el.entry.Fatal(formatMessage(message, args...))
}

func (el *EngineLogger) Panic(message string, args ...any) {
	el.entry.Panic(formatMessage(message, args...))
}

// bracketFormatter emits bracket-delimited fields:
//
//	[time][level][engine=..][request_id=..][query_hash=..][extra fields sorted][msg]
//
// request_id is truncated to last 8 chars; query_hash to first 12.
type bracketFormatter struct {
	TimestampFormat string
}

func (f *bracketFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	ts := entry.Time.Format(f.TimestampFormat)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "[%s][%s]", ts, entry.Level.String())

	// Context identity fields in fixed order, then remaining fields sorted, then msg last.
	priority := []string{"engine", "tenant", "request_id", "query_hash"}
	written := make(map[string]bool, len(entry.Data))

	for _, key := range priority {
		val, ok := entry.Data[key]
		if !ok {
			continue
		}
		s := fmt.Sprintf("%v", val)
		switch key {
		case "request_id":
			if len(s) > 8 {
				s = s[len(s)-8:]
			}
		case "query_hash":
			if len(s) > 12 {
				s = s[:12]
			}
		}
		fmt.Fprintf(&buf, "[%s=%s]", key, quoteIfNeeded(s))
		written[key] = true
	}

	rest := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		if !written[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		fmt.Fprintf(&buf, "[%s=%s]", k, quoteIfNeeded(fmt.Sprintf("%v", entry.Data[k])))
	}

	// Message last so context fields are scannable without scrolling past a long msg.
	if entry.Message != "" {
		fmt.Fprintf(&buf, "[%s]", quoteIfNeeded(entry.Message))
	}

	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// quoteIfNeeded wraps s in double-quotes if it contains spaces.
func quoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func InitLogger(isVerbose, isDebug bool, format string) {
	switch format {
	case LogFormatText:
		logrus.SetFormatter(&bracketFormatter{TimestampFormat: "2006-01-02 15:04:05"})
	case LogFormatJSON:
		logrus.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
		})
	}

	if isDebug {
		logrus.SetOutput(io.MultiWriter(os.Stdout))
		logrus.SetReportCaller(true)
	} else {
		f, err := os.OpenFile("./logs.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open logs file ./logs.txt: %v\n", err)
			logrus.SetOutput(io.MultiWriter(os.Stdout))
		} else {
			logrus.SetOutput(io.MultiWriter(f, os.Stdout))
		}
		logrus.SetReportCaller(false)
	}

	level := logrus.InfoLevel
	if isVerbose {
		level = logrus.DebugLevel
	}
	if isDebug {
		level = logrus.TraceLevel
	}
	logrus.SetLevel(level)
}

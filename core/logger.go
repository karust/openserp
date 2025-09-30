package core

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

type customFormatter struct {
	logrus.TextFormatter
}

func (f *customFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	message := entry.Message

	// Check if engine name is provided as a field
	engineName := ""
	if engine, exists := entry.Data["engine"]; exists {
		if engineStr, ok := engine.(string); ok {
			engineName = engineStr
		}
	}

	// Format: [timestamp][level][engine] message
	if engineName != "" {
		return []byte(fmt.Sprintf("[%s][%s][%s] %s\n",
			entry.Time.Format(f.TimestampFormat),
			strings.ToUpper(entry.Level.String()),
			engineName,
			message)), nil
	}

	// Format: [timestamp][level] message (no engine)
	return []byte(fmt.Sprintf("[%s][%s] %s\n",
		entry.Time.Format(f.TimestampFormat),
		strings.ToUpper(entry.Level.String()),
		message)), nil
}

// EngineLogger provides simplified logging for search engines
type EngineLogger struct {
	engine string
	logger *logrus.Entry
}

// NewEngineLogger creates a new logger for a specific search engine
func NewEngineLogger(engine string) *EngineLogger {
	return &EngineLogger{
		engine: engine,
		logger: logrus.WithField("engine", engine),
	}
}

// Debug logs a debug message
func (el *EngineLogger) Debug(message string, args ...interface{}) {
	el.logger.Debugf(message, args...)
}

// Info logs an info message
func (el *EngineLogger) Info(message string, args ...interface{}) {
	el.logger.Infof(message, args...)
}

// Warn logs a warning message
func (el *EngineLogger) Warn(message string, args ...interface{}) {
	el.logger.Warnf(message, args...)
}

// Error logs an error message
func (el *EngineLogger) Error(message string, args ...interface{}) {
	el.logger.Errorf(message, args...)
}

// Fatal logs a fatal message
func (el *EngineLogger) Fatal(message string, args ...interface{}) {
	el.logger.Fatalf(message, args...)
}

// Panic logs a panic message
func (el *EngineLogger) Panic(message string, args ...interface{}) {
	el.logger.Panicf(message, args...)
}

// LogWithEngine logs a message with engine information (deprecated - use EngineLogger instead)
func LogWithEngine(level logrus.Level, engine, message string, args ...interface{}) {
	entry := logrus.WithField("engine", engine)
	switch level {
	case logrus.DebugLevel:
		entry.Debugf(message, args...)
	case logrus.InfoLevel:
		entry.Infof(message, args...)
	case logrus.WarnLevel:
		entry.Warnf(message, args...)
	case logrus.ErrorLevel:
		entry.Errorf(message, args...)
	case logrus.FatalLevel:
		entry.Fatalf(message, args...)
	case logrus.PanicLevel:
		entry.Panicf(message, args...)
	}
}

func InitLogger(isVerbose, isDebug bool) {
	logrus.SetFormatter(&customFormatter{logrus.TextFormatter{
		FullTimestamp:          true,
		TimestampFormat:        "2006-01-02 15:04:05",
		ForceColors:            true,
		DisableLevelTruncation: true,
	}})

	if isVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if isDebug {
		logrus.SetOutput(io.MultiWriter(os.Stdout))
		logrus.SetLevel(logrus.TraceLevel)
		logrus.SetReportCaller(true)
	} else {
		f, err := os.OpenFile("./logs.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Failed to create logsfile: ./logs.txt")
			panic(err)
		}

		logrus.SetOutput(io.MultiWriter(f, os.Stdout))
		logrus.SetLevel(logrus.DebugLevel)
	}
}

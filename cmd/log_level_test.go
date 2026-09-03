package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func TestApplyLogLevel(t *testing.T) {
	previous := logrus.GetLevel()
	t.Cleanup(func() { logrus.SetLevel(previous) })

	for _, tc := range []struct {
		raw   string
		level logrus.Level
	}{
		{" error ", logrus.ErrorLevel},
		{"warn", logrus.WarnLevel},
		{"warning", logrus.WarnLevel},
		{"panic", logrus.PanicLevel},
		{"fatal", logrus.FatalLevel},
		{"trace", logrus.TraceLevel},
		{"info", logrus.InfoLevel},
		{"DEBUG", logrus.DebugLevel},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			logrus.SetLevel(logrus.TraceLevel)
			if err := applyLogLevel(tc.raw); err != nil {
				t.Fatalf("applyLogLevel(%q): %v", tc.raw, err)
			}
			if got := logrus.GetLevel(); got != tc.level {
				t.Fatalf("level = %s, want %s", got, tc.level)
			}
		})
	}

	logrus.SetLevel(logrus.WarnLevel)
	if err := applyLogLevel("  "); err != nil {
		t.Fatalf("empty log level: %v", err)
	}
	if got := logrus.GetLevel(); got != logrus.WarnLevel {
		t.Fatalf("empty level changed logger to %s", got)
	}

	if err := applyLogLevel("verbose"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid level error = %v", err)
	}
}

func TestInitializeConfigLogLevelPrecedence(t *testing.T) {
	previous := config
	previousLevel := logrus.GetLevel()
	t.Cleanup(func() { config = previous; logrus.SetLevel(previousLevel) })

	for _, tc := range []struct {
		name       string
		file, env  string
		flag       string
		wantConfig string
		wantLevel  logrus.Level
	}{
		{"config only", "warn", "", "", "warn", logrus.WarnLevel},
		{"env overrides config", "warn", "error", "", "error", logrus.ErrorLevel},
		{"flag overrides env", "warn", "error", "debug", "debug", logrus.DebugLevel},
		{"explicit empty flag", "warn", "error", "", "", logrus.InfoLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("config.yaml", []byte("app:\n  log_level: "+tc.file+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("OPENSERP_APP_LOG_LEVEL", tc.env)
			cmd := configTestCommand(t, tc.flag)
			logrus.SetLevel(logrus.InfoLevel)
			if tc.name == "explicit empty flag" {
				if err := cmd.Flags().Set("log-level", ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := initializeConfig(cmd); err != nil {
				t.Fatalf("initializeConfig: %v", err)
			}
			if config.App.LogLevel != tc.wantConfig {
				t.Fatalf("log level = %q, want %q", config.App.LogLevel, tc.wantConfig)
			}
			if got := logrus.GetLevel(); got != tc.wantLevel {
				t.Fatalf("logger level = %s, want %s", got, tc.wantLevel)
			}
		})
	}
}

func TestInitializeConfigMissingConfigRespectsEarlyLogLevel(t *testing.T) {
	for _, tc := range []struct {
		name, flag, env string
		wantWarning     bool
	}{
		{"default", "", "", true},
		{"CLI error", "error", "", false},
		{"env error", "", "error", false},
		{"CLI warn overrides env", "warn", "error", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("OPENSERP_APP_LOG_LEVEL", tc.env)
			t.Setenv("OPENSERP_SERVER_CONFIG_PATH", "")
			previous := config
			t.Cleanup(func() { config = previous })
			var output bytes.Buffer
			t.Cleanup(captureLogger(&output))
			if err := initializeConfig(configTestCommand(t, tc.flag)); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(output.String(), "cannot read config"); got != tc.wantWarning {
				t.Fatalf("warning present = %v, want %v; output: %s", got, tc.wantWarning, output.String())
			}
		})
	}
}

func TestPersistentPreRunLogLevels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags map[string]string
		want  logrus.Level
	}{
		{"CLI default", nil, logrus.WarnLevel},
		{"quiet", map[string]string{"quiet": "true"}, logrus.WarnLevel},
		{"verbose", map[string]string{"verbose": "true"}, logrus.DebugLevel},
		{"debug", map[string]string{"debug": "true"}, logrus.TraceLevel},
		{"debug wins", map[string]string{"quiet": "true", "verbose": "true", "debug": "true"}, logrus.TraceLevel},
		{"explicit error wins", map[string]string{"quiet": "true", "verbose": "true", "debug": "true", "log-level": "error"}, logrus.ErrorLevel},
		{"explicit info overrides quiet", map[string]string{"quiet": "true", "log-level": "info"}, logrus.InfoLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, key := range []string{"OPENSERP_APP_LOG_LEVEL", "OPENSERP_SERVER_CONFIG_PATH", "OPENSERP_SERVER_QUIET", "OPENSERP_SERVER_VERBOSE", "OPENSERP_SERVER_DEBUG"} {
				t.Setenv(key, "")
			}
			previous := config
			t.Cleanup(func() { config = previous })
			var output bytes.Buffer
			t.Cleanup(captureLogger(&output))
			cmd := configTestCommand(t, "")
			cmd.Use = "search"
			for _, flag := range []string{"quiet", "verbose", "debug"} {
				cmd.Flags().Bool(flag, false, "")
			}
			for flag, value := range tc.flags {
				if err := cmd.Flags().Set(flag, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := RootCmd.PersistentPreRunE(cmd, nil); err != nil {
				t.Fatal(err)
			}
			if got := logrus.GetLevel(); got != tc.want {
				t.Fatalf("level = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestInitializeConfigRejectsInvalidLevel(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := configTestCommand(t, "no-such-level")
	if err := initializeConfig(cmd); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("initializeConfig error = %v, want invalid level error", err)
	}
}

func TestInitializeConfigExplicitMissingConfigReturnsError(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := configTestCommand(t, "error")
	if err := cmd.Flags().Set("config", filepath.Join(t.TempDir(), "missing.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := initializeConfig(cmd); err == nil || !strings.Contains(err.Error(), "cannot read config") {
		t.Fatalf("initializeConfig error = %v, want explicit config read error", err)
	}
}

func configTestCommand(t *testing.T, level string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("log-level", "", "")
	if level != "" {
		if err := cmd.Flags().Set("log-level", level); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func captureLogger(output io.Writer) func() {
	logger := logrus.StandardLogger()
	previousOut := logger.Out
	previousFormatter := logger.Formatter
	previousLevel := logger.Level
	previousCaller := logger.ReportCaller
	logger.SetOutput(output)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logger.SetLevel(logrus.InfoLevel)
	logger.SetReportCaller(false)
	return func() {
		logger.SetOutput(previousOut)
		logger.SetFormatter(previousFormatter)
		logger.SetLevel(previousLevel)
		logger.SetReportCaller(previousCaller)
	}
}

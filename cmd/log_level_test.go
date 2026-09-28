package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestLogLevelOverride(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		typed []string
		want  logrus.Level
		ok    bool
	}{
		{name: "unset"},
		{name: "config", raw: " DEBUG ", want: logrus.DebugLevel, ok: true},
		{name: "typed -v beats env/config", raw: "error", typed: []string{"verbose"}},
		{name: "--log_level beats typed -d", raw: "error", typed: []string{"log_level", "debug"}, want: logrus.ErrorLevel, ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String("log_level", "", "")
			flags.Bool("quiet", false, "")
			flags.Bool("verbose", false, "")
			flags.Bool("debug", false, "")
			for _, name := range tc.typed {
				flags.Lookup(name).Changed = true
			}

			level, ok, err := logLevelOverride(flags, tc.raw)
			if err != nil || ok != tc.ok || (ok && level != tc.want) {
				t.Fatalf("got (%s, %v, %v), want (%s, %v)", level, ok, err, tc.want, tc.ok)
			}
		})
	}

	for _, raw := range []string{"bogus", "panic", "fatal"} {
		if _, _, err := logLevelOverride(pflag.NewFlagSet("test", pflag.ContinueOnError), raw); err == nil {
			t.Errorf("%q: want invalid level error", raw)
		}
	}
}

func TestInitializeConfigLogLevel(t *testing.T) {
	cases := []struct {
		name, env, flag, want string
	}{
		{"config", "", "", "warn"},
		{"env beats config", "error", "", "error"},
		{"flag beats env", "error", "debug", "debug"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("OPENSERP_APP_LOG_LEVEL", tc.env)
			if err := os.WriteFile("config.yaml", []byte("app:\n  log_level: warn\n"), 0600); err != nil {
				t.Fatal(err)
			}

			if err := initializeConfig(configCommand(t, tc.flag)); err != nil {
				t.Fatal(err)
			}
			if config.App.LogLevel != tc.want {
				t.Fatalf("log level = %q, want %q", config.App.LogLevel, tc.want)
			}
		})
	}
}

// Issue #44: no ./config.yaml is normal and must not log a warning.
func TestInitializeConfigWithoutConfigFile(t *testing.T) {
	isolateConfig(t)
	var logs bytes.Buffer
	logger := logrus.StandardLogger()
	out := logger.Out
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(out) })

	if err := initializeConfig(configCommand(t, "")); err != nil {
		t.Fatal(err)
	}
	if logs.Len() > 0 {
		t.Fatalf("unexpected logs: %s", logs.String())
	}
}

func configCommand(t *testing.T, logLevel string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("log_level", "", "")
	if logLevel != "" {
		if err := cmd.Flags().Set("log_level", logLevel); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

// isolateConfig runs the test in an empty dir without config env vars.
func isolateConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("OPENSERP_SERVER_CONFIG_PATH", "")
	t.Setenv("OPENSERP_APP_LOG_LEVEL", "")
	previous := config
	t.Cleanup(func() { config = previous })
}

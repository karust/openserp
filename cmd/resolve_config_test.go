package cmd

import (
	"os"
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

func TestInitializeConfigGoogleResolve(t *testing.T) {
	for _, tc := range []struct {
		name, file, env, flag, want string
	}{
		{name: "defaults", want: "true"},
		{name: "config", file: "false", want: "false"},
		{name: "config true resolves all", file: "true", want: "true"},
		{name: "top N", file: "3", want: "3"},
		{name: "env overrides config", file: "false", env: "2", want: "2"},
		{name: "flag overrides env", file: "false", env: "2", flag: "true", want: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("OPENSERP_GOOGLE_RESOLVE_URLS", tc.env)
			if tc.file != "" {
				if err := os.WriteFile("config.yaml", []byte("google:\n  resolve_urls: "+tc.file+"\n  resolve_concurrency: 2\n  resolve_timeout: 750ms\n  resolve_cooldown: 1m\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := configCommand(t, "")
			cmd.Flags().String("resolve-urls", "true", "")
			if tc.flag != "" {
				if err := cmd.Flags().Set("resolve-urls", tc.flag); err != nil {
					t.Fatal(err)
				}
			}
			if err := initializeConfig(cmd); err != nil {
				t.Fatal(err)
			}
			want := core.DefaultURLResolveConfig()
			want.URLs = tc.want
			if tc.file != "" {
				want.Concurrency, want.Timeout, want.Cooldown = 2, 750*time.Millisecond, time.Minute
			}
			if got := config.GoogleConfig.URLResolveConfig; got != want {
				t.Fatalf("Google resolve config = %+v, want %+v", got, want)
			}
		})
	}
}

func TestInitializeConfigRejectsInvalidGoogleResolve(t *testing.T) {
	for _, setting := range []string{
		"resolve_urls: invalid", "resolve_concurrency: 0", "resolve_timeout: -1s", "resolve_cooldown: 0s",
	} {
		t.Run(setting, func(t *testing.T) {
			isolateConfig(t)
			if err := os.WriteFile("config.yaml", []byte("google:\n  "+setting+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := initializeConfig(configCommand(t, "")); err == nil {
				t.Fatal("expected invalid Google resolve config to fail")
			}
		})
	}
}

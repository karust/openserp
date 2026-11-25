package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestInitializeConfigSocketMode(t *testing.T) {
	for _, tc := range []struct {
		name, file, env, flag, want string
		wantErr                     bool
	}{
		{name: "defaults"},
		{name: "quoted YAML", file: "\"0660\"", want: "0660"},
		{name: "numeric YAML", file: "0660", wantErr: true},
		{name: "env without YAML", env: "0660", want: "0660"},
		{name: "env overrides YAML", file: "0660", env: "0600", want: "0600"},
		{name: "flag overrides env", env: "0600", flag: "0660", want: "0660"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("OPENSERP_SERVER_SOCKET_MODE", tc.env)
			if tc.file != "" {
				if err := os.WriteFile("config.yaml", []byte("server:\n  socket_mode: "+tc.file+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := configCommand(t, "")
			cmd.Flags().String("socket-mode", "", "")
			if tc.flag != "" {
				if err := cmd.Flags().Set("socket-mode", tc.flag); err != nil {
					t.Fatal(err)
				}
			}
			err := initializeConfig(cmd)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "must be a quoted octal string") {
					t.Fatalf("expected a socket mode type error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := config.Server.SocketMode; got != tc.want {
				t.Fatalf("socket mode = %q, want %q", got, tc.want)
			}
		})
	}
}

package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/karust/openserp/core"
)

func TestInitializeConfigDNSServer(t *testing.T) {
	restoreDNS := core.CustomDNSServer()
	t.Cleanup(func() { _ = core.SetCustomDNSServer(restoreDNS) })

	t.Run("default empty", func(t *testing.T) {
		isolateConfig(t)
		if err := initializeConfig(configCommand(t, "")); err != nil {
			t.Fatal(err)
		}
		if config.DNSServer != "" || core.CustomDNSServer() != "" {
			t.Fatalf("dns_server = %q/%q, want empty", config.DNSServer, core.CustomDNSServer())
		}
	})

	t.Run("config file", func(t *testing.T) {
		isolateConfig(t)
		if err := os.WriteFile("config.yaml", []byte("dns_server: 8.8.8.8:53\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := initializeConfig(configCommand(t, "")); err != nil {
			t.Fatal(err)
		}
		if config.DNSServer != "8.8.8.8:53" || core.CustomDNSServer() != "8.8.8.8:53" {
			t.Fatalf("dns_server = %q/%q, want 8.8.8.8:53", config.DNSServer, core.CustomDNSServer())
		}
	})

	t.Run("invalid rejected", func(t *testing.T) {
		isolateConfig(t)
		if err := os.WriteFile("config.yaml", []byte("dns_server: bogus\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := initializeConfig(configCommand(t, "")); err == nil || !strings.Contains(err.Error(), "dns_server") {
			t.Fatalf("expected dns_server error, got %v", err)
		}
	})

	t.Run("env override", func(t *testing.T) {
		isolateConfig(t)
		t.Setenv("OPENSERP_DNS_SERVER", "1.1.1.1:53")
		if err := initializeConfig(configCommand(t, "")); err != nil {
			t.Fatal(err)
		}
		if config.DNSServer != "1.1.1.1:53" {
			t.Fatalf("dns_server = %q, want 1.1.1.1:53", config.DNSServer)
		}
	})

	t.Run("flag beats config", func(t *testing.T) {
		isolateConfig(t)
		if err := os.WriteFile("config.yaml", []byte("dns_server: 8.8.8.8:53\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := configCommand(t, "")
		cmd.Flags().String("dns-server", "", "")
		if err := cmd.Flags().Set("dns-server", "9.9.9.9:53"); err != nil {
			t.Fatal(err)
		}
		if err := initializeConfig(cmd); err != nil {
			t.Fatal(err)
		}
		if config.DNSServer != "9.9.9.9:53" {
			t.Fatalf("dns_server = %q, want 9.9.9.9:53", config.DNSServer)
		}
	})
}

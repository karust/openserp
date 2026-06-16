package cmd

import (
	"strings"
	"testing"

	"github.com/karust/openserp/core"
	extractpkg "github.com/karust/openserp/extract"
)

func TestNormalizeCLIExtractTop(t *testing.T) {
	tests := []struct {
		name    string
		raw     int
		want    int
		wantErr bool
	}{
		{name: "disabled", raw: 0, want: 0},
		{name: "one", raw: 1, want: 1},
		{name: "clamped", raw: 20, want: maxCLIExtractTop},
		{name: "negative", raw: -1, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeCLIExtractTop(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("normalizeCLIExtractTop(%d) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestApplyCLIExtractFlagSetsAutoMode(t *testing.T) {
	previous := config
	config.Extract = extractpkg.DefaultConfig()
	defer func() { config = previous }()

	query := core.Query{Text: "weather today"}
	if err := applyCLIExtractFlag(&query, 2); err != nil {
		t.Fatalf("applyCLIExtractFlag() error = %v", err)
	}
	if !query.Extract {
		t.Fatal("expected query.Extract")
	}
	if query.ExtractTop != 2 {
		t.Fatalf("ExtractTop = %d, want 2", query.ExtractTop)
	}
	if query.ExtractMode != string(extractpkg.ModeAuto) {
		t.Fatalf("ExtractMode = %q, want auto", query.ExtractMode)
	}
}

func TestApplyCLIExtractFlagRequiresEnabledConfig(t *testing.T) {
	previous := config
	config.Extract = extractpkg.Config{Enabled: false}
	defer func() { config = previous }()

	query := core.Query{Text: "weather today"}
	err := applyCLIExtractFlag(&query, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %q, want disabled message", err.Error())
	}
}

func TestSearchCommandHasExtractFlag(t *testing.T) {
	if searchCMD.Flags().Lookup("extract") == nil {
		t.Fatal("expected search command to expose --extract")
	}
}

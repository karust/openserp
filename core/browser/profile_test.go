package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectProfile(t *testing.T) {
	tests := []struct {
		name         string
		engine       string
		region       string
		wantLocale   string
		wantTimezone string
	}{
		{
			name:         "google ru lane",
			engine:       "google",
			region:       "ru",
			wantLocale:   "ru-RU",
			wantTimezone: "Europe/Moscow",
		},
		{
			name:         "yandex defaults to ru",
			engine:       "yandex",
			region:       "",
			wantLocale:   "ru-RU",
			wantTimezone: "Europe/Moscow",
		},
		{
			name:         "google default lane uses us profile",
			engine:       "google",
			region:       "en",
			wantLocale:   "en-US",
			wantTimezone: "America/New_York",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := SelectProfile(tt.engine, tt.region)
			if profile.ID == "" {
				t.Fatal("expected non-empty profile ID")
			}
			if profile.Locale != tt.wantLocale {
				t.Fatalf("expected locale %q, got %q", tt.wantLocale, profile.Locale)
			}
			if profile.Timezone != tt.wantTimezone {
				t.Fatalf("expected timezone %q, got %q", tt.wantTimezone, profile.Timezone)
			}
			if profile.Platform == "" {
				t.Fatal("expected non-empty platform")
			}
		})
	}
}

func TestNormalizeRegion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: "ru", want: "ru"},
		{input: "RU", want: "ru"},
		{input: "ru-RU", want: "ru"},
		{input: "ru_RU", want: "ru"},
		{input: "ru-RU,ru;q=0.9", want: "ru"},
		{input: "en-US,en;q=0.9", want: "us"},
		{input: "de", want: "us"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeRegion(tt.input); got != tt.want {
				t.Fatalf("NormalizeRegion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadProfilesFromJSON(t *testing.T) {
	originalCatalog, originalLaneProfiles, originalDefaultRegions := snapshotProfileState()
	t.Cleanup(func() {
		restoreProfileState(originalCatalog, originalLaneProfiles, originalDefaultRegions)
	})

	path := filepath.Join(t.TempDir(), "profiles.json")
	payload := `{
		"profiles": [
			{
				"id": "custom-ru",
				"user_agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
				"uach_brands": [
					{"brand": "Chromium", "version": "136"}
				],
				"uach_full_version_list": [
					{"brand": "Chromium", "version": "136.0.0.0"}
				],
				"platform": "Linux",
				"platform_version": "6.0.0",
				"architecture": "x86",
				"mobile": false,
				"accept_language": "ru-RU,ru;q=0.9",
				"navigator_langs": ["ru-RU"],
				"locale": "ru-RU",
				"timezone": "Europe/Moscow",
				"viewport": {"width": 1920, "height": 1080}
			}
		],
		"lane_profile_ids": {
			"google:ru-RU": "custom-ru"
		},
		"default_region_by_engine": {
			"google": "ru-RU"
		}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write profiles json: %v", err)
	}

	if err := LoadProfilesFromJSON(path); err != nil {
		t.Fatalf("load profiles json: %v", err)
	}

	if got := DefaultRegionForEngine("google"); got != "ru" {
		t.Fatalf("expected google default region ru, got %q", got)
	}

	profile := SelectProfile("google", "")
	if profile.ID != "custom-ru" {
		t.Fatalf("expected custom profile id custom-ru, got %q", profile.ID)
	}
	if profile.Timezone != "Europe/Moscow" {
		t.Fatalf("expected timezone Europe/Moscow, got %q", profile.Timezone)
	}
}

func snapshotProfileState() (map[string]Profile, map[string]string, map[string]string) {
	profileCatalogMu.RLock()
	defer profileCatalogMu.RUnlock()

	catalogCopy := make(map[string]Profile, len(catalog))
	for k, v := range catalog {
		catalogCopy[k] = v
	}

	laneProfilesCopy := make(map[string]string, len(laneProfileIDs))
	for k, v := range laneProfileIDs {
		laneProfilesCopy[k] = v
	}

	defaultRegionsCopy := make(map[string]string, len(defaultRegionByEngine))
	for k, v := range defaultRegionByEngine {
		defaultRegionsCopy[k] = v
	}

	return catalogCopy, laneProfilesCopy, defaultRegionsCopy
}

func restoreProfileState(catalogState map[string]Profile, laneProfiles map[string]string, defaultRegions map[string]string) {
	profileCatalogMu.Lock()
	defer profileCatalogMu.Unlock()

	catalog = catalogState
	laneProfileIDs = laneProfiles
	defaultRegionByEngine = defaultRegions
}

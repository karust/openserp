package browser

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
)

type BrandVersion struct {
	Brand   string `json:"brand"`
	Version string `json:"version"`
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Profile struct {
	ID              string         `json:"id"`
	UserAgent       string         `json:"user_agent"`
	UACHBrands      []BrandVersion `json:"uach_brands"`
	UACHFullVerList []BrandVersion `json:"uach_full_version_list"`
	Platform        string         `json:"platform"`
	PlatformVersion string         `json:"platform_version"`
	Architecture    string         `json:"architecture"`
	Bitness         string         `json:"bitness"`
	Mobile          bool           `json:"mobile"`
	AcceptLanguage  string         `json:"accept_language"`
	NavigatorLangs  []string       `json:"navigator_langs"`
	Locale          string         `json:"locale"`
	Timezone        string         `json:"timezone"`
	Viewport        Viewport       `json:"viewport"`
	WebGLVendor     string         `json:"webgl_vendor"`
	WebGLRenderer   string         `json:"webgl_renderer"`
	Tags            []string       `json:"tags"`
	Weight          int            `json:"weight"`
}

type catalogConfig struct {
	Profiles              []Profile         `json:"profiles"`
	LaneProfileIDs        map[string]string `json:"lane_profile_ids"`
	DefaultRegionByEngine map[string]string `json:"default_region_by_engine"`
}

const (
	ProfileChromeWinUS   = "chrome-win-uhd620"
	ProfileChromeWinRU   = "chrome-win-ru"
	ProfileChromeMacUS   = "chrome-macos-intel-iris"
	ProfileChromeLinuxUS = "chrome-linux-mesa-uhd620"
	ProfileChromeLinuxRU = "chrome-linux-ru"
)

//go:embed profiles.json
var defaultProfilesJSON []byte

//go:embed patch.js
var PatchJS []byte

var profileCatalogMu sync.RWMutex

var catalog = map[string]Profile{}

var laneProfileIDs = map[string]string{}

var defaultRegionByEngine = map[string]string{}

func init() {
	if err := loadProfilesFromJSONBytes(defaultProfilesJSON); err != nil {
		panic(fmt.Sprintf("load embedded browser profiles: %v", err))
	}
}

func LoadProfilesFromJSON(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read profiles json %q: %w", path, err)
	}

	if err := loadProfilesFromJSONBytes(data); err != nil {
		return fmt.Errorf("parse profiles json %q: %w", path, err)
	}
	return nil
}

func loadProfilesFromJSONBytes(data []byte) error {
	var cfg catalogConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if len(cfg.Profiles) == 0 {
		return fmt.Errorf("profiles list is empty")
	}

	nextCatalog := make(map[string]Profile, len(cfg.Profiles))
	for i, profile := range cfg.Profiles {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			return fmt.Errorf("profiles[%d].id is empty", i)
		}
		if strings.TrimSpace(profile.UserAgent) == "" {
			return fmt.Errorf("profiles[%d].user_agent is empty", i)
		}
		if _, exists := nextCatalog[profile.ID]; exists {
			return fmt.Errorf("duplicate profile id %q", profile.ID)
		}
		nextCatalog[profile.ID] = profile
	}

	nextLaneProfileIDs := make(map[string]string, len(cfg.LaneProfileIDs))
	for rawLaneKey, profileID := range cfg.LaneProfileIDs {
		laneKey, err := normalizeLaneKey(rawLaneKey)
		if err != nil {
			return err
		}
		profileID = strings.TrimSpace(profileID)
		if profileID == "" {
			return fmt.Errorf("lane profile id for %q is empty", laneKey)
		}
		if _, exists := nextCatalog[profileID]; !exists {
			return fmt.Errorf("lane %q references unknown profile id %q", laneKey, profileID)
		}
		nextLaneProfileIDs[laneKey] = profileID
	}

	nextDefaultRegionByEngine := map[string]string{
		"yandex": "ru",
	}
	for engine, region := range cfg.DefaultRegionByEngine {
		engine = NormalizeEngine(engine)
		if engine == "" {
			return fmt.Errorf("default_region_by_engine contains empty engine key")
		}
		nextDefaultRegionByEngine[engine] = normalizeConfiguredRegion(region)
	}

	profileCatalogMu.Lock()
	catalog = nextCatalog
	laneProfileIDs = nextLaneProfileIDs
	defaultRegionByEngine = nextDefaultRegionByEngine
	profileCatalogMu.Unlock()

	return nil
}

func normalizeLaneKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid lane key %q, expected engine:region", value)
	}
	engine := NormalizeEngine(parts[0])
	if engine == "" {
		return "", fmt.Errorf("invalid lane key %q, engine is empty", value)
	}
	region := normalizeConfiguredRegion(parts[1])
	return engine + ":" + region, nil
}

func normalizeConfiguredRegion(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		return "us"
	}
	return NormalizeRegion(region)
}

func Catalog() []Profile {
	profileCatalogMu.RLock()
	defer profileCatalogMu.RUnlock()

	out := make([]Profile, 0, len(catalog))
	for _, profile := range catalog {
		out = append(out, profile)
	}
	return out
}

// SelectProfile returns a deterministic profile for the given engine and region.
// It respects lane_profile_ids overrides and falls back to the OS-preferred default.
// Used by tests and single-instance callers; internally delegates to SelectProfileForSession with empty salt.
func SelectProfile(engine string, region string) Profile {
	return SelectProfileForSession(engine, region, "")
}

// SelectProfileForSession picks a profile for (engine, region, salt).
// If a lane_profile_ids override exists it is always honoured.
// Empty salt picks the first eligible profile (same as SelectProfile).
// Non-empty salt uses weighted selection seeded by FNV-1a hash of salt,
// giving each session a stable but varied profile.
func SelectProfileForSession(engine, region, salt string) Profile {
	engine = NormalizeEngine(engine)
	region = NormalizeRegion(region)
	if region == "" {
		region = DefaultRegionForEngine(engine)
	}
	laneKey := engine + ":" + region

	profileCatalogMu.RLock()
	profileID, ok := laneProfileIDs[laneKey]
	profileCatalogMu.RUnlock()
	if ok {
		return profileByID(profileID)
	}

	pool := eligibleProfiles(engine, region)
	return pickWeighted(pool, salt)
}

type weightedProfile struct {
	profile Profile
	weight  int
}

// eligibleProfiles builds the weighted pool for (engine, region).
// Linux profiles are preferred 4x on linux runtime; Windows 4x on windows; macOS 4x on darwin.
// Profiles tagged "ru" are included only when region == "ru"; "ru"-tagged profiles are excluded otherwise.
func eligibleProfiles(engine, region string) []weightedProfile {
	profileCatalogMu.RLock()
	snap := make([]Profile, 0, len(catalog))
	for _, p := range catalog {
		snap = append(snap, p)
	}
	profileCatalogMu.RUnlock()

	// Stable ordering so empty-salt picks are deterministic across map iterations.
	slices.SortFunc(snap, func(a, b Profile) int {
		return strings.Compare(a.ID, b.ID)
	})

	goos := runtime.GOOS

	var pool []weightedProfile
	for _, p := range snap {
		isRu := slices.Contains(p.Tags, "ru")
		if region == "ru" && !isRu {
			continue
		}
		if region != "ru" && isRu {
			continue
		}

		w := p.Weight
		if w <= 0 {
			w = 1
		}

		platformLower := strings.ToLower(p.Platform)
		switch goos {
		case "linux":
			if platformLower == "linux" {
				w *= 4
			}
		case "windows":
			if platformLower == "windows" {
				w *= 4
			}
		case "darwin":
			if platformLower == "macos" {
				w *= 4
			}
		}

		pool = append(pool, weightedProfile{profile: p, weight: w})
	}

	return pool
}

// pickWeighted selects a profile from pool using FNV-1a hash of salt modulo total weight.
// Empty salt returns the first profile in the pool (deterministic for tests).
func pickWeighted(pool []weightedProfile, salt string) Profile {
	if len(pool) == 0 {
		return profileByID(defaultProfileID("us"))
	}
	if salt == "" {
		return pool[0].profile
	}

	total := 0
	for _, wp := range pool {
		total += wp.weight
	}
	if total <= 0 {
		return pool[0].profile
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(salt))
	idx := int(h.Sum32()) % total

	cumulative := 0
	for _, wp := range pool {
		cumulative += wp.weight
		if idx < cumulative {
			return wp.profile
		}
	}
	return pool[len(pool)-1].profile
}

func LaneKey(engine string, region string) string {
	engine = NormalizeEngine(engine)
	if engine == "" {
		engine = "unknown"
	}

	region = NormalizeRegion(region)
	if region == "" {
		region = DefaultRegionForEngine(engine)
	}

	return engine + ":" + region
}

func DefaultRegionForEngine(engine string) string {
	engine = NormalizeEngine(engine)

	profileCatalogMu.RLock()
	defer profileCatalogMu.RUnlock()

	if region, ok := defaultRegionByEngine[engine]; ok {
		return region
	}
	return "us"
}

func NormalizeEngine(engine string) string {
	return strings.ToLower(strings.TrimSpace(engine))
}

func NormalizeRegion(region string) string {
	region = strings.TrimSpace(strings.ToLower(region))
	if region == "" {
		return ""
	}

	if idx := strings.Index(region, ","); idx >= 0 {
		region = region[:idx]
	}
	if idx := strings.Index(region, ";"); idx >= 0 {
		region = region[:idx]
	}

	region = strings.ReplaceAll(region, "_", "-")
	if idx := strings.Index(region, "-"); idx >= 0 {
		region = region[:idx]
	}

	switch region {
	case "ru", "be", "kz", "ky":
		return "ru"
	default:
		return "us"
	}
}

// ProfileByID looks up a profile by exact ID. Returns (profile, true) when found,
// (zero, false) when the ID is not in the catalog. Unlike the internal profileByID,
// it does not fall back to a default; the caller decides what to do on miss.
func ProfileByID(profileID string) (Profile, bool) {
	profileCatalogMu.RLock()
	defer profileCatalogMu.RUnlock()
	profile, ok := catalog[strings.TrimSpace(profileID)]
	return profile, ok
}

func profileByID(profileID string) Profile {
	profileCatalogMu.RLock()
	defer profileCatalogMu.RUnlock()

	if profile, ok := catalog[profileID]; ok {
		return profile
	}
	if fallback, ok := catalog[defaultProfileID("us")]; ok {
		return fallback
	}
	for _, profile := range catalog {
		return profile
	}
	return Profile{}
}

func defaultProfileID(region string) string {
	region = NormalizeRegion(region)
	if region == "" {
		region = "us"
	}

	switch runtime.GOOS {
	case "windows":
		if region == "ru" {
			return ProfileChromeWinRU
		}
		return ProfileChromeWinUS
	case "darwin":
		return ProfileChromeMacUS
	default:
		if region == "ru" {
			return ProfileChromeLinuxRU
		}
		return ProfileChromeLinuxUS
	}
}

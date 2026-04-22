package detectors

import (
	"fmt"
	"sort"
	"strings"

	"github.com/karust/openserp/core/fpcheck"
)

var standardDetectorFactories = []struct {
	name string
	new  func() fpcheck.Detector
}{
	{name: "sannysoft", new: NewSannysoft},
	{name: "rebrowser", new: NewRebrowser},
	{name: "browserscan", new: NewBrowserScan},
	{name: "pixelscan", new: NewPixelScan},
	{name: "deviceandbrowser", new: NewDeviceAndBrowser},
}

func All() []fpcheck.Detector {
	detectors := make([]fpcheck.Detector, 0, len(standardDetectorFactories))
	for _, item := range standardDetectorFactories {
		detectors = append(detectors, item.new())
	}
	return detectors
}

func Select(name string, customURL string) ([]fpcheck.Detector, error) {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" || trimmed == "all" {
		return All(), nil
	}

	if IsCustom(trimmed) {
		customDetector, err := NewCustom(customURL)
		if err != nil {
			return nil, err
		}
		return []fpcheck.Detector{customDetector}, nil
	}

	for _, item := range standardDetectorFactories {
		if strings.EqualFold(item.name, trimmed) {
			return []fpcheck.Detector{item.new()}, nil
		}
	}

	return nil, fmt.Errorf("unknown detector %q (allowed: %s)", name, strings.Join(Names(), ","))
}

func Names() []string {
	names := make([]string, 0, len(standardDetectorFactories)+1)
	for _, item := range standardDetectorFactories {
		names = append(names, item.name)
	}
	names = append(names, customDetectorName)
	sort.Strings(names)
	return names
}

func IsCustom(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), customDetectorName)
}

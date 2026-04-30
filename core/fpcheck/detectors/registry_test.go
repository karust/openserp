package detectors

import "testing"

func TestSelectCustomRequiresURL(t *testing.T) {
	_, err := Select("custom", "")
	if err == nil {
		t.Fatal("expected custom detector selection without URL to fail")
	}
}

func TestSelectCustomAcceptsHTTPSURL(t *testing.T) {
	detectorList, err := Select("custom", "https://localhost:9000")
	if err != nil {
		t.Fatalf("expected custom detector URL to be accepted, got %v", err)
	}
	if len(detectorList) != 1 {
		t.Fatalf("expected one detector, got %d", len(detectorList))
	}
	if detectorList[0].Name() != "custom" {
		t.Fatalf("expected custom detector, got %q", detectorList[0].Name())
	}
	if detectorList[0].URL() != "https://localhost:9000" {
		t.Fatalf("expected normalized custom URL to be preserved, got %q", detectorList[0].URL())
	}
	custom, ok := detectorList[0].(Custom)
	if !ok {
		t.Fatalf("expected custom detector type, got %T", detectorList[0])
	}
	if custom.Selector() != "body" {
		t.Fatalf("expected default custom selector body, got %q", custom.Selector())
	}
}

func TestSelectCustomAcceptsSelector(t *testing.T) {
	detectorList, err := SelectWithCustomSelector("custom", "https://localhost:9000", "pre")
	if err != nil {
		t.Fatalf("expected custom detector URL to be accepted, got %v", err)
	}

	custom, ok := detectorList[0].(Custom)
	if !ok {
		t.Fatalf("expected custom detector type, got %T", detectorList[0])
	}
	if custom.Selector() != "pre" {
		t.Fatalf("expected custom selector pre, got %q", custom.Selector())
	}
}

func TestNamesIncludesCustom(t *testing.T) {
	names := Names()
	found := false
	for _, name := range names {
		if name == "custom" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected names to include custom detector, got %#v", names)
	}
}

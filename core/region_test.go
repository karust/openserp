package core

import "testing"

// The full ResolveRegion behavior is exercised in the core/region subpackage.
// This test only verifies the core.* re-export wiring stays intact.
func TestResolveRegionReExport(t *testing.T) {
	got := ResolveRegion("Berlin")
	if got.GoogleCanonical != "Berlin,Berlin,Germany" {
		t.Errorf("GoogleCanonical = %q, want %q", got.GoogleCanonical, "Berlin,Berlin,Germany")
	}
	if cc := CountryFromRegion("en-GB"); cc != "GB" {
		t.Errorf("CountryFromRegion(en-GB) = %q, want GB", cc)
	}
	if loc := ParseLocale("de_AT"); loc.Language != "de" || loc.Country != "AT" {
		t.Errorf("ParseLocale(de_AT) = %+v, want {de AT}", loc)
	}
}

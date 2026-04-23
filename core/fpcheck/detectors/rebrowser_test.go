package detectors

import "testing"

func TestRebrowserChecksToDetections_UsesIconAndRating(t *testing.T) {
	checks := []rebrowserCheck{
		{
			Type:   "navigatorWebdriver",
			Icon:   "🔴",
			Rating: -1,
			Note:   "Own properties detected",
		},
		{
			Type:   "runtimeEnableLeak",
			Icon:   "",
			Rating: 1,
			Note:   "runtime leak",
		},
		{
			Type:   "viewport",
			Icon:   "🟢",
			Rating: 1,
			Note:   "looks fine",
		},
	}

	detections := rebrowserChecksToDetections(checks)

	if !detections["navigatorwebdriver"].Detected {
		t.Fatal("expected red icon check to be detected")
	}
	if !detections["runtimeenableleak"].Detected {
		t.Fatal("expected rating>=1 check to be detected")
	}
	if detections["viewport"].Detected {
		t.Fatal("expected green icon check to be not detected")
	}
}

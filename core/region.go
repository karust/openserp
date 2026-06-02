package core

import "github.com/karust/openserp/core/region"

// Region resolution lives in the dependency-free github.com/karust/openserp/core/region
// subpackage so lightweight consumers that only need geotargeting can import it
// without pulling in core's browser/scraping dependencies. The aliases and
// wrappers below preserve the historical core.* API for existing callers.

// RegionTarget is the resolved, per-engine targeting for a user-supplied region
// hint. See region.RegionTarget for details.
type RegionTarget = region.RegionTarget

// ResolveRegion turns a free-text region hint into per-engine targeting.
// See region.ResolveRegion for accepted inputs and semantics.
func ResolveRegion(hint string) RegionTarget {
	return region.ResolveRegion(hint)
}

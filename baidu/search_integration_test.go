//go:build integration
// +build integration

package baidu

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchBaidu(t *testing.T) {
	ithelper.RunEngineTests(t, func(b *core.Browser) core.SearchEngine {
		return New(*b, ithelper.EngineOptions())
	})
}

//go:build integration
// +build integration

package ecosia

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchEcosia(t *testing.T) {
	ithelper.RunEngineTests(t, func(b *core.Browser) core.SearchEngine {
		return New(*b, ithelper.EngineOptions())
	})
}

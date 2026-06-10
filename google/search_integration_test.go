//go:build integration
// +build integration

package google

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchGoogle(t *testing.T) {
	ithelper.RunEngineTests(t, func(b *core.Browser) core.SearchEngine {
		return New(*b, core.SearchEngineOptions{})
	})
}

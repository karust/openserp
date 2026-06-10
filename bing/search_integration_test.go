//go:build integration
// +build integration

package bing

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchBing(t *testing.T) {
	ithelper.RunEngineTests(t, func(b *core.Browser) core.SearchEngine {
		return New(*b, core.SearchEngineOptions{})
	})
}

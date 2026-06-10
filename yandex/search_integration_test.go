//go:build integration
// +build integration

package yandex

import (
	"testing"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil/ithelper"
)

func TestSearchYandex(t *testing.T) {
	ithelper.RunEngineTests(t, func(b *core.Browser) core.SearchEngine {
		return New(*b, core.SearchEngineOptions{})
	})
}

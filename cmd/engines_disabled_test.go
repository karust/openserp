package cmd

import (
	"slices"
	"testing"

	"github.com/karust/openserp/bing"
	"github.com/karust/openserp/core"
)

func TestDisabledEnginesAreLeftOutOfServe(t *testing.T) {
	isolateConfig(t)
	t.Setenv("OPENSERP_BING_DISABLED", "true")
	if err := initializeConfig(configCommand(t, "")); err != nil {
		t.Fatal(err)
	}
	if !config.BingConfig.Disabled || config.GoogleConfig.Disabled {
		t.Fatalf("disabled: bing=%v google=%v, want true false", config.BingConfig.Disabled, config.GoogleConfig.Disabled)
	}

	var names []string
	for _, s := range browserEngineSpecs() {
		names = append(names, s.name)
	}
	if slices.Contains(names, "bing") || !slices.Contains(names, "google") || len(names) != len(engineSpecs())-1 {
		t.Fatalf("browser engines = %v, want every engine but bing", names)
	}
	if got := enabledEngineNames("google", "bing"); !slices.Equal(got, []string{"google"}) {
		t.Fatalf("enabledEngineNames = %v, want [google]", got)
	}
}

func TestBingSearchBoxConfig(t *testing.T) {
	isolateConfig(t)
	t.Setenv("OPENSERP_BING_SEARCH_BOX", "")
	if err := initializeConfig(configCommand(t, "")); err != nil {
		t.Fatal(err)
	}
	spec, _ := resolveEngineSpec("bing")
	if engine := spec.factory(core.Browser{}, spec.opts()).(*bing.Bing); !engine.SearchBox {
		t.Fatal("search_box should default to true")
	}

	t.Setenv("OPENSERP_BING_SEARCH_BOX", "false")
	if err := initializeConfig(configCommand(t, "")); err != nil {
		t.Fatal(err)
	}
	if engine := spec.factory(core.Browser{}, spec.opts()).(*bing.Bing); engine.SearchBox {
		t.Fatal("OPENSERP_BING_SEARCH_BOX=false should turn the search box off")
	}
}

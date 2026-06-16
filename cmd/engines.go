package cmd

import (
	"context"
	"io"

	"github.com/karust/openserp/baidu"
	"github.com/karust/openserp/bing"
	"github.com/karust/openserp/core"
	"github.com/karust/openserp/duckduckgo"
	"github.com/karust/openserp/ecosia"
	"github.com/karust/openserp/google"
	"github.com/karust/openserp/yandex"
)

// engineSpec is the single registry row for a search engine, driving CLI search,
// raw dispatch, serve's browserEngineSpecs, and the alias/validation strings.
// cfg points into the live config global; rawSearchFn is nil when an engine has
// no browserless mode.
type engineSpec struct {
	name        string
	aliases     []string
	factory     func(core.Browser, core.SearchEngineOptions) core.SearchEngine
	rawSearchFn func(context.Context, core.Query) ([]core.SearchResult, error)
	parseHTMLFn func(io.Reader) ([]core.SearchResult, error)
	cfg         *EngineConfig
}

func (s engineSpec) opts() core.SearchEngineOptions {
	return s.cfg.SearchEngineOptions
}

func engineSpecs() []engineSpec {
	return []engineSpec{
		{name: "google", factory: newEngine(google.New), rawSearchFn: google.Search, parseHTMLFn: google.ParseHTML, cfg: &config.GoogleConfig},
		{name: "yandex", factory: newEngine(yandex.New), rawSearchFn: yandex.Search, parseHTMLFn: yandex.ParseHTML, cfg: &config.YandexConfig},
		{name: "baidu", factory: newEngine(baidu.New), rawSearchFn: baidu.Search, parseHTMLFn: baidu.ParseHTML, cfg: &config.BaiduConfig},
		{name: "bing", factory: newEngine(bing.New), parseHTMLFn: bing.ParseHTML, cfg: &config.BingConfig},
		{name: "duckduckgo", aliases: []string{"duck", "ddg"}, factory: newEngine(duckduckgo.New), parseHTMLFn: duckduckgo.ParseHTML, cfg: &config.DuckDuckGoConfig},
		{name: "ecosia", factory: newEngine(ecosia.New), rawSearchFn: ecosia.Search, parseHTMLFn: ecosia.ParseHTML, cfg: &config.EcosiaConfig},
	}
}

// newEngine adapts a concrete pkg.New (returning *Engine) to the
// core.SearchEngine-typed factory the registry stores.
func newEngine[T core.SearchEngine](ctor func(core.Browser, core.SearchEngineOptions) T) func(core.Browser, core.SearchEngineOptions) core.SearchEngine {
	return func(b core.Browser, o core.SearchEngineOptions) core.SearchEngine {
		return ctor(b, o)
	}
}

// engineValidArgs returns every accepted engine token (canonical names +
// aliases) for cobra's OnlyValidArgs validation.
func engineValidArgs() []string {
	specs := engineSpecs()
	args := make([]string, 0, len(specs))
	for _, s := range specs {
		args = append(args, s.name)
		args = append(args, s.aliases...)
	}
	return args
}

// resolveEngineSpec returns the spec whose canonical name or alias matches raw
// (case/space already normalized by the caller), or false when unknown.
func resolveEngineSpec(raw string) (engineSpec, bool) {
	for _, s := range engineSpecs() {
		if s.name == raw {
			return s, true
		}
		for _, alias := range s.aliases {
			if alias == raw {
				return s, true
			}
		}
	}
	return engineSpec{}, false
}

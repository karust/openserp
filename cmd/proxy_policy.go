package cmd

import (
	"strings"

	"github.com/karust/openserp/core"
)

func buildEngineProxyPolicyMap() map[string]string {
	return map[string]string{
		"google":     config.GoogleConfig.Proxy,
		"yandex":     config.YandexConfig.Proxy,
		"baidu":      config.BaiduConfig.Proxy,
		"bing":       config.BingConfig.Proxy,
		"duckduckgo": config.DuckDuckGoConfig.Proxy,
		"ecosia":     config.EcosiaConfig.Proxy,
	}
}

func buildNormalizedProxyConfig(runtime string) (core.ProxyConfig, error) {
	return core.NormalizeProxyConfig(core.ProxyConfig{
		Runtime:        runtime,
		Proxies:        config.Proxies,
		EnginePolicies: buildEngineProxyPolicyMap(),
	})
}

func resolveEngineProxyPolicy(proxyCfg core.ProxyConfig, engineName string) core.ProxyPolicy {
	engineKey := strings.ToLower(strings.TrimSpace(engineName))
	return core.ResolveEffectiveProxyPolicy(proxyCfg.Proxies.Global, proxyCfg.EnginePolicies[engineKey])
}

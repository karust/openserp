package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/karust/openserp/baidu"
	"github.com/karust/openserp/bing"
	"github.com/karust/openserp/core"
	"github.com/karust/openserp/duckduckgo"
	"github.com/karust/openserp/google"
	"github.com/karust/openserp/yandex"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var searchCMD = &cobra.Command{
	Use:     "search",
	Aliases: []string{"find"},
	Short:   "Search results using chosen web search engine (google, yandex, baidu, bing, duckduckgo)",
	Args:    cobra.MatchAll(cobra.OnlyValidArgs, cobra.ExactArgs(2)),
	Run:     search,
}

func search(cmd *cobra.Command, args []string) {
	engineType := normalizeEngineArg(args[0])
	query := core.Query{
		Text:     args[1],
		Limit:    10,
		Filter:   true,
		Insecure: config.Server.Insecure,
	}

	captchaSolverEnabled, captchaSolverAPIKey, err := resolveCaptchaSolverConfig()
	if err != nil {
		logrus.WithError(err).Error(fmt.Sprintf("Error validating captcha solver config: %v", err))
		os.Exit(1)
	}

	proxyRuntime := core.ProxyRuntimeBrowser
	if config.Server.IsRawRequests {
		proxyRuntime = core.ProxyRuntimeRaw
	}

	proxyCfg, err := buildNormalizedProxyConfig(proxyRuntime)
	if err != nil {
		logrus.WithError(err).Error(fmt.Sprintf("Error validating proxy config: %v", err))
		return
	}

	policy := resolveEngineProxyPolicy(proxyCfg, engineType)

	selectedProxy, err := selectCLIProxy(proxyCfg, policy)
	if err != nil {
		logrus.WithError(err).Error(fmt.Sprintf("Error selecting proxy for %s: %v", engineType, err))
		return
	}

	if config.Server.IsRawRequests {
		query.ProxyURL = selectedProxy
	}

	logrus.WithFields(logrus.Fields{
		"engine":     engineType,
		"query_hash": core.QueryHashFromQuery(query),
	}).Info(fmt.Sprintf("Starting SERP search request using %s engine for query: %s", engineType, query.Text))

	var results []core.SearchResult
	if config.Server.IsRawRequests {
		logrus.WithField("engine", engineType).Info(fmt.Sprintf("Using raw requests mode for %s search", engineType))
		results, err = searchRaw(engineType, query)
	} else {
		logrus.WithField("engine", engineType).Info(fmt.Sprintf("Using browser mode for %s search", engineType))
		results, err = searchBrowser(engineType, query, selectedProxy, captchaSolverEnabled, captchaSolverAPIKey)
	}

	if err != nil {
		logrus.WithError(err).WithField("engine", engineType).Error(fmt.Sprintf("Error during %s search: %s", engineType, err))
		return
	}

	logrus.WithFields(logrus.Fields{
		"engine":        engineType,
		"results_count": len(results),
	}).Info(fmt.Sprintf("Successfully completed SERP search using %s engine, returned %d results", engineType, len(results)))

	b, err := json.MarshalIndent(results, "", " ")
	if err != nil {
		logrus.Error(err)
		return
	}

	fmt.Println(string(b))
}

func searchBrowser(engineType string, query core.Query, browserProxyURL string, captchaSolverEnabled bool, captchaSolverAPIKey string) ([]core.SearchResult, error) {
	var engine core.SearchEngine
	blockedResourceTypes := core.MustParseBlockedResourceTypes(config.App.BlockResources)
	if core.IsAuthenticatedSocksProxyURL(browserProxyURL) {
		return nil, fmt.Errorf(
			"%w: browser runtime does not support authenticated SOCKS proxy %s",
			core.ErrProxyUnavailable,
			core.MaskProxyURL(browserProxyURL),
		)
	}

	opts := core.BrowserOpts{
		IsHeadless:           !config.App.IsBrowserHead,
		IsLeakless:           config.App.IsLeakless,
		Timeout:              time.Second * time.Duration(config.App.Timeout),
		LeavePageOpen:        config.App.IsLeaveHead,
		CaptchaSolverEnabled: captchaSolverEnabled,
		CaptchaSolverApiKey:  captchaSolverAPIKey,
		BrowserPath:          config.App.BrowserPath,
		ProxyURL:             browserProxyURL,
		Insecure:             config.Server.Insecure,
		BlockResourceTypes:   blockedResourceTypes,
		BlockTrackers:        config.App.BlockTrackers,
	}

	if config.Server.IsDebug {
		opts.IsHeadless = false
	}

	browser, err := core.NewBrowser(opts)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(engineType) {
	case "yandex":
		engine = yandex.New(*browser, config.YandexConfig.SearchEngineOptions)
	case "google":
		engine = google.New(*browser, config.GoogleConfig.SearchEngineOptions)
	case "baidu":
		engine = baidu.New(*browser, config.BaiduConfig.SearchEngineOptions)
	case "bing":
		engine = bing.New(*browser, config.BingConfig.SearchEngineOptions)
	case "duckduckgo":
		engine = duckduckgo.New(*browser, config.DuckDuckGoConfig.SearchEngineOptions)
	default:
		return nil, fmt.Errorf("no %q search engine found", engineType)
	}

	return engine.Search(context.Background(), query)
}

func searchRaw(engineType string, query core.Query) ([]core.SearchResult, error) {
	logrus.Warn("Browserless results are very inconsistent or may not even work!")
	ctx := context.Background()

	switch strings.ToLower(engineType) {
	case "yandex":
		return yandex.Search(ctx, query)
	case "google":
		return google.Search(ctx, query)
	case "baidu":
		return baidu.Search(ctx, query)
	case "bing":
		logrus.Warn("Bing does not support raw HTTP requests mode. Please use browser mode instead.")
		return nil, fmt.Errorf("bing does not support raw requests mode")
	case "duckduckgo":
		logrus.Warn("DuckDuckGo does not support raw HTTP requests mode. Please use browser mode instead.")
		return nil, fmt.Errorf("duckduckgo does not support raw requests mode")
	default:
		return nil, fmt.Errorf("no %q search engine found", engineType)
	}
}

func selectCLIProxy(proxyCfg core.ProxyConfig, policy core.ProxyPolicy) (string, error) {
	if policy.Mode == core.ProxyModeOff {
		return "", nil
	}

	if global := strings.TrimSpace(proxyCfg.Proxies.Global); global != "" {
		return global, nil
	}

	if proxyCfg.Registry == nil {
		return "", fmt.Errorf("%w: no proxy registry configured", core.ErrProxyUnavailable)
	}

	selected := proxyCfg.Registry.NextByTag(policy.Tag)
	if selected == "" {
		return "", fmt.Errorf("%w: no healthy proxy available for tag %q", core.ErrProxyUnavailable, policy.Tag)
	}

	return selected, nil
}

func normalizeEngineArg(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "duck":
		return "duckduckgo"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func init() {
	RootCmd.AddCommand(searchCMD)
}

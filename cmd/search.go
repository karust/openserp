package cmd

import (
	"encoding/json"
	"fmt"
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
	var err error
	engineType := args[0]
	query := core.Query{
		Text:     args[1],
		Limit:    10,
		ProxyURL: config.App.ProxyURL,
		Insecure: config.App.Insecure,
	}

	logrus.Infof("Starting SERP search request using %s engine for query: %s", engineType, query.Text)

	var results []core.SearchResult
	if config.App.IsRawRequests {
		logrus.Infof("Using raw requests mode for %s search", engineType)
		results, err = searchRaw(engineType, query)
	} else {
		logrus.Infof("Using browser mode for %s search", engineType)
		results, err = searchBrowser(engineType, query)
	}

	if err != nil {
		logrus.Errorf("Error during %s search: %s", engineType, err)
		return
	}

	logrus.Infof("Successfully completed SERP search using %s engine, returned %d results", engineType, len(results))

	b, err := json.MarshalIndent(results, "", " ")
	if err != nil {
		logrus.Error(err)
		return
	}

	fmt.Println(string(b))
}
func searchBrowser(engineType string, query core.Query) ([]core.SearchResult, error) {
	var engine core.SearchEngine

	opts := core.BrowserOpts{
		IsHeadless:          !config.App.IsBrowserHead, // Disable headless if browser head mode is set
		IsLeakless:          config.App.IsLeakless,
		Timeout:             time.Second * time.Duration(config.App.Timeout),
		LeavePageOpen:       config.App.IsLeaveHead,
		CaptchaSolverApiKey: config.Config2Capcha.ApiKey,
		ProxyURL:            config.App.ProxyURL,
		Insecure:            config.App.Insecure,
		UseStealth:          config.App.IsStealth,
	}

	if config.App.IsDebug {
		opts.IsHeadless = false
	}

	browser, err := core.NewBrowser(opts)
	if err != nil {
		logrus.Error(err)
	}

	switch strings.ToLower(engineType) {
	case "yandex":
		engine = yandex.New(*browser, config.YandexConfig)
	case "google":
		engine = google.New(*browser, config.GoogleConfig)
	case "baidu":
		engine = baidu.New(*browser, config.BaiduConfig)
	case "bing":
		engine = bing.New(*browser, config.BingConfig)
	case "duck":
		engine = duckduckgo.New(*browser, config.DuckDuckGoConfig)
	default:
		logrus.Infof("No `%s` search engine found", engineType)
	}

	return engine.Search(query)
}

func searchRaw(engineType string, query core.Query) ([]core.SearchResult, error) {
	logrus.Warn("Browserless results are very inconsistent or may not even work!")

	switch strings.ToLower(engineType) {
	case "yandex":
		return yandex.Search(query)
	case "google":
		return google.Search(query)
	case "baidu":
		return baidu.Search(query)
	case "bing":
		logrus.Warn("Bing does not support raw HTTP requests mode. Please use browser mode instead.")
		return nil, fmt.Errorf("bing does not support raw requests mode")
	case "duck":
		logrus.Warn("DuckDuckGo does not support raw HTTP requests mode. Please use browser mode instead.")
		return nil, fmt.Errorf("duckduckgo does not support raw requests mode")
	default:
		logrus.Infof("No `%s` search engine found", engineType)
	}
	return nil, nil
}

func init() {
	RootCmd.AddCommand(searchCMD)
}

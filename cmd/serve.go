package cmd

import (
	"time"

	"github.com/karust/openserp/baidu"
	"github.com/karust/openserp/core"
	"github.com/karust/openserp/google"
	"github.com/karust/openserp/yandex"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var serveCMD = &cobra.Command{
	Use:     "serve",
	Aliases: []string{"listen"},
	Short:   "Start HTTP server, to provide search engine results via API",
	Args:    cobra.MatchAll(cobra.NoArgs),
	Run:     serve,
}

func serve(cmd *cobra.Command, args []string) {
	opts := core.BrowserOpts{
		IsHeadless:          !config.App.IsBrowserHead, // Disable headless if browser head mode is set
		IsLeakless:          config.App.IsLeakless,
		Timeout:             time.Second * time.Duration(config.App.Timeout),
		LeavePageOpen:       config.App.IsLeaveHead,
		CaptchaSolverApiKey: config.Config2Capcha.ApiKey,
	}

	if config.App.IsDebug {
		opts.IsHeadless = false
	}

	browser, err := core.NewBrowser(opts)
	if err != nil {
		logrus.Error(err)
	}

	yand := yandex.New(*browser, config.YandexConfig)
	gogl := google.New(*browser, config.GoogleConfig)
	baidu := baidu.New(*browser, config.BaiduConfig)

	serv := core.NewServer(config.App.Host, config.App.Port, gogl, yand, baidu)
	serv.Listen()
}

func init() {
	RootCmd.AddCommand(serveCMD)
}

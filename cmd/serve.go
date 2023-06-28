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
		IsHeadless:    !appConf.IsBrowserHead, // Disable headless if browser head mode is set
		IsLeakless:    appConf.IsLeakless,
		Timeout:       time.Second * time.Duration(appConf.Timeout),
		LeavePageOpen: appConf.IsLeaveHead,
	}

	if appConf.IsDebug {
		opts.IsHeadless = false
	}

	browser, err := core.NewBrowser(opts)
	if err != nil {
		logrus.Error(err)
	}

	yand := yandex.New(*browser, appConf.YandexConfig)
	gogl := google.New(*browser, appConf.GoogleConfig)
	baidu := baidu.New(*browser, appConf.BaiduConfig)

	serv := core.NewServer(appConf.Host, appConf.Port, gogl, yand, baidu)
	serv.Listen()
}

func init() {
	RootCmd.AddCommand(serveCMD)
}

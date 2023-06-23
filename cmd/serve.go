package cmd

import (
	"time"

	"github.com/karust/googler/baidu"
	"github.com/karust/googler/core"
	"github.com/karust/googler/google"
	"github.com/karust/googler/yandex"
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
		IsHeadless: !appConf.IsBrowserHead,
		IsLeakless: appConf.IsLeakless,
		Timeout:    time.Second * time.Duration(appConf.Timeout),
	}

	browser, err := core.NewBrowser(opts)
	if err != nil {
		logrus.Error(err)
	}

	yand := yandex.New(*browser)
	gogl := google.New(*browser)
	baidu := baidu.New(*browser)

	serv := core.NewServer(appConf.Host, appConf.Port, gogl, yand, baidu)
	serv.Listen()
}

func init() {
	RootCmd.AddCommand(serveCMD)
}

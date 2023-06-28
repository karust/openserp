package cmd

import (
	"fmt"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	version                    = "0.2.1"
	defaultConfigFilename      = "config"
	envPrefix                  = "OPENSERP"
	replaceHyphenWithCamelCase = false
)

type AppConfig struct {
	Host          string
	Port          int
	Timeout       int
	ConfigPath    string
	IsBrowserHead bool                     `mapstructure:"head"`
	IsLeaveHead   bool                     `mapstructure:"leave_head"`
	IsLeakless    bool                     `mapstructure:"leakless"`
	IsDebug       bool                     `mapstructure:"debug"`
	IsVerbose     bool                     `mapstructure:"verbose"`
	IsRawRequests bool                     `mapstructure:"raw_requests"`
	GoogleConfig  core.SearchEngineOptions `mapstructure:"google"`
	YandexConfig  core.SearchEngineOptions `mapstructure:"yandex"`
	BaiduConfig   core.SearchEngineOptions `mapstructure:"baidu"`
}

var appConf = AppConfig{}

var RootCmd = &cobra.Command{
	Use:     "openserp",
	Short:   "Open SERP",
	Long:    `Search via Google, Yandex and Baidu`,
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		core.InitLogger(appConf.IsVerbose, appConf.IsDebug)
		err := initializeConfig(cmd)
		logrus.Debugf("Config: %+v", appConf)
		return err
	},
	// Run: func(cmd *cobra.Command, args []string) {
	// 	// Working with OutOrStdout/OutOrStderr allows us to unit test our command easier
	// 	//out := cmd.OutOrStdout()
	// 	logrus.Trace("Config:", appConf)
	// },
}

// Bind each cobra flag to its associated viper configuration (config file and environment variable)
func bindFlags(cmd *cobra.Command, vpr *viper.Viper) {
	cmd.Flags().VisitAll(func(flg *pflag.Flag) {
		configName := "app." + flg.Name
		//if replaceHyphenWithCamelCase {
		//	configName = strings.ReplaceAll(f.Name, "-", "")
		//}

		// Apply viper config value to the flag if viper has a value
		if !flg.Changed && vpr.IsSet(configName) {
			val := vpr.Get(configName)
			cmd.Flags().Set(flg.Name, fmt.Sprintf("%v", val))
		}
	})
}

// Initialize Viper
func initializeConfig(cmd *cobra.Command) error {
	v := viper.New()

	// Base name of the config file, without the file extension
	v.SetConfigName(defaultConfigFilename)
	v.AddConfigPath(".")

	// Return an error if we cannot parse the config file.
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return err
		}
	}

	v.SetEnvPrefix(envPrefix)

	// Bind environment variables to their equivalent keys with underscores
	//v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// Bind the current command's flags to viper
	bindFlags(cmd, v)

	return nil
}

func init() {
	RootCmd.PersistentFlags().IntVarP(&appConf.Port, "port", "p", 7070, "Port number to run server")
	RootCmd.PersistentFlags().StringVarP(&appConf.Host, "host", "a", "127.0.0.1", "Host address to run server")
	RootCmd.PersistentFlags().IntVarP(&appConf.Timeout, "timeout", "t", 30, "Timeout to fail request")
	RootCmd.PersistentFlags().StringVarP(&appConf.ConfigPath, "config", "c", "./config.yaml", "Configuration file path")
	RootCmd.PersistentFlags().BoolVarP(&appConf.IsVerbose, "verbose", "v", false, "Use verbose output")
	RootCmd.PersistentFlags().BoolVarP(&appConf.IsDebug, "debug", "d", false, "Use debug output. Disable headless browser")
	RootCmd.PersistentFlags().BoolVarP(&appConf.IsBrowserHead, "head", "", false, "Enable browser UI")
	RootCmd.PersistentFlags().BoolVarP(&appConf.IsLeakless, "leakless", "l", false, "Use leakless mode to insure browser instances are closed after search")
	RootCmd.PersistentFlags().BoolVarP(&appConf.IsRawRequests, "raw", "r", false, "Disable browser usage, use HTTP requests")
	RootCmd.PersistentFlags().BoolVarP(&appConf.IsLeaveHead, "leave", "", false, "Leave browser and tabs opened after search is made")
}

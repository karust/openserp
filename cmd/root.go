package cmd

import (
	"fmt"
	"strings"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	version               = "0.3"
	defaultConfigFilename = "config"
	envPrefix             = "OPENSERP"
)

type Config struct {
	App           AppConfig                `mapstructure:"app"`
	Config2Capcha Config2Captcha           `mapstructure:"2captcha"`
	GoogleConfig  core.SearchEngineOptions `mapstructure:"google"`
	YandexConfig  core.SearchEngineOptions `mapstructure:"yandex"`
	BaiduConfig   core.SearchEngineOptions `mapstructure:"baidu"`
}

type Config2Captcha struct {
	ApiKey string `mapstructure:"apikey"`
}

type AppConfig struct {
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	Timeout       int    `mapstructure:"timeout"`
	ConfigPath    string `mapstructure:"config_path"`
	IsBrowserHead bool   `mapstructure:"head"`
	IsLeaveHead   bool   `mapstructure:"leave_head"`
	IsLeakless    bool   `mapstructure:"leakless"`
	IsDebug       bool   `mapstructure:"debug"`
	IsVerbose     bool   `mapstructure:"verbose"`
	IsRawRequests bool   `mapstructure:"raw_requests"`
}

var config = Config{}

var RootCmd = &cobra.Command{
	Use:          "openserp",
	Short:        "Open SERP",
	Long:         `Get [Google, Yandex, Baidu] search engine results via API or CLI.`,
	Version:      version,
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		core.InitLogger(config.App.IsVerbose, config.App.IsDebug)

		err := initializeConfig(cmd)
		if err != nil {
			return err
		}

		logrus.Debugf("Final config: %+v", config)
		return nil
	},

	// Run: func(cmd *cobra.Command, args []string) {
	// 	// Working with OutOrStdout/OutOrStderr allows us to unit test our command easier
	// 	//out := cmd.OutOrStdout()
	// 	logrus.Trace("Config:", config)
	// },
}

// Bind each cobra flag to its associated viper configuration (config file and environment variable)
func bindFlags(cmd *cobra.Command, vpr *viper.Viper) {
	cmd.Flags().VisitAll(func(flg *pflag.Flag) {
		configName := "app." + flg.Name

		// Apply viper config value to the flag if viper has a value
		if flg.Changed {
			vpr.Set(configName, flg.Value)
		}
	})
}

// Initialize Viper
func initializeConfig(cmd *cobra.Command) error {
	v := viper.New()

	// Base name of the config file, without the file extension
	v.SetConfigName(defaultConfigFilename)
	v.AddConfigPath(".")

	// 1. Config. Return an error if we cannot parse the config file.
	err := v.ReadInConfig()
	if err != nil {
		err = fmt.Errorf("cannot read config: %v", err)
		logrus.Warn(err)
	}

	// 2. Env. Bind environment variables to their equivalent keys with underscores
	for _, key := range v.AllKeys() {
		envKey := envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		err := v.BindEnv(key, envKey)
		if err != nil {
			logrus.Errorf("Unable to bind ENV valye: %v", err)
		}
	}

	// 3. Cmd flags. Bind the current command's flags to viper
	bindFlags(cmd, v)

	// Dump Viper values to config struct
	err = v.Unmarshal(&config)
	if err != nil {
		return fmt.Errorf("cannot unmarshall config: %v", err)
	}

	if config.App.IsDebug {
		logrus.Debug("Viper config:")
		v.Debug()
	}
	return nil
}

func init() {
	RootCmd.PersistentFlags().IntVarP(&config.App.Port, "port", "p", 7070, "Port number to run server")
	RootCmd.PersistentFlags().StringVarP(&config.App.Host, "host", "a", "127.0.0.1", "Host address to run server")
	RootCmd.PersistentFlags().IntVarP(&config.App.Timeout, "timeout", "t", 30, "Timeout to fail request")
	RootCmd.PersistentFlags().StringVarP(&config.App.ConfigPath, "config", "c", "", "Configuration file path")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsVerbose, "verbose", "v", false, "Use verbose output")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsDebug, "debug", "d", false, "Use debug output. Disable headless browser")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsBrowserHead, "head", "", false, "Enable browser UI")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsLeakless, "leakless", "l", false, "Use leakless mode to insure browser instances are closed after search")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsRawRequests, "raw", "r", false, "Disable browser usage, use HTTP requests")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsLeaveHead, "leave", "", false, "Leave browser and tabs opened after search is made")
	RootCmd.PersistentFlags().StringVarP(&config.Config2Capcha.ApiKey, "2captcha_key", "", "", "2 captcha api key")
}

package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	version               = "0.5.6"
	defaultConfigFilename = "config"
	envPrefix             = "OPENSERP"
)

type Config struct {
	App              AppConfig                `mapstructure:"app"`
	Cache            CacheConfig              `mapstructure:"cache"`
	Resilience       ResilienceConfig         `mapstructure:"resilience"`
	CircuitBreaker   CircuitBreakerConfig     `mapstructure:"circuit_breaker"`
	CORS             CORSConfig               `mapstructure:"cors"`
	Config2Capcha    Config2Captcha           `mapstructure:"2captcha"`
	GoogleConfig     core.SearchEngineOptions `mapstructure:"google"`
	YandexConfig     core.SearchEngineOptions `mapstructure:"yandex"`
	BaiduConfig      core.SearchEngineOptions `mapstructure:"baidu"`
	BingConfig       core.SearchEngineOptions `mapstructure:"bing"`
	DuckDuckGoConfig core.SearchEngineOptions `mapstructure:"duckduckgo"`
}

type Config2Captcha struct {
	ApiKey string `mapstructure:"apikey"`
}

type AppConfig struct {
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	Timeout       int    `mapstructure:"timeout"`
	ConfigPath    string `mapstructure:"config_path"`
	BrowserPath   string `mapstructure:"browser_path"`
	IsBrowserHead bool   `mapstructure:"head"`
	IsLeaveHead   bool   `mapstructure:"leave_head"`
	IsLeakless    bool   `mapstructure:"leakless"`
	IsDebug       bool   `mapstructure:"debug"`
	IsVerbose     bool   `mapstructure:"verbose"`
	IsRawRequests bool   `mapstructure:"raw_requests"`
	ProxyURL      string `mapstructure:"proxy"`
	Insecure      bool   `mapstructure:"insecure"`
	IsStealth     bool   `mapstructure:"stealth"`
}

type CacheConfig struct {
	TTLSeconds int `mapstructure:"ttl_seconds"`
	MaxSize    int `mapstructure:"max_size"`
}

type ResilienceConfig struct {
	MaxRetries            int  `mapstructure:"max_retries"`
	AllowEndpointFallback bool `mapstructure:"allow_endpoint_fallback"`
}

type CircuitBreakerConfig struct {
	Failures        int `mapstructure:"failures"`
	RecoverySeconds int `mapstructure:"recovery_seconds"`
	Successes       int `mapstructure:"successes"`
}

type CORSConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	AllowOrigins string `mapstructure:"allow_origins"`
	AllowMethods string `mapstructure:"allow_methods"`
	AllowHeaders string `mapstructure:"allow_headers"`
	MaxAge       int    `mapstructure:"max_age"`
}

var config = Config{}

var flagToConfigKey = map[string]string{
	"config":                  "app.config_path",
	"browser-path":            "app.browser_path",
	"leave":                   "app.leave_head",
	"raw":                     "app.raw_requests",
	"2captcha_key":            "2captcha.apikey",
	"cache_ttl":               "cache.ttl_seconds",
	"cache_max_size":          "cache.max_size",
	"max_retries":             "resilience.max_retries",
	"allow_endpoint_fallback": "resilience.allow_endpoint_fallback",
	"cb_failures":             "circuit_breaker.failures",
	"cb_recovery":             "circuit_breaker.recovery_seconds",
	"cb_successes":            "circuit_breaker.successes",
}

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
		configName, ok := flagToConfigKey[flg.Name]
		if !ok {
			configName = "app." + flg.Name
		}

		if err := vpr.BindPFlag(configName, flg); err != nil {
			logrus.Errorf("Unable to bind flag %s: %v", flg.Name, err)
		}

		if flg.Changed {
			val, err := parseFlagValue(flg)
			if err != nil {
				logrus.Errorf("Unable to parse flag %s: %v", flg.Name, err)
				return
			}
			vpr.Set(configName, val)
		}
	})
}

func parseFlagValue(flg *pflag.Flag) (interface{}, error) {
	switch flg.Value.Type() {
	case "string":
		return flg.Value.String(), nil
	case "bool":
		return strconv.ParseBool(flg.Value.String())
	case "int":
		return strconv.Atoi(flg.Value.String())
	default:
		return flg.Value.String(), nil
	}
}

// Initialize Viper
func initializeConfig(cmd *cobra.Command) error {
	v := viper.New()
	setConfigDefaults(v)

	// Base name of the config file, without the file extension
	v.SetConfigName(defaultConfigFilename)
	v.AddConfigPath(".")

	// 1. Config file (lowest priority). Return an error if we cannot parse the config file.
	err := v.ReadInConfig()
	if err != nil {
		err = fmt.Errorf("cannot read config: %v", err)
		logrus.Warn(err)
	}

	// 2. Environment variables (medium priority). Bind environment variables to their equivalent keys with underscores
	for _, key := range v.AllKeys() {
		envKey := envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		err := v.BindEnv(key, envKey)
		if err != nil {
			logrus.Errorf("Unable to bind ENV valye: %v", err)
		}
	}

	// 3. Command flags (highest priority). Bind the current command's flags to viper
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

func setConfigDefaults(v *viper.Viper) {
	v.SetDefault("cache.ttl_seconds", 300)
	v.SetDefault("cache.max_size", 1000)
	// Keep stage2 defaults stable even when config file is absent.
	v.SetDefault("resilience.max_retries", 3)
	v.SetDefault("resilience.allow_endpoint_fallback", false)
	v.SetDefault("circuit_breaker.failures", 5)
	v.SetDefault("circuit_breaker.recovery_seconds", 60)
	v.SetDefault("circuit_breaker.successes", 2)
	v.SetDefault("cors.enabled", true)
	v.SetDefault("cors.allow_origins", "*")
	v.SetDefault("cors.allow_methods", "GET, POST, OPTIONS")
	v.SetDefault("cors.allow_headers", "Origin, Content-Type, Accept, Authorization")
	v.SetDefault("cors.max_age", 86400)
}

func init() {
	RootCmd.PersistentFlags().IntVarP(&config.App.Port, "port", "p", 7070, "Port number to run server")
	RootCmd.PersistentFlags().StringVarP(&config.App.Host, "host", "a", "127.0.0.1", "Host address to run server")
	RootCmd.PersistentFlags().IntVarP(&config.App.Timeout, "timeout", "t", 30, "Timeout to fail request")
	RootCmd.PersistentFlags().StringVarP(&config.App.ConfigPath, "config", "c", "", "Configuration file path")
	RootCmd.PersistentFlags().StringVarP(&config.App.BrowserPath, "browser-path", "", "", "Custom browser binary path (Chrome/Chromium/Edge/Brave..)")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsVerbose, "verbose", "v", false, "Use verbose output")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsDebug, "debug", "d", false, "Use debug output. Disable headless browser")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsBrowserHead, "head", "", false, "Enable browser UI")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsLeakless, "leakless", "l", false, "Use leakless mode to insure browser instances are closed after search")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsRawRequests, "raw", "r", false, "Disable browser usage, use HTTP requests")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsLeaveHead, "leave", "", false, "Leave browser and tabs opened after search is made")
	RootCmd.PersistentFlags().StringVarP(&config.Config2Capcha.ApiKey, "2captcha_key", "", "", "2 captcha api key")
	RootCmd.PersistentFlags().StringVarP(&config.App.ProxyURL, "proxy", "x", "", "HTTP or Socks5 proxy URL (e.g. http://user:pass@127.0.0.1:8080)")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsStealth, "stealth", "s", false, "Use stealth browser plugin")
	RootCmd.PersistentFlags().BoolVarP(&config.App.Insecure, "insecure", "k", false, "Allow insecure TLS connections")
	RootCmd.PersistentFlags().IntVar(&config.Cache.TTLSeconds, "cache_ttl", 300, "Cache TTL in seconds (0 to disable)")
	RootCmd.PersistentFlags().IntVar(&config.Cache.MaxSize, "cache_max_size", 1000, "Maximum number of cached responses")
	RootCmd.PersistentFlags().IntVar(&config.Resilience.MaxRetries, "max_retries", 3, "Max retry attempts per search engine (0 to disable)")
	RootCmd.PersistentFlags().BoolVar(&config.Resilience.AllowEndpointFallback, "allow_endpoint_fallback", false, "Allow dedicated endpoints to fallback to other engines")
	RootCmd.PersistentFlags().IntVar(&config.CircuitBreaker.Failures, "cb_failures", 5, "Consecutive failures before circuit breaker opens")
	RootCmd.PersistentFlags().IntVar(&config.CircuitBreaker.RecoverySeconds, "cb_recovery", 60, "Seconds before retrying an engine with open circuit")
	RootCmd.PersistentFlags().IntVar(&config.CircuitBreaker.Successes, "cb_successes", 2, "Consecutive successful half-open checks needed to close circuit")
}

package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/karust/openserp/core"
	browserprofile "github.com/karust/openserp/core/browser"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	version               = "0.7.6"
	defaultConfigFilename = "config"
	envPrefix             = "OPENSERP"
)

type Config struct {
	Server           ServerConfig         `mapstructure:"server"`
	App              AppConfig            `mapstructure:"app"`
	Proxies          core.ProxiesConfig   `mapstructure:"proxies"`
	Cache            CacheConfig          `mapstructure:"cache"`
	Resilience       ResilienceConfig     `mapstructure:"resilience"`
	CircuitBreaker   CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	CORS             CORSConfig           `mapstructure:"cors"`
	Captcha          CaptchaConfig        `mapstructure:"captcha"`
	Config2Capcha    Config2Captcha       `mapstructure:"2captcha"`
	GoogleConfig     EngineConfig         `mapstructure:"google"`
	YandexConfig     EngineConfig         `mapstructure:"yandex"`
	BaiduConfig      EngineConfig         `mapstructure:"baidu"`
	BingConfig       EngineConfig         `mapstructure:"bing"`
	DuckDuckGoConfig EngineConfig         `mapstructure:"duckduckgo"`
}

type Config2Captcha struct {
	ApiKey string `mapstructure:"apikey"`
}

type ServerConfig struct {
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	ConfigPath    string `mapstructure:"config_path"`
	IsDebug       bool   `mapstructure:"debug"`
	IsVerbose     bool   `mapstructure:"verbose"`
	IsRawRequests bool   `mapstructure:"raw_requests"`
	Insecure      bool   `mapstructure:"insecure"`
}

type AppConfig struct {
	Timeout        int           `mapstructure:"timeout"`
	BrowserPath    string        `mapstructure:"browser_path"`
	ProfilesJSON   string        `mapstructure:"profiles"`
	IsBrowserHead  bool          `mapstructure:"head"`
	IsLeaveHead    bool          `mapstructure:"leave_head"`
	IsLeakless     bool          `mapstructure:"leakless"`
	BlockResources string        `mapstructure:"block_resources"`
	BlockTrackers  bool          `mapstructure:"block_trackers"`
	DebugEndpoints bool          `mapstructure:"debug_endpoints"`
	LogFormat      string        `mapstructure:"log_format"`
	MaxProcesses   int           `mapstructure:"max_processes"`
	IdleTTL        time.Duration `mapstructure:"idle_ttl"`
	MegaTimeout    time.Duration `mapstructure:"mega_timeout"`
}

type EngineConfig struct {
	core.SearchEngineOptions `mapstructure:",squash"`
	Proxy                    string `mapstructure:"proxy"`
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

type CaptchaConfig struct {
	SolverEnabled bool `mapstructure:"solver_enabled"`
}

var config = Config{}

var flagToConfigKey = map[string]string{
	"host":                    "server.host",
	"port":                    "server.port",
	"timeout":                 "app.timeout",
	"config":                  "server.config_path",
	"browser-path":            "app.browser_path",
	"profiles-json":           "app.profiles",
	"verbose":                 "server.verbose",
	"debug":                   "server.debug",
	"head":                    "app.head",
	"leakless":                "app.leakless",
	"raw":                     "server.raw_requests",
	"leave":                   "app.leave_head",
	"2captcha_key":            "2captcha.apikey",
	"proxy":                   "proxies.global",
	"debug-endpoints":         "app.debug_endpoints",
	"insecure":                "server.insecure",
	"cache_ttl":               "cache.ttl_seconds",
	"cache_max_size":          "cache.max_size",
	"max_retries":             "resilience.max_retries",
	"allow_endpoint_fallback": "resilience.allow_endpoint_fallback",
	"cb_failures":             "circuit_breaker.failures",
	"cb_recovery":             "circuit_breaker.recovery_seconds",
	"cb_successes":            "circuit_breaker.successes",
	"log_format":              "app.log_format",
}

var RootCmd = &cobra.Command{
	Use:          "openserp",
	Short:        "Open SERP",
	Long:         `Get [Google, Yandex, Baidu] search engine results via API or CLI.`,
	Version:      version,
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		err := initializeConfig(cmd)
		if err != nil {
			return err
		}
		if err := browserprofile.LoadProfilesFromJSON(config.App.ProfilesJSON); err != nil {
			return fmt.Errorf("load app.profiles: %w", err)
		}

		logFormat, err := core.NormalizeLogFormat(config.App.LogFormat)
		if err != nil {
			return err
		}
		config.App.LogFormat = logFormat

		core.InitLogger(config.Server.IsVerbose, config.Server.IsDebug, config.App.LogFormat)
		logrus.WithField("config", sanitizedConfigForLog(config)).Debug("Final config")
		return nil
	},
}

func sanitizedConfigForLog(cfg Config) map[string]interface{} {
	return map[string]interface{}{
		"server": cfg.Server,
		"app": map[string]interface{}{
			"timeout":         cfg.App.Timeout,
			"browser_path":    cfg.App.BrowserPath != "",
			"profiles":        cfg.App.ProfilesJSON != "",
			"head":            cfg.App.IsBrowserHead,
			"leave_head":      cfg.App.IsLeaveHead,
			"leakless":        cfg.App.IsLeakless,
			"block_resources": cfg.App.BlockResources,
			"block_trackers":  cfg.App.BlockTrackers,
			"debug_endpoints": cfg.App.DebugEndpoints,
			"log_format":      cfg.App.LogFormat,
			"max_processes":   cfg.App.MaxProcesses,
			"idle_ttl":        cfg.App.IdleTTL.String(),
			"mega_timeout":    cfg.App.MegaTimeout.String(),
		},
		"proxies": map[string]interface{}{
			"global":                  maskedProxyForLog(cfg.Proxies.Global),
			"entries":                 len(cfg.Proxies.Entries),
			"allow_request_proxy_url": cfg.Proxies.AllowRequestProxyURL,
			"health":                  cfg.Proxies.Health,
			"lanes":                   cfg.Proxies.Lanes,
		},
		"cache":           cfg.Cache,
		"resilience":      cfg.Resilience,
		"circuit_breaker": cfg.CircuitBreaker,
		"cors":            cfg.CORS,
		"captcha":         cfg.Captcha,
		"2captcha": map[string]interface{}{
			"apikey_configured": strings.TrimSpace(cfg.Config2Capcha.ApiKey) != "",
		},
		"google":     cfg.GoogleConfig,
		"yandex":     cfg.YandexConfig,
		"baidu":      cfg.BaiduConfig,
		"bing":       cfg.BingConfig,
		"duckduckgo": cfg.DuckDuckGoConfig,
	}
}

func maskedProxyForLog(proxyURL string) string {
	if strings.TrimSpace(proxyURL) == "" {
		return ""
	}
	return core.MaskProxyURL(proxyURL)
}

// Bind each cobra flag to its associated viper configuration (config file and environment variable)
func bindFlags(cmd *cobra.Command, vpr *viper.Viper) {
	cmd.Flags().VisitAll(func(flg *pflag.Flag) {
		configName, ok := flagToConfigKey[flg.Name]
		if !ok {
			configName = "app." + flg.Name
		}

		if err := vpr.BindPFlag(configName, flg); err != nil {
			logrus.WithError(err).Error(fmt.Sprintf("Unable to bind flag %s: %v", flg.Name, err))
		}

		if flg.Changed {
			val, err := parseFlagValue(flg)
			if err != nil {
				logrus.WithError(err).Error(fmt.Sprintf("Unable to parse flag %s: %v", flg.Name, err))
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

	explicitConfigPath := strings.TrimSpace(cmd.Flag("config").Value.String())
	if explicitConfigPath == "" {
		explicitConfigPath = strings.TrimSpace(os.Getenv(envPrefix + "_SERVER_CONFIG_PATH"))
	}

	if explicitConfigPath != "" {
		v.SetConfigFile(explicitConfigPath)
	} else {
		// Base name of the config file, without the file extension
		v.SetConfigName(defaultConfigFilename)
		v.AddConfigPath(".")
	}

	// 1. Config file (lowest priority). Return an error if we cannot parse the config file.
	err := v.ReadInConfig()
	if err != nil {
		if explicitConfigPath != "" {
			return fmt.Errorf("cannot read config %q: %w", explicitConfigPath, err)
		}
		err = fmt.Errorf("cannot read config: %v", err)
		logrus.Warn(err)
	}

	// 2. Environment variables (medium priority). Bind environment variables to their equivalent keys with underscores
	for _, key := range v.AllKeys() {
		envKey := envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		err := v.BindEnv(key, envKey)
		if err != nil {
			logrus.WithError(err).Error(fmt.Sprintf("Unable to bind ENV valye: %v", err))
		}
	}

	// 3. Command flags (highest priority). Bind the current command's flags to viper
	bindFlags(cmd, v)

	// Keep compatibility with historical typo in local configs. Runs after all
	// sources are merged so CLI flags and env vars take precedence over the typo key.
	if v.IsSet("app.block_resorces") && !v.IsSet("app.block_resources") {
		v.Set("app.block_resources", v.Get("app.block_resorces"))
		logrus.Warn(`config key "app.block_resorces" is deprecated, use "app.block_resources"`)
	}

	if err := validateRemovedConfigPaths(v); err != nil {
		return err
	}

	// Dump Viper values to config struct
	if err := validateEngineProxyTags(v); err != nil {
		return err
	}

	err = v.Unmarshal(&config)
	if err != nil {
		return fmt.Errorf("cannot unmarshall config: %v", err)
	}

	if _, err := core.ParseBlockedResourceTypes(config.App.BlockResources); err != nil {
		return fmt.Errorf("invalid app.block_resources: %w", err)
	}

	config.Proxies, err = core.NormalizeProxiesConfig(config.Proxies)
	if err != nil {
		return fmt.Errorf("invalid proxies config: %w", err)
	}

	return nil
}

func validateEngineProxyTags(v *viper.Viper) error {
	for _, engineName := range []string{"google", "yandex", "baidu", "bing", "duckduckgo"} {
		key := engineName + ".proxy"
		if !v.IsSet(key) {
			continue
		}

		raw := v.Get(key)
		tag, ok := raw.(string)
		if !ok {
			return fmt.Errorf("invalid %s.proxy config: proxy must be a string tag", engineName)
		}

		if _, err := core.NormalizeProxyTag(tag); err != nil {
			return fmt.Errorf("invalid %s.proxy config: %w", engineName, err)
		}
	}

	return nil
}

func validateRemovedConfigPaths(v *viper.Viper) error {
	legacyKeys := map[string]string{
		"app.proxy":                    "use proxies.global or proxies.entries with per-engine proxy tags instead",
		"proxy_pool":                   "use proxies.entries and proxies.health.failure_threshold instead",
		"proxy_pool.urls":              "use proxies.entries instead",
		"proxy_pool.failure_threshold": "use proxies.health.failure_threshold instead",
		"app.host":                     "move to server.host",
		"app.port":                     "move to server.port",
		"app.debug":                    "move to server.debug",
		"app.verbose":                  "move to server.verbose",
		"app.raw_requests":             "move to server.raw_requests",
		"app.insecure":                 "move to server.insecure",
		"proxies.defaults":             "use proxies.global or per-engine proxy tags instead",
		"proxies.defaults.mode":        "use proxies.global or per-engine proxy tags instead",
		"proxies.defaults.tag":         "use per-engine proxy tags on each engine instead",
		"google.proxy.mode":            "use google.proxy: <tag> or omit it for direct mode",
		"google.proxy.tag":             "use google.proxy: <tag>",
		"yandex.proxy.mode":            "use yandex.proxy: <tag> or omit it for direct mode",
		"yandex.proxy.tag":             "use yandex.proxy: <tag>",
		"baidu.proxy.mode":             "use baidu.proxy: <tag> or omit it for direct mode",
		"baidu.proxy.tag":              "use baidu.proxy: <tag>",
		"bing.proxy.mode":              "use bing.proxy: <tag> or omit it for direct mode",
		"bing.proxy.tag":               "use bing.proxy: <tag>",
		"duckduckgo.proxy.mode":        "use duckduckgo.proxy: <tag> or omit it for direct mode",
		"duckduckgo.proxy.tag":         "use duckduckgo.proxy: <tag>",
	}

	for key, hint := range legacyKeys {
		if v.IsSet(key) {
			return fmt.Errorf("config key %q is removed in proxy v2: %s", key, hint)
		}
	}
	return nil
}

func setConfigDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", 7070)
	v.SetDefault("server.debug", false)
	v.SetDefault("server.verbose", false)
	v.SetDefault("server.raw_requests", false)
	v.SetDefault("server.insecure", false)
	v.SetDefault("app.log_format", "")

	v.SetDefault("app.timeout", 30)
	v.SetDefault("app.browser_path", "")
	v.SetDefault("app.profiles", "")
	v.SetDefault("app.head", false)
	v.SetDefault("app.leave_head", false)
	v.SetDefault("app.leakless", false)
	v.SetDefault("app.block_resources", "")
	v.SetDefault("app.block_trackers", false)
	v.SetDefault("app.debug_endpoints", false)
	v.SetDefault("app.max_processes", 4)
	v.SetDefault("app.idle_ttl", "10m")
	v.SetDefault("app.mega_timeout", "90s")

	v.SetDefault("proxies.entries", []interface{}{})
	v.SetDefault("proxies.global", "")
	v.SetDefault("proxies.allow_request_proxy_url", false)
	v.SetDefault("proxies.health.failure_threshold", core.DefaultProxyFailureThreshold)
	v.SetDefault("proxies.lanes.enabled", true)
	v.SetDefault("proxies.lanes.max_lanes", core.DefaultProxyLaneMaxLanes)
	v.SetDefault("proxies.lanes.drop_cookies_on_challenge", true)

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
	v.SetDefault("cors.allow_headers", "Origin, Content-Type, Accept, Authorization, X-Use-Proxy, X-Proxy-URL, X-Proxy-Country, X-Proxy-Class, X-Proxy-Provider, X-Proxy-Session-ID, X-Request-ID, X-Tenant")
	v.SetDefault("cors.max_age", 86400)
	v.SetDefault("captcha.solver_enabled", false)
}

func init() {
	RootCmd.PersistentFlags().IntVarP(&config.Server.Port, "port", "p", 7070, "Port number to run server")
	RootCmd.PersistentFlags().StringVarP(&config.Server.Host, "host", "a", "127.0.0.1", "Host address to run server")
	RootCmd.PersistentFlags().IntVarP(&config.App.Timeout, "timeout", "t", 30, "Timeout to fail request")
	RootCmd.PersistentFlags().StringVarP(&config.Server.ConfigPath, "config", "c", "", "Configuration file path")
	RootCmd.PersistentFlags().StringVarP(&config.App.BrowserPath, "browser-path", "", "", "Custom browser binary path (Chrome/Chromium/Edge/Brave..)")
	RootCmd.PersistentFlags().StringVar(&config.App.ProfilesJSON, "profiles", "", "Path to browser profile catalog JSON")
	RootCmd.PersistentFlags().BoolVarP(&config.Server.IsVerbose, "verbose", "v", false, "Use verbose output")
	RootCmd.PersistentFlags().BoolVarP(&config.Server.IsDebug, "debug", "d", false, "Use debug output. Disable headless browser")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsBrowserHead, "head", "", false, "Enable browser UI")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsLeakless, "leakless", "l", false, "Use leakless mode to insure browser instances are closed after search")
	RootCmd.PersistentFlags().BoolVarP(&config.Server.IsRawRequests, "raw", "r", false, "Disable browser usage, use HTTP requests")
	RootCmd.PersistentFlags().BoolVarP(&config.App.IsLeaveHead, "leave", "", false, "Leave browser and tabs opened after search is made")
	RootCmd.PersistentFlags().StringVarP(&config.Config2Capcha.ApiKey, "2captcha_key", "", "", "2 captcha api key")
	RootCmd.PersistentFlags().StringVarP(&config.Proxies.Global, "proxy", "x", "", "Force a single proxy for all engines (same as proxies.global)")
	RootCmd.PersistentFlags().BoolVar(&config.App.DebugEndpoints, "debug-endpoints", false, "Enable debug-only HTTP endpoints")
	RootCmd.PersistentFlags().BoolVarP(&config.Server.Insecure, "insecure", "k", false, "Allow insecure TLS connections")
	RootCmd.PersistentFlags().IntVar(&config.Cache.TTLSeconds, "cache_ttl", 300, "Cache TTL in seconds (0 to disable)")
	RootCmd.PersistentFlags().IntVar(&config.Cache.MaxSize, "cache_max_size", 1000, "Maximum number of cached responses")
	RootCmd.PersistentFlags().IntVar(&config.Resilience.MaxRetries, "max_retries", 3, "Max retry attempts per search engine (0 to disable)")
	RootCmd.PersistentFlags().BoolVar(&config.Resilience.AllowEndpointFallback, "allow_endpoint_fallback", false, "Allow dedicated endpoints to fallback to other engines")
	RootCmd.PersistentFlags().IntVar(&config.CircuitBreaker.Failures, "cb_failures", 5, "Consecutive failures before circuit breaker opens")
	RootCmd.PersistentFlags().IntVar(&config.CircuitBreaker.RecoverySeconds, "cb_recovery", 60, "Seconds before retrying an engine with open circuit")
	RootCmd.PersistentFlags().IntVar(&config.CircuitBreaker.Successes, "cb_successes", 2, "Consecutive successful half-open checks needed to close circuit")
	RootCmd.PersistentFlags().StringVar(&config.App.LogFormat, "log_format", "", "Log format: json or text (default: json in production, text in debug)")
}

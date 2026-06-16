package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/karust/openserp/core"
	extractpkg "github.com/karust/openserp/extract"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// searchFlags holds the per-invocation CLI flags for the search command.
type searchFlags struct {
	limit    int
	lang     string
	region   string
	start    int
	site     string
	filetype string
	format   string
	full     bool
	features bool
	extract  int
	timeout  int
}

var searchOpts searchFlags

var searchCMD = &cobra.Command{
	Use:     "search [engine] [query]",
	Aliases: []string{"find"},
	Short:   "Search results using chosen web search engine (google, yandex, baidu, bing, duckduckgo, ecosia)",
	// Validate the engine ourselves; cobra.OnlyValidArgs would also reject the
	// query arg. ValidArgs still feeds shell completion.
	Args:      cobra.MatchAll(cobra.ExactArgs(2), validateEngineArg),
	ValidArgs: engineValidArgs(),
	RunE:      search,
}

// validateEngineArg checks args[0] against the registry with a clear error,
// without rejecting the query arg.
func validateEngineArg(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if _, ok := resolveEngineSpec(normalizeEngineArg(args[0])); !ok {
		return fmt.Errorf("unknown engine %q; valid: %s", args[0], strings.Join(engineValidArgs(), ", "))
	}
	return nil
}

func search(cmd *cobra.Command, args []string) error {
	// Already validated by validateEngineArg, so this can't miss.
	engineType := normalizeEngineArg(args[0])
	spec, _ := resolveEngineSpec(engineType)

	format, err := normalizeSearchFormat(searchOpts.format)
	if err != nil {
		return err
	}

	limit := searchOpts.limit
	if limit <= 0 {
		limit = 10
	}
	query := core.Query{
		Text:     args[1],
		LangCode: searchOpts.lang,
		Region:   searchOpts.region,
		Site:     searchOpts.site,
		Filetype: searchOpts.filetype,
		Limit:    limit,
		Start:    searchOpts.start,
		Filter:   true,
		Features: searchOpts.features,
		Insecure: config.Server.Insecure,
	}
	if err := applyCLIExtractFlag(&query, searchOpts.extract); err != nil {
		return err
	}

	captchaSolverEnabled, captchaSolverAPIKey, err := resolveCaptchaSolverConfig()
	if err != nil {
		return fmt.Errorf("validate captcha solver config: %w", err)
	}

	proxyRuntime := core.ProxyRuntimeBrowser
	if config.Server.IsRawRequests {
		proxyRuntime = core.ProxyRuntimeRaw
	}

	proxyCfg, err := buildNormalizedProxyConfig(proxyRuntime)
	if err != nil {
		return fmt.Errorf("validate proxy config: %w", err)
	}

	policy := resolveEngineProxyPolicy(proxyCfg, engineType)

	selectedProxy, err := selectCLIProxy(proxyCfg, policy)
	if err != nil {
		return fmt.Errorf("select proxy for %s: %w", engineType, err)
	}

	if config.Server.IsRawRequests {
		query.ProxyURL = selectedProxy
	}

	// Bound the whole search so a wedged Chrome can't hang the CLI forever.
	timeoutSec := searchOpts.timeout
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	logrus.WithFields(logrus.Fields{
		"engine":     engineType,
		"query_hash": core.QueryHashFromQuery(query),
	}).Info(fmt.Sprintf("Starting SERP search request using %s engine for query: %s", engineType, query.Text))

	startedAt := time.Now()
	var results []core.SearchResult
	if config.Server.IsRawRequests {
		logrus.WithField("engine", engineType).Info(fmt.Sprintf("Using raw requests mode for %s search", engineType))
		results, err = searchRaw(ctx, spec, query)
	} else {
		logrus.WithField("engine", engineType).Info(fmt.Sprintf("Using browser mode for %s search", engineType))
		results, err = searchBrowser(ctx, spec, query, selectedProxy, captchaSolverEnabled, captchaSolverAPIKey)
	}

	if err != nil {
		return fmt.Errorf("%s search: %w", engineType, err)
	}

	logrus.WithFields(logrus.Fields{
		"engine":        engineType,
		"results_count": len(results),
	}).Info(fmt.Sprintf("Successfully completed SERP search using %s engine, returned %d results", engineType, len(results)))

	env := buildCLIEnvelope(spec.name, query, results, startedAt)
	if query.Extract {
		if err := enrichCLIEnvelopeWithExtraction(ctx, env, query, format, selectedProxy, captchaSolverEnabled, captchaSolverAPIKey); err != nil {
			return fmt.Errorf("extract search results: %w", err)
		}
	}
	payload := renderCLIEnvelope(env, format, searchOpts.full)
	fmt.Println(strings.TrimRight(string(payload), "\n"))
	return nil
}

func buildCLIEnvelope(engineName string, query core.Query, results []core.SearchResult, startedAt time.Time) *core.Envelope {
	env := core.NewEnvelope(query, uuid.NewString(), startedAt, []string{engineName})
	ectx := core.EnrichContext{Engine: engineName, Query: query}
	for _, r := range results {
		core.AppendEnrichedSearchResult(env, r, ectx, startedAt)
	}
	env.Finalize(startedAt, query)
	return env
}

// renderCLIEnvelope renders a v2.1 envelope. JSON/ndjson always carry the full
// envelope; text/markdown omit serp_features unless --full.
func renderCLIEnvelope(env *core.Envelope, format string, full bool) []byte {
	if !full && format != "json" && format != "ndjson" {
		env.SerpFeatures = nil
	}

	switch format {
	case "text":
		return core.RenderText(env)
	case "markdown":
		return core.RenderMarkdown(env)
	case "ndjson":
		return core.RenderNDJSON(env)
	default: // json
		b, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			logrus.WithError(err).Error("marshal envelope")
			return nil
		}
		return b
	}
}

const maxCLIExtractTop = 5

func applyCLIExtractFlag(query *core.Query, extractTop int) error {
	top, err := normalizeCLIExtractTop(extractTop)
	if err != nil {
		return err
	}
	if top == 0 {
		return nil
	}
	if !config.Extract.Enabled {
		return fmt.Errorf("extraction is disabled in config")
	}
	query.Extract = true
	query.ExtractTop = top
	query.ExtractMode = string(extractpkg.ModeAuto)
	return nil
}

func normalizeCLIExtractTop(raw int) (int, error) {
	if raw < 0 {
		return 0, fmt.Errorf("--extract must be a non-negative integer")
	}
	if raw > maxCLIExtractTop {
		return maxCLIExtractTop, nil
	}
	return raw, nil
}

func enrichCLIEnvelopeWithExtraction(ctx context.Context, env *core.Envelope, query core.Query, format string, proxyURL string, captchaSolverEnabled bool, captchaSolverAPIKey string) error {
	if env == nil || !query.Extract {
		return nil
	}
	query.ProxyURL = proxyURL
	extractor, closeExtractor, err := newCLIExtractor(captchaSolverEnabled, captchaSolverAPIKey)
	if err != nil {
		return err
	}
	defer closeExtractor()

	// Same depth bounds, batch deadline, and candidate fill-in as the HTTP server.
	core.EnrichEnvelopeWithExtraction(ctx, env, query, format, extractor, config.Extract)
	return nil
}

// newCLIExtractor builds an Extractor backed by a lazily-created, single-use
// browser. The raw path delegates to core.RawExtractFetch; the rendered path
// validates the target, gates auth'd SOCKS, then reuses core.RenderExtractHTML.
func newCLIExtractor(captchaSolverEnabled bool, captchaSolverAPIKey string) (extractpkg.Extractor, func(), error) {
	cfg := config.Extract.Normalized()
	var browserMu sync.Mutex
	var browser *core.Browser

	closeExtractor := func() {
		browserMu.Lock()
		defer browserMu.Unlock()
		if browser == nil {
			return
		}
		if err := browser.Close(); err != nil {
			logrus.WithError(err).Debug("Extraction browser close error")
		}
		browser = nil
	}

	extractor := extractpkg.Extractor{
		Cfg: cfg,
		RawFetch: func(ctx context.Context, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
			return core.RawExtractFetch(ctx, req, cfg, config.Server.Insecure)
		},
		RenderedFetch: func(ctx context.Context, req extractpkg.ExtractRequest) (*extractpkg.FetchResponse, error) {
			if err := validateCLIExtractTargetURL(ctx, req.URL, cfg.AllowPrivateNetworks); err != nil {
				return nil, err
			}
			if core.IsAuthenticatedSocksProxyURL(req.ProxyURL) {
				return nil, fmt.Errorf(
					"%w: browser runtime does not support authenticated SOCKS proxy %s",
					core.ErrProxyUnavailable,
					core.MaskProxyURL(req.ProxyURL),
				)
			}

			browserMu.Lock()
			if browser == nil {
				created, err := newCLIExtractBrowser(cfg, req.ProxyURL, captchaSolverEnabled, captchaSolverAPIKey)
				if err != nil {
					browserMu.Unlock()
					return nil, err
				}
				browser = created
			}
			current := browser
			browserMu.Unlock()

			return core.RenderExtractHTML(ctx, current, req)
		},
	}
	return extractor, closeExtractor, nil
}

func newCLIExtractBrowser(cfg extractpkg.Config, proxyURL string, captchaSolverEnabled bool, captchaSolverAPIKey string) (*core.Browser, error) {
	blockedResourceTypes, err := core.ParseBlockedResourceTypes(config.App.BlockResources)
	if err != nil {
		return nil, fmt.Errorf("invalid block_resources config: %w", err)
	}
	opts := core.BrowserOpts{
		IsHeadless:           !config.App.IsBrowserHead && !config.Server.IsDebug,
		IsLeakless:           config.App.IsLeakless,
		Timeout:              cfg.Timeout,
		LeavePageOpen:        false,
		CaptchaSolverEnabled: captchaSolverEnabled,
		CaptchaSolverApiKey:  captchaSolverAPIKey,
		BrowserPath:          config.App.BrowserPath,
		ProxyURL:             proxyURL,
		Insecure:             config.Server.Insecure,
		BlockResourceTypes:   blockedResourceTypes,
		BlockTrackers:        config.App.BlockTrackers,
	}
	return core.NewBrowser(opts)
}

func validateCLIExtractTargetURL(ctx context.Context, rawURL string, allowPrivateNetworks bool) error {
	targetURL := extractpkg.NormalizeURL(strings.TrimSpace(rawURL))
	if allowPrivateNetworks {
		parsed, err := url.ParseRequestURI(targetURL)
		if err != nil {
			return fmt.Errorf("invalid url: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("url must use http or https")
		}
		return nil
	}
	return core.ValidatePublicHTTPURL(ctx, targetURL)
}

func normalizeSearchFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "json":
		return "json", nil
	case "text", "txt":
		return "text", nil
	case "markdown", "md":
		return "markdown", nil
	case "ndjson", "jsonl":
		return "ndjson", nil
	default:
		return "", fmt.Errorf("invalid --format %q; valid: json, text, markdown, ndjson", raw)
	}
}

func searchBrowser(ctx context.Context, spec engineSpec, query core.Query, browserProxyURL string, captchaSolverEnabled bool, captchaSolverAPIKey string) ([]core.SearchResult, error) {
	blockedResourceTypes, err := core.ParseBlockedResourceTypes(config.App.BlockResources)
	if err != nil {
		return nil, fmt.Errorf("invalid block_resources config: %w", err)
	}
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
	// Close the browser so Chromium never outlives the CLI run.
	defer func() {
		if closeErr := browser.Close(); closeErr != nil {
			logrus.WithError(closeErr).Debug("Browser close error")
		}
	}()

	engine := spec.factory(*browser, spec.opts())
	return engine.Search(ctx, query)
}

func searchRaw(ctx context.Context, spec engineSpec, query core.Query) ([]core.SearchResult, error) {
	logrus.Warn("Browserless results are very inconsistent or may not even work!")
	if spec.rawSearchFn == nil {
		logrus.Warnf("%s does not support raw HTTP requests mode. Please use browser mode instead.", spec.name)
		return nil, fmt.Errorf("%s does not support raw requests mode", spec.name)
	}
	return spec.rawSearchFn(ctx, query)
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
	return strings.ToLower(strings.TrimSpace(raw))
}

func init() {
	searchCMD.Flags().IntVar(&searchOpts.limit, "limit", 10, "Maximum number of results")
	searchCMD.Flags().StringVar(&searchOpts.lang, "lang", "", "Language hint (e.g. EN, DE, RU)")
	searchCMD.Flags().StringVar(&searchOpts.region, "region", "", "Region/market hint (e.g. RU, en-US)")
	searchCMD.Flags().IntVar(&searchOpts.start, "start", 0, "Pagination start offset")
	searchCMD.Flags().StringVar(&searchOpts.site, "site", "", "Restrict results to a domain (e.g. github.com)")
	searchCMD.Flags().StringVar(&searchOpts.filetype, "file", "", "File type filter (e.g. pdf)")
	searchCMD.Flags().StringVar(&searchOpts.format, "format", "json", "Output format: json, text, markdown, ndjson")
	searchCMD.Flags().BoolVar(&searchOpts.full, "full", false, "Include SERP features in text/markdown output")
	searchCMD.Flags().BoolVar(&searchOpts.features, "features", false, "Parse SERP feature modules (browser mode)")
	searchCMD.Flags().IntVar(&searchOpts.extract, "extract", 0, "Extract clean content from the top N results using auto mode (1-5)")
	searchCMD.Flags().IntVar(&searchOpts.timeout, "search-timeout", 60, "Overall search timeout in seconds")
	RootCmd.AddCommand(searchCMD)
}

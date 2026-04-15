# OpenSERP Architecture

## 1. Overview

OpenSERP is a Go API + CLI for search results extraction from Google, Yandex, Baidu, Bing, and DuckDuckGo.

It supports two execution modes:

- Browser mode (default): headless Chromium via `go-rod`, with engine-specific DOM parsing.
- Raw HTTP mode: direct requests + HTML parsing (`goquery`) for engines that implement raw parsing.

Browser mode is the primary path and supports all engines. Raw mode currently supports Google, Yandex, and Baidu only.

## 2. Directory Structure

```text
openserp/
├── main.go                        # Entry point, executes cmd.RootCmd
├── AGENTS.md                      # Contributor + agent project guidance
├── README.md                      # User-facing quickstart and API overview
├── config.yaml                    # Runtime configuration (loaded by Viper)
├── docs/
│   ├── ARCHITECTURE.md            # This architecture reference
│   ├── openapi.yaml               # OpenAPI 3.0 specification
│   └── embed.go                   # Embeds openapi.yaml for /openapi.yaml endpoint
├── cmd/
│   ├── root.go                    # Cobra root command + Viper config binding/defaults
│   ├── serve.go                   # HTTP server bootstrap, engine wiring, browser pooling
│   ├── search.go                  # CLI one-shot search command
│   └── proxy_policy.go            # Proxy policy mapping from config to runtime
├── core/
│   ├── common.go                  # Shared domain types: Query, SearchResult, SearchEngine
│   ├── server.go                  # Fiber routes, request handlers, cache/proxy headers
│   ├── middleware.go              # CORS, request logging, JSON error envelope
│   ├── browser.go                 # Chromium navigation lifecycle and page orchestration
│   ├── http_client.go             # Raw HTTP client (uTLS fingerprinting)
│   ├── resilient.go               # Retry + CB + rate limiting + proxy orchestration
│   ├── retry.go                   # Backoff retry runner and retry conditions
│   ├── circuit_breaker.go         # Per-engine circuit breaker state machine
│   ├── cache.go                   # In-memory TTL cache for API responses
│   ├── proxy.go                   # Proxy normalization, pools, health/rotation, stats
│   ├── logger.go                  # Logging setup helpers
│   └── captcha.go                 # Captcha-related helpers/errors
├── google/                        # Google engine implementation
│   ├── url.go                     # URL builders
│   ├── search.go                  # Browser mode parser
│   └── search_raw.go              # Raw HTTP parser
├── yandex/                        # Yandex engine implementation
│   ├── url.go
│   ├── search.go
│   └── search_raw.go
├── baidu/                         # Baidu engine implementation
│   ├── url.go
│   ├── search.go
│   └── search_raw.go
├── bing/                          # Bing engine implementation (browser-only)
│   ├── url.go
│   └── search.go
├── duckduckgo/                    # DuckDuckGo engine implementation (browser-only)
│   ├── url.go
│   └── search.go
├── testutil/                      # Integration gating and shared test fixtures/helpers
└── .github/workflows/ci.yml       # CI checks (test/vet/build/lint/openapi lint)
```

## 3. Key Interfaces and Types

### `core.SearchEngine`

Contract for all engines:

- `Search(Query) ([]SearchResult, error)` for web results
- `SearchImage(Query) ([]SearchResult, error)` for image results
- `IsInitialized() bool` for health readiness
- `Name() string` for endpoint and stats identity
- `GetRateLimiter() *rate.Limiter` for per-engine throttling

### `core.Query`

Parsed from query parameters and request headers:

- `Text` (`text`)
- `LangCode` (`lang`)
- `DateInterval` (`date`, format `YYYYMMDD..YYYYMMDD`)
- `Filetype` (`file`)
- `Site` (`site`)
- `Limit` (`limit`, default `25`)
- `Start` (`start`, default `0`)
- `Filter` (`filter`, default `true`)
- `Answers` (`answers`, default `false`)
- `ProxyOverride` (`X-Use-Proxy` header: `<tag>` or `direct`)
- Internal runtime fields: `ProxyURL`, `Insecure`

Validation summary:

- `start` must be `>= 0`
- At least one of `text`, `site`, or `file` must be non-empty
- Invalid query parsing is returned as JSON error response

### `core.SearchResult`

Single SERP item shape:

- `rank` (int)
- `url` (string)
- `title` (string)
- `description` (string)
- `ad` (bool)

Mega endpoints return `core.MegaSearchResult`, which extends `SearchResult` with:

- `engine` (string)

## 4. Request Flow

```text
HTTP request
  -> Fiber router
  -> handleDedicatedEndpoint / handleMegaEndpoint
  -> Query.InitFromContext
  -> ResilientSearcher.SearchPrimary/SearchWithFallback (or mega parallel search)
     -> CircuitBreaker.AllowRequest
     -> RateLimiter.Wait
     -> Proxy policy resolution and proxy selection
     -> RetryableSearch (backoff/retry loop)
     -> Engine.Search / Engine.SearchImage
        Browser path: Browser.Navigate(url) -> DOM parse -> []SearchResult
        Raw path:     raw HTTP request -> goquery parse -> []SearchResult
  -> De-duplication (mega endpoints)
  -> Cache.Set (if enabled and cacheable)
  -> JSON response + X-Cache/X-Proxy-*/X-Fallback-Engine headers
```

## 5. Browser vs Raw Mode

### Browser Mode (default)

- Enabled when `server.raw_requests: false`
- Uses Chromium + `go-rod` navigation and page parsing
- Supported engines: Google, Yandex, Baidu, Bing, DuckDuckGo
- Best compatibility, but heavier resource usage

### Raw HTTP Mode

- Enabled when `server.raw_requests: true`
- Uses direct HTTP + HTML parsing without launching a browser
- Supported engines: Google, Yandex, Baidu
- Faster/lighter, but less reliable for anti-bot protected pages and missing image support

Mode switch options:

- Config: `server.raw_requests`
- CLI flag: `--raw`

## 6. Resilience Stack

The effective request protection sequence is:

1. Rate limiter (`engine.GetRateLimiter().Wait`)
2. Retry with exponential backoff (`core/retry.go`)
3. Circuit breaker per engine (`core/circuit_breaker.go`)
4. Proxy selection/rotation + health tracking (`core/proxy.go`)
5. Response cache (API-level TTL cache in `core/cache.go`)

Important behaviors:

- `ErrCaptcha` is non-retryable.
- `ErrProxyUnavailable` does not record circuit-breaker failure.
- Dedicated endpoints are engine-pure by default (`allow_endpoint_fallback: false`).
- Fallback responses are not cached on dedicated endpoints.

## 7. Config Reference

Defaults below are the shipped defaults in `config.yaml` (if present). If the config file is missing, fallback defaults from `cmd/root.go` are applied.

### `server`

| Key | Default | Description |
| --- | --- | --- |
| `server.host` | `0.0.0.0` | API bind host |
| `server.port` | `7000` | API bind port |
| `server.debug` | `false` | Debug mode, forces headful browser |
| `server.verbose` | `true` | Info-level request logs |
| `server.raw_requests` | `false` | `true` = raw HTTP mode |
| `server.insecure` | `true` | Allow insecure TLS connections |

### `app`

| Key | Default | Description |
| --- | --- | --- |
| `app.timeout` | `15` | Request timeout in seconds |
| `app.browser_path` | `""` | Custom browser binary path |
| `app.head` | `false` | Headful browser UI |
| `app.leakless` | `false` | Force browser process cleanup |
| `app.leave_head` | `false` | Keep browser tabs open |
| `app.stealth` | `false` | Enable stealth plugin |

### `proxies`

| Key | Default | Description |
| --- | --- | --- |
| `proxies.global` | unset | Force single proxy for all engines |
| `proxies.entries[]` | empty | Tagged proxy pool entries (`url`, `tags`) |
| `proxies.health.failure_threshold` | `3` | Disable proxy after N failures |

Per-engine optional proxy tag:

- `google.proxy`
- `yandex.proxy`
- `baidu.proxy`
- `bing.proxy`
- `duckduckgo.proxy`

### `cache`

| Key | Default | Description |
| --- | --- | --- |
| `cache.ttl_seconds` | `60` | Response cache TTL (0 disables cache) |
| `cache.max_size` | `1000` | Max cached entries |

### `resilience`

| Key | Default | Description |
| --- | --- | --- |
| `resilience.max_retries` | `2` | Retry attempts per request |
| `resilience.allow_endpoint_fallback` | `false` | Allow dedicated endpoints to fallback to other engines |

### `circuit_breaker`

| Key | Default | Description |
| --- | --- | --- |
| `circuit_breaker.failures` | `5` | Failures before opening circuit |
| `circuit_breaker.recovery_seconds` | `60` | Open -> half-open wait time |
| `circuit_breaker.successes` | `2` | Half-open successes to close circuit |

### `cors`

| Key | Default | Description |
| --- | --- | --- |
| `cors.enabled` | `true` | Enable CORS middleware |
| `cors.allow_origins` | `"*"` | Allowed origins |
| `cors.allow_methods` | `"GET, POST, OPTIONS"` | Allowed methods |
| `cors.allow_headers` | `"Origin, Content-Type, Accept, Authorization, X-Use-Proxy"` | Allowed headers |
| `cors.max_age` | `86400` | Preflight cache max age (seconds) |

### `2captcha`

| Key | Default | Description |
| --- | --- | --- |
| `2captcha.apikey` | unset | Optional captcha solver key |

### Engine rate-limit defaults

For each engine (`google`, `yandex`, `baidu`, `bing`, `duckduckgo`):

| Key | Default | Description |
| --- | --- | --- |
| `<engine>.rate_requests` | `4` | Average requests per minute |
| `<engine>.rate_burst` | `2` | Burst capacity |
| `<engine>.rate_seconds` | `60` (implicit) | Rate window seconds |
| `<engine>.selector_timeout` | `5` (implicit) | Selector wait timeout seconds |

Google-only additional toggle:

- `google.captcha` (default: `true`)

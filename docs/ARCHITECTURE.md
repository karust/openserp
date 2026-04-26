# OpenSERP Architecture

## Overview

OpenSERP is a Go API + CLI for search result extraction from Google, Yandex, Baidu, Bing, and DuckDuckGo.

Execution modes:

- **Browser mode**: default path, headless Chromium via `go-rod`, supported by all engines.
- **Raw HTTP mode**: direct HTTP + `goquery`, currently supported by Google, Yandex, and Baidu.

Browser mode is the primary compatibility path.

## Project Layout

```text
openserp/
├── main.go
├── README.md
├── config.yaml
├── docs/
│   ├── ARCHITECTURE.md
│   ├── CONTRIBUTING.md
│   ├── openapi.yaml
│   └── embed.go
├── cmd/
│   ├── root.go
│   ├── serve.go
│   ├── search.go
│   └── proxy_policy.go
├── core/
│   ├── common.go
│   ├── server.go
│   ├── response.go
│   ├── result.go
│   ├── response_builder.go
│   ├── clusters.go
│   ├── format_markdown.go
│   ├── format_text.go
│   ├── enrichment_domain.go
│   ├── enrichment_domains.yaml
│   ├── middleware.go
│   ├── browser.go
│   ├── http_client.go
│   ├── resilient.go
│   ├── retry.go
│   ├── circuit_breaker.go
│   ├── cache.go
│   ├── proxy.go
│   ├── logger.go
│   └── captcha.go
├── google/
├── yandex/
├── baidu/
├── bing/
├── duckduckgo/
└── testutil/
```

## Core Interfaces

### `core.SearchEngine`

All engines implement:

- `Search(context.Context, Query) ([]SearchResult, error)`
- `SearchImage(context.Context, Query) ([]SearchResult, error)`
- `IsInitialized() bool`
- `Name() string`
- `GetRateLimiter() *rate.Limiter`

### `core.Query`

Parsed from query parameters (`text`, `lang`, `date`, `file`, `site`, `limit`, `start`, `filter`, `answers`) and the `X-Use-Proxy` request header. At least one of `text`, `site`, or `file` must be non-empty.

### Internal `core.SearchResult`

Engine parsers return the older internal shape:

- `Rank`
- `URL`
- `Title`
- `Description`
- `Ad`

HTTP handlers convert this into the public v1 response through `core/response_builder.go`.

## HTTP Request Flow

```text
HTTP request
  -> Fiber middleware
     -> RequestContextMiddleware
     -> CORS
     -> RequestLoggerMiddleware
  -> handleDedicatedEndpoint / handleMegaEndpoint
  -> Query.InitFromContext
  -> resolveFormat
  -> cache lookup for JSON responses only
  -> ResilientSearcher
     -> circuit breaker
     -> rate limiter
     -> proxy policy resolution
     -> retry loop
     -> engine.Search / engine.SearchImage
        -> browser path: Browser.Navigate -> DOM parse -> []SearchResult
        -> raw path: HTTP client -> goquery parse -> []SearchResult
  -> response enrichment
     -> stable IDs
     -> normalized URL/display URL
     -> pagination position
     -> domain_info/classification
     -> image metadata extraction
  -> mega-only normalized URL dedupe + clusters
  -> cache write for eligible JSON responses
  -> output serializer: JSON, Markdown, text, or NDJSON
```

## Public API Response

JSON endpoints return a v1 envelope.

Top-level fields:

- `query`: request echo, including `engines_requested`
- `meta`: `request_id`, `requested_at`, `took_ms`, `engines_failed`, `version`
- `results`: normalized web or image results
- `pagination`: `page`, `has_more`, `next_start`
- `clusters`: only on `/mega/search`

Stable ID prefixes:

- `s_`: web search result
- `i_`: image result
- `c_`: mega search URL cluster

`meta.engines_failed` is the only engine status list in the body. Clients can derive responded engines as:

```text
query.engines_requested - meta.engines_failed
```

Dedicated endpoint fallback is represented by:

- `X-Fallback-Engine`
- `results[].engine`
- `meta.engines_failed` containing the primary engine

## Mega Search

`/mega/search` and `/mega/image` run selected engines in parallel.

`/mega/search` behavior:

- Uses `engines` query parameter if provided; otherwise uses all configured engines.
- Skips duplicate engine names.
- Allows partial success; failed engines are listed in `meta.engines_failed`.
- Deduplicates flat results by normalized URL.
- Builds `clusters` from all enriched results before flat dedupe.
- Sorts clusters by score descending, then best rank ascending.

Cluster score:

```text
sum(1 / rank for each occurrence) / engines_queried
```

The score is capped at `1.0` and rounded to two decimals.

## Response Formatting

`resolveFormat` supports:

- `json` (default)
- `markdown`
- `text`
- `ndjson`

The format can be selected with `?format=` or by `Accept` header:

- `text/markdown`
- `text/plain`
- `application/x-ndjson`

Only JSON responses use the response cache. Cached JSON refreshes request-scoped metadata before sending:

- `meta.request_id`
- `meta.requested_at`
- `meta.took_ms`

## Domain Enrichment

`core/enrichment_domain.go` derives:

- `domain_info`: public suffix, SLD, and category booleans
- `classification`: content type and known source hint

Public suffix parsing uses `golang.org/x/net/publicsuffix`.

Mutable domain category data lives in:

```text
core/enrichment_domains.yaml
```

It can be replaced at runtime:

```bash
OPENSERP_ENRICHMENT_DOMAINS_FILE=/path/to/enrichment_domains.yaml ./openserp serve
```

## Resilience Stack

Request protection sequence:

1. Engine rate limiter
2. Retry with backoff
3. Circuit breaker
4. Proxy policy and proxy health
5. Response cache

Important behaviors:

- `ErrCaptcha` is non-retryable.
- Proxy health is degraded only for proxy/network failures, not parser or captcha errors.
- Dedicated endpoints are engine-pure by default.
- Dedicated fallback is opt-in via `resilience.allow_endpoint_fallback`.
- Fallback responses are not cached on dedicated endpoints.

## Proxy Model

Proxy policy can come from:

- global config
- per-engine config
- per-request `X-Use-Proxy`

Supported request override values:

- `X-Use-Proxy: direct`
- `X-Use-Proxy: <tag>`

Response headers:

- `X-Proxy-Mode`: `off` or `tag_pool`
- `X-Proxy-Tag`
- `X-Proxy-Used`

## Config Reference

Config priority: `CLI flags > OPENSERP_* env vars > config.yaml > defaults` (via Viper).

See [config.yaml](../config.yaml) for all available sections and defaults.

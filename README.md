![OpenSERP](./logo.svg)

# OpenSERP

[![Go Reference](https://pkg.go.dev/badge/github.com/karust/openserp.svg)](https://pkg.go.dev/github.com/karust/openserp)
[![release](https://img.shields.io/github/v/release/karust/openserp)](https://github.com/karust/openserp/releases)
[![Docker Pulls](https://img.shields.io/docker/v/karust/openserp)](https://hub.docker.com/r/karust/openserp)
[![CI](https://github.com/karust/openserp/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/karust/openserp/actions/workflows/ci.yml)
[![Telegram](https://img.shields.io/badge/Telegram-openserp__cloud-26A5E4?logo=telegram&logoColor=white)](https://t.me/openserp_cloud)

**OpenSERP** is a free, open-source SERP API and CLI for Google, Yandex, Baidu, Bing, DuckDuckGo, and Ecosia.

No API keys, no per-search billing: one command gives you live, structured search results on localhost - including engines the paid APIs don't cover. Use it as a search tool for LLMs and agents, or as a backend for SEO rank tracking. If you'd rather not run infrastructure, there is a [hosted version](https://openserp.org/cloud) with the same API.

![OpenSERP CLI demo](./docs/demo.gif)

## Features

- Dedicated endpoints for six engines, same JSON schema across all of them
- **Megasearch** - one query across several engines at once, merged and deduped
- **URL extraction** - search results plus clean markdown of the target pages in one call
- SERP features: AI summaries, answer boxes, people-also-ask, related searches
- Image search, filters (language, date range, file type, site), **JSON/Markdown/Text/NdJSON** output
- Proxies, cache, resilient mode, prebuilt Docker images

## Quick Start

### Docker

Prebuilt images are published to [docker hub: `karust/openserp`](https://hub.docker.com/r/karust/openserp).

```sh
# Run the API server via prebuilt image
docker run --rm -p 127.0.0.1:7000:7000 karust/openserp:latest serve -a 0.0.0.0 -p 7000

# Or
docker compose up
```

### Go install

```sh
go install github.com/karust/openserp@latest
openserp search duckduckgo "open source serp api" --format markdown
```

### From Source

```sh
git clone https://github.com/karust/openserp.git
cd openserp
go build -o openserp .
./openserp serve
```

### First request

```sh
# mode=any returns the first engine that responds
curl "http://127.0.0.1:7000/mega/search?engines=bing,google&text=golang+vs+rust&extract=1&mode=any"
```

<details>
<summary>Example JSON response</summary>

```json
{
  "query": {
    "text": "golang vs rust",
    "engines_requested": ["bing", "google"]
  },
  "meta": {
    "request_id": "019ecdc0-a66d-79a4-9d2b-9e9b480d495e",
    "requested_at": "2026-06-16T00:06:55Z",
    "took_ms": 720,
    "engines_responded": ["bing"],
    "engines_failed": [],
    "version": "2.1"
  },
  "results": [
    {
      "id": "s_5a8273f16b19ab64",
      "rank": 1,
      "type": "organic",
      "title": "The Go Programming Language",
      "url": "https://go.dev/",
      "display_url": "go.dev",
      "snippet": "Get Started Playground Tour Stack Overflow Help Packages Standard Library …",
      "domain": "go.dev",
      "favicon": "https://go.dev/favicon.ico",
      "position": {
        "absolute": 1
      },
      "engine": "bing",
      "domain_info": {
        "tld": "dev",
        "sld": "go",
        "category": ""
      },
      "extracted": {
        "title": "Build simple, secure, scalable systems with Go",
        "format": "markdown",
        "content": "## Build simple, secure, scalable systems with Go\n\n![Go Gopher climbing a ladder.](https://go.dev/images/gophers/ladder.svg)\n\n- “At the time, no single team member knew Go, but **within a month, everyone was writing in Go** and we were building out the endpoints. ........",
        "mode_used": "fast",
        "fetched_at": "2026-06-16T00:06:56Z"
      }
    },
    {
      "id": "s_1a364ebcb3035539",
      "rank": 2,
      "type": "organic",
      "title": "Go (programming language) - Wikipedia",
      "url": "https://en.wikipedia.org/wiki/Go_(programming_language)",
      "display_url": "en.wikipedia.org › wiki › Go_(programming_language)",
      "snippet": "In Go's package system, each package has a path (e.g., \"compress/bzip2\" or \"golang.org/x/net/html\") and a name (e.g., bzip2 or html). …",
      "domain": "en.wikipedia.org",
      "favicon": "https://en.wikipedia.org/favicon.ico",
      "position": {
        "absolute": 2
      },
      "engine": "bing",
      "domain_info": {
        "tld": "org",
        "sld": "wikipedia",
        "category": ""
      },
      "classification": {
        "content_type": "article",
        "source_hint": "encyclopedia"
      }
    },
    ...
  ],
  "serp_features": [],
  "pagination": {
    "page": 1,
    "has_more": false,
    "next_start": 10
  },
  "clusters": [
    {
      "id": "c_f20b23a020101dce",
      "canonical_url": "https://go.dev/",
      "domain": "go.dev",
      "title": "The Go Programming Language",
      "occurrences": [
        {
          "engine": "bing",
          "rank": 1,
          "result_id": "s_5a8273f16b19ab64"
        }
      ],
      "engines_count": 1,
      "best_rank": 1,
      "score": 0.5
    },
    ...
  ]
}
```

</details>

## SDKs & Examples

Official client packages. Each works against your self-hosted server (set `baseUrl`) or the [hosted API](https://openserp.org/cloud) (set `apiKey`):

| Type                        | Package                                                                                      | Source                                                              | Install                         |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------- |
| JavaScript / TypeScript SDK | [`@openserp/sdk`](https://www.npmjs.com/package/@openserp/sdk)                               | [openserpapi/sdk-js](https://github.com/openserpapi/sdk-js)         | `npm install @openserp/sdk`     |
| Python SDK                  | [`openserp`](https://pypi.org/project/openserp/)                                             | [openserpapi/sdk-python](https://github.com/openserpapi/sdk-python) | `pip install openserp`          |
| MCP server (AI agents)      | [`@openserp/mcp`](https://www.npmjs.com/package/@openserp/mcp)                               | [openserpapi/mcp](https://github.com/openserpapi/mcp)               | `npx @openserp/mcp`             |
| n8n community node          | [`@openserp/n8n-nodes-openserp`](https://www.npmjs.com/package/@openserp/n8n-nodes-openserp) | [openserpapi/n8n](https://github.com/openserpapi/n8n)               | Install via n8n community nodes |

See [**examples**](./examples) for small JavaScript and Python use cases covering search, AI grounding, SEO, content extraction, and image search.

```js
import { OpenSERP } from "@openserp/sdk";

// Use your self-hosted server
const client = new OpenSERP({ baseUrl: "http://localhost:7000" });
const { results } = await client.search({ engine: "google", text: "openserp", limit: 5 });
```

## Search Endpoints

Available engine names: `google`, `yandex`, `baidu`, `bing`, `duckduckgo`, `ecosia`.

Dedicated engine endpoints:

```bash
curl "http://127.0.0.1:7000/google/search?text=golang&limit=10"
```

Image search:

```bash
curl "http://127.0.0.1:7000/bing/image?text=golang+logo&limit=10"
```

Megasearch:

```bash
curl "http://127.0.0.1:7000/mega/search?text=golang&limit=10"
```

`/mega/search` returns the same envelope as engine endpoints plus `clusters`: results are deduplicated by normalized URL, and clusters keep the per-engine occurrences and ranks.

| Mode       | Best for                             | Behavior                                       |
| ---------- | ------------------------------------ | ---------------------------------------------- |
| `balanced` | Most multi-engine SERP workflows     | Queries engines in parallel and merges results |
| `fast`     | Lowest latency                       | Uses the fastest available engine              |
| `any`      | Fallback-style availability checking | Tries engines sequentially until one responds  |

<details>
<summary>More megasearch examples</summary>

```bash
# Fast mode
curl "http://127.0.0.1:7000/mega/search?text=golang&mode=fast&engines=google,bing,yandex"

# Any mode
curl "http://127.0.0.1:7000/mega/search?text=golang&mode=any&engines=google,yandex,bing"

# Balanced mode with aggregation controls
curl "http://127.0.0.1:7000/mega/search?text=golang&mode=balanced&dedupe=true&merge=true"

# Advanced filtering
curl "http://127.0.0.1:7000/mega/search?text=golang&engines=google,bing&limit=20&date=20250101..20251231&lang=EN&region=US"

# Image megasearch
curl "http://127.0.0.1:7000/mega/image?text=golang+logo&limit=20"
```

</details>

List engines:

```bash
curl "http://127.0.0.1:7000/mega/engines"
```

URL extraction:

```bash
# Extract one URL as JSON
curl "http://127.0.0.1:7000/extract?url=https://example.com&mode=auto"

# Return clean page markdown
curl "http://127.0.0.1:7000/extract?url=https://example.com&format=markdown"

# Extract several URLs at once - returns a bare [{page_content, metadata}] array
# (Open WebUI external loader compatible); failed URLs become items with metadata.error
curl -X POST "http://127.0.0.1:7000/extract/batch" \
  -H "Content-Type: application/json" \
  -d '{"urls":["https://example.com","https://go.dev"],"mode":"fast"}'

# Embed extracted content under the top search results
curl "http://127.0.0.1:7000/google/search?text=llm+observability&extract=2&format=markdown"
```

## CLI Search

No server required - query an engine straight from the terminal. The CLI shares the same engines, formats, and filters as the API.

```sh
openserp search ecosia "weather in london" --format markdown
```

<details>
<summary>CLI output and more examples</summary>

```markdown
# Search results for "weather in london"

**Query:** weather in london - **Engines:** ecosia - **Took:** 866ms

## Results

### 1. London - BBC Weather

**bbc.com › weather › 2643743** - organic

Latest forecast for London ... Tonight will continue dry, and there will be mainly clear skies. Just a few patches of cloud drifting in from the north at times.

-> https://www.bbc.com/weather/2643743

### 2. London (Greater London) weather - Met Office

**weather.metoffice.gov.uk › forecast › gcpvj0v07** - organic

Remaining warm with light winds and dry. Possibly cloudy at times Monday and Tuesday, then Wednesday sunnier conditions are likely.

-> https://weather.metoffice.gov.uk/forecast/gcpvj0v07

### 3. London, London, United Kingdom Weather Forecast

**accuweather.com › en › gb › london › ec4a-2 › wea…** - organic

London, London, United Kingdom Weather Forecast, with current conditions, wind, air quality, and what to expect for the next 3 days.

-> https://www.accuweather.com/en/gb/london/ec4a-2/weather-forecast/328328
```

More CLI examples:

```sh
# JSON is the default format
openserp search google "golang generics" --limit 20

# Plain text, German results
openserp search yandex "wetter berlin" --format text --lang DE --region DE

# Restrict to a site and stream NdJSON
openserp search bing "release notes" --site github.com --format ndjson

# Embed clean page content from the top 2 results
openserp search google "llm observability" --extract 2 --format markdown

# Browserless (raw HTTP) mode through a proxy
# (raw mode: google, yandex, baidu, ecosia)
openserp search ecosia "weather in london" --raw --proxy http://user:pass@127.0.0.1:8080
```

</details>

Run `openserp search --help` for the full flag list. Engine names: `google`, `yandex`, `baidu`, `bing`, `duckduckgo`, `ecosia`.

Use `--log-level` to control log verbosity, including startup warnings:

```sh
openserp search bing "golang" --log-level error
```

Supported levels are `panic`, `fatal`, `error`, `warn` (`warning` also works), `info`,
`debug`, and `trace`. You can also set `OPENSERP_APP_LOG_LEVEL` or `app.log_level`
in `config.yaml`; the flag overrides the environment variable, which overrides the
config file. An empty value preserves the existing defaults: CLI commands log
warnings and errors, while `serve` also logs info; `--verbose` enables debug logs
and `--debug` enables trace logs. A non-empty log level overrides the log threshold
from `--quiet`, `--verbose`, and `--debug`, without changing their other behavior
(such as `--debug` enabling the browser UI). Search results still go to stdout.

## Query Parameters

Common parameters:

| Parameter      | Description                                                                                                                                                                                             | Example                              |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| `text`         | Search query                                                                                                                                                                                            | `golang programming`                 |
| `lang`         | Language code                                                                                                                                                                                           | `EN`, `DE`, `RU`, `ES`               |
| `region`       | Market/location hint. Countries/locales work across engines; Google also accepts city names via `uule`; Yandex accepts numeric `lr`.                                                                    | `DE`, `en-GB`, `Berlin`, `213`       |
| `date`         | Date range                                                                                                                                                                                              | `20250101..20251231`                 |
| `file`         | File extension                                                                                                                                                                                          | `pdf`, `doc`, `xls`                  |
| `site`         | Site-specific search                                                                                                                                                                                    | `github.com`                         |
| `limit`        | Number of organic results, max 100. When omitted or `<=10`, only the first SERP page is parsed.                                                                                                         | `25`, `50`                           |
| `start`        | Pagination offset                                                                                                                                                                                       | `0`, `10`, `20`                      |
| `format`       | Output format                                                                                                                                                                                           | `json`, `markdown`, `text`, `ndjson` |
| `extract`      | Fetch and embed target-page content for top web results. Bool or int depth: `0`/`false` off, `true`/`1` top result, `N` top N (1-5). `extract_mode`/`min_runes` imply `extract=true` unless `extract=0` | `1`, `3`, `true`                     |
| `extract_mode` | Extraction strategy: raw HTTP first, raw only, or browser-rendered                                                                                                                                      | `auto`, `fast`, `rendered`           |

Engine-specific parameters:

| Parameter  | Supported engines | Notes                                                                  |
| ---------- | ----------------- | ---------------------------------------------------------------------- |
| `filter`   | `google`          | Duplicate filter: `true` hides similar results, `false` includes them. |
| `features` | browser `Search`  | Populate `serp_features[]` from the live page. Defaults to `true`.     |

## Proxy Support

OpenSERP supports HTTP and SOCKS5 proxies.

Simple global proxy:

```bash
./openserp serve --proxy socks5://127.0.0.1:1080
./openserp search bing "query" --proxy http://user:pass@127.0.0.1:8080
```

Advanced proxy configuration is available in [config.yaml](./config.yaml). You can enable tagged proxy pools and per-request override via `X-Use-Proxy: <tag>` or `X-Use-Proxy: direct`.

## API Docs

Once the server is running, the interactive docs are available locally:

- Swagger UI: `http://127.0.0.1:7000/docs` - full schemas, error shapes, and the `/health`, `/ready`, `/stats/*` endpoints
- OpenAPI YAML: `http://127.0.0.1:7000/openapi.yaml`

To browse the spec without running the server, see [docs/openapi.yaml](./docs/openapi.yaml). For a higher-level overview of how OpenSERP works internally, see the [architecture docs](https://openserp.org/docs/architecture/).

## Self-Hosted or Cloud

- **Self-hosted (this repo)** - free, MIT-licensed, full control over runtime, proxies, cache, and scaling.
- **[OpenSERP Cloud](https://openserp.org/cloud)** - same endpoints and response schema, no infrastructure to run.

Client code migrates in either direction, so you are never locked in.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).

## Contributing

Contributions are welcome. See [docs/CONTRIBUTING.md](./docs/CONTRIBUTING.md).

## Feedback & Updates

- [GitHub Issues](https://github.com/karust/openserp/issues) - bugs, feature ideas, and reproducible issues.
- [feedback@openserp.org](mailto:feedback@openserp.org) - private notes, longer feedback, or anything that does not fit GitHub Issues.
- [Telegram](https://t.me/openserp_cloud) - OpenSERP news, release notes, and project updates.

> OpenSERP is free and open-source. Only links listed in this repository and on [openserp.org](https://openserp.org) are associated with the project.

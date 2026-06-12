![OpenSERP](./logo.svg)

# OpenSERP (Search Engine Results)

[![Go Report Card](https://goreportcard.com/badge/github.com/karust/openserp)](https://goreportcard.com/report/github.com/karust/openserp)
[![Go Reference](https://pkg.go.dev/badge/github/karust/openserp?style=for-the-badge)](https://pkg.go.dev/github.com/karust/openserp)
[![release](https://img.shields.io/github/v/release/karust/openserp)](https://github.com/karust/openserp/releases)
[![Docker Pulls](https://img.shields.io/docker/v/karust/openserp)](https://hub.docker.com/r/karust/openserp)
[![CI](https://github.com/karust/openserp/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/karust/openserp/actions/workflows/ci.yml)

**OpenSERP** is a free, open-source API and CLI for accessing normalized search engine results from **Google, Yandex, Baidu, Bing, DuckDuckGo, and Ecosia**.

Run it locally, self-host it, or use the optional hosted API when you do not want to manage infrastructure.

**Official website:** [openserp.org](https://openserp.org)

**Feedback:** [GitHub Issues](https://github.com/karust/openserp/issues) or [feedback@openserp.org](mailto:feedback@openserp.org)

**Latest updates, usage examples**: [Telegram](https://t.me/+RJEKspw3mUlhZDMy)

> 💡 OpenSERP is free and open-source. Only links listed in this repository and on the official website are associated with the project.

## Features

- 🔍 **Multi-engine** - search with dedicated endpoints for each engine
- 🌐 **Megasearch** - cross-engine aggregation with deduplication
- 🖼 **Images** - image search is also available
- 🎯 **Advanced filters** - language, date range, file type, and site queries
- ✨ **SERP features** - AI summaries, answer boxes, people-also-ask, and related searches in a response
- 📄 **URL extraction** - turn target pages into clean markdown/text for grounding and automation
- 🌍 **Configurable** - proxy, cache, and resilient mode
- 🐳 **Docker-ready** - local and container deployment
- 📝 **Data Formats** - JSON, Markdown, Text, NdJSON response formats

## ⚡ Quick Start

### Docker

```sh
# Run the API server via prebuilt image
docker run --rm -p 127.0.0.1:7000:7000 karust/openserp:latest serve -a 0.0.0.0 -p 7000

# Or use docker-compose (pulls the prebuilt image)
docker compose up
```

### From Source

```sh
git clone https://github.com/karust/openserp.git
cd openserp
go build -o openserp .
./openserp serve
```

## Deployment Options

- **Self-hosted (this repo)** - free, MIT-licensed, with full control over runtime, proxies, cache, and scaling.
- **[Hosted API](https://openserp.org/cloud)** - optional managed version from the project maintainers, with the same API shape.

The hosted API helps fund continued development of the open-source project. Same endpoints, same response schema, and client code can migrate either direction.

## API Docs

Once the server is running, the interactive docs are available locally:

- Swagger UI: `http://127.0.0.1:7000/docs`
- OpenAPI YAML: `http://127.0.0.1:7000/openapi.yaml`

To browse the spec without running the server, see [docs/openapi.yaml](./docs/openapi.yaml). For a higher-level overview of how OpenSERP works internally, see the [architecture docs](https://openserp.org/docs/architecture/).

## SDKs & Examples

Official client packages. Each works against your self-hosted server (set `baseUrl`) or the [hosted API](https://openserp.org/cloud) (set `apiKey`):

| Type                        | Package                                                                                      | Install                         |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------------------- |
| JavaScript / TypeScript SDK | [`@openserp/sdk`](https://www.npmjs.com/package/@openserp/sdk)                               | `npm install @openserp/sdk`     |
| Python SDK                  | [`openserp`](https://pypi.org/project/openserp/)                                             | `pip install openserp`          |
| MCP server (AI agents)      | [`@openserp/mcp`](https://www.npmjs.com/package/@openserp/mcp)                               | `npx @openserp/mcp`             |
| n8n community node          | [`@openserp/n8n-nodes-openserp`](https://www.npmjs.com/package/@openserp/n8n-nodes-openserp) | Install via n8n community nodes |

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
# Search all configured engines
curl "http://127.0.0.1:7000/mega/search?text=golang&limit=10"

# Fast mode: only one fastest engine is queried
curl "http://127.0.0.1:7000/mega/search?text=golang&mode=fast&engines=google,bing,yandex"

# Any mode: sequential fallback in provided order (default order if none provided)
curl "http://127.0.0.1:7000/mega/search?text=golang&mode=any&engines=google,yandex,bing"

# Balanced mode (default): parallel all engines with aggregation controls
curl "http://127.0.0.1:7000/mega/search?text=golang&mode=balanced&dedupe=true&merge=true"

# Advanced filtering
curl "http://127.0.0.1:7000/mega/search?text=golang&engines=google,bing&limit=20&date=20250101..20251231&lang=EN&region=US"

# Image megasearch
curl "http://127.0.0.1:7000/mega/image?text=golang+logo&limit=20"
```

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

# Embed extracted content under the top search results
curl "http://127.0.0.1:7000/google/search?text=llm+observability&extract=true&extract_top=2&format=markdown"
```

## 🔍 Query Parameters

Common parameters:

| Parameter      | Description                                                                                                                          | Example                              |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------ |
| `text`         | Search query                                                                                                                         | `golang programming`                 |
| `lang`         | Language code                                                                                                                        | `EN`, `DE`, `RU`, `ES`               |
| `region`       | Market/location hint. Countries/locales work across engines; Google also accepts city names via `uule`; Yandex accepts numeric `lr`. | `DE`, `en-GB`, `Berlin`, `213`       |
| `date`         | Date range                                                                                                                           | `20250101..20251231`                 |
| `file`         | File extension                                                                                                                       | `pdf`, `doc`, `xls`                  |
| `site`         | Site-specific search                                                                                                                 | `github.com`                         |
| `limit`        | Number of organic results, max 100. When omitted or `<=10`, only the first SERP page is parsed.                                      | `25`, `50`                           |
| `start`        | Pagination offset                                                                                                                    | `0`, `10`, `20`                      |
| `format`       | Output format                                                                                                                        | `json`, `markdown`, `text`, `ndjson` |
| `extract`      | Fetch and embed target-page content for top web results                                                                              | `true`                               |
| `extract_top`  | Number of top web results to extract, clamped to 1-5                                                                                 | `3`                                  |
| `extract_mode` | Extraction strategy: raw HTTP first, raw only, or browser-rendered                                                                   | `auto`, `fast`, `rendered`           |

Engine-specific parameters:

| Parameter  | Supported engines | Notes                                                                  |
| ---------- | ----------------- | ---------------------------------------------------------------------- |
| `filter`   | `google`          | Duplicate filter: `true` hides similar results, `false` includes them. |
| `features` | browser `Search`  | Populate `serp_features[]` from the live page. Defaults to `true`.     |

## Search Response Example

<details>
<summary>Search response example</summary>

```json
{
  "query": {
    "text": "golang",
    "engines_requested": ["google"]
  },
  "meta": {
    "request_id": "019dc6c1-da45-706e-a57c-d671fa2862ee",
    "requested_at": "2026-04-25T22:27:52Z",
    "took_ms": 6410,
    "engines_failed": [],
    "version": "2.1"
  },
  "results": [
    {
      "id": "s_78341aa47c336101",
      "rank": 1,
      "type": "organic",
      "title": "Documentation - The Go Programming Language",
      "url": "https://go.dev/doc/",
      "display_url": "go.dev > doc",
      "snippet": "Official Go documentation, tutorials, references, and release notes.",
      "domain": "go.dev",
      "favicon": "https://go.dev/favicon.ico",
      "position": {
        "absolute": 1
      },
      "engine": "google",
      "domain_info": {
        "tld": "dev",
        "sld": "go",
        "category": ""
      }
    }
  ],
  "pagination": {
    "page": 1,
    "has_more": true,
    "next_start": 25
  }
}
```

</details>

## Mega Response Notes

`/mega/search` returns the same envelope plus `clusters`. Results are deduplicated by normalized URL; clusters keep the per-engine occurrences.

<details>
<summary>Cluster example</summary>

```json
{
  "id": "c_a1b2c3d4e5f6a1b2",
  "canonical_url": "https://go.dev/",
  "domain": "go.dev",
  "title": "The Go Programming Language",
  "occurrences": [
    { "engine": "google", "rank": 1, "result_id": "s_78341aa47c336101" },
    { "engine": "bing", "rank": 2, "result_id": "s_20f9f15f0c3d9f6d" }
  ],
  "engines_count": 2,
  "best_rank": 1,
  "score": 0.75
}
```

</details>

## Image Response Example

<details>
<summary>Image result example</summary>

```json
{
  "id": "i_a1b2c3d4e5f6a1b2",
  "rank": 1,
  "type": "image",
  "title": "Go Gopher Logo",
  "image": {
    "url": "https://example.com/images/go-logo.png",
    "thumbnail": "https://example.com/images/go-logo-thumb.png",
    "width": 1200,
    "height": 800
  },
  "source": {
    "page_url": "https://go.dev/brand/",
    "domain": "go.dev"
  },
  "engine": "bing"
}
```

</details>

## Error Responses

`400 Bad Request`:

```json
{
  "error": "bad_request",
  "code": 400,
  "message": "EMPTY_QUERY: query cannot be empty: provide text, site, or file parameter",
  "reason": "EMPTY_QUERY"
}
```

`503 Service Unavailable`:

```json
{
  "error": "service_unavailable",
  "code": 503,
  "message": "captcha found, please stop sending requests for a while: captcha detected"
}
```

## 🌍 Proxy Support

OpenSERP supports HTTP and SOCKS5 proxies.

Simple global proxy:

```bash
./openserp serve --proxy socks5://127.0.0.1:1080
./openserp search bing "query" --proxy http://user:pass@127.0.0.1:8080
```

Advanced proxy configuration is available in [config.yaml](./config.yaml). You can enable tagged proxy pools and per-request override via `X-Use-Proxy: <tag>` or `X-Use-Proxy: direct`.

A [managed API](https://openserp.org/cloud) is also available for teams that do not want to operate infrastructure.

## Health & Stats

```bash
curl -i "http://127.0.0.1:7000/health"
curl "http://127.0.0.1:7000/ready"
curl "http://127.0.0.1:7000/stats"
curl "http://127.0.0.1:7000/stats/cache"
curl "http://127.0.0.1:7000/stats/proxy"
curl "http://127.0.0.1:7000/stats/cb"
```

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).

## Contributing

Contributions are welcome. See [docs/CONTRIBUTING.md](./docs/CONTRIBUTING.md).

## Feedback & Updates

- [GitHub Issues](https://github.com/karust/openserp/issues) - bugs, feature ideas, and reproducible issues.
- [Telegram channel](https://t.me/openserp_cloud) - OpenSERP news, release notes, and project updates. Direct messages are open for quick feedback and hosted API questions.
- [feedback@openserp.org](mailto:feedback@openserp.org) - private notes, longer feedback, or anything that does not fit GitHub Issues.

###### _"OpenSERP" is the name of this open-source project. The official website is [openserp.org](https://openserp.org). Resources not linked on this page are not affiliated with the project._

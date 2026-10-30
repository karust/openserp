![OpenSERP](./logo.svg)

# OpenSERP

[![Go Reference](https://pkg.go.dev/badge/github.com/karust/openserp.svg)](https://pkg.go.dev/github.com/karust/openserp)
[![release](https://img.shields.io/github/v/release/karust/openserp)](https://github.com/karust/openserp/releases)
[![Docker Pulls](https://img.shields.io/docker/v/karust/openserp)](https://hub.docker.com/r/karust/openserp)
[![CI](https://github.com/karust/openserp/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/karust/openserp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Telegram](https://img.shields.io/badge/Telegram-openserp__cloud-26A5E4?logo=telegram&logoColor=white)](https://t.me/openserp_cloud)

OpenSERP is a **free self-hosted web search for AI agents and SEO.** REST API and CLI for Google, Bing, Yandex, Baidu, DuckDuckGo and Ecosia.

No API keys, no per-search billing. It loads search results in browser and returns structured JSON with source engines and ranks. Read the linked pages as clean Markdown, or connect an agent through MCP.

Works with Claude Code, Cursor, Codex, Claude Desktop and any MCP client, [Open WebUI](https://docs.openwebui.com/features/chat-conversations/web-search/providers/openserp/) (built-in provider), [n8n](https://www.npmjs.com/package/@openserp/n8n-nodes-openserp), and plain HTTP from any language.

[API examples](#search-endpoints) · [CLI](#cli-search) · [Agent setup](#use-with-ai-agents) · [API reference](./docs/openapi.yaml)

![Claude Code answering a question with OpenSERP over MCP](./docs/agent-demo.gif)

## Quick Start

```sh
# Start the API server
docker run --rm -p 127.0.0.1:7000:7000 karust/openserp serve -a 0.0.0.0 -p 7000

# Search Google and read the top result as Markdown
curl "http://127.0.0.1:7000/google/search?text=how+do+mcp+servers+work&extract=1"
```

<details>
<summary>Other ways to install</summary>

```sh
# Docker Compose
docker compose up

# Go
go install github.com/karust/openserp@latest
openserp serve

# From source
git clone https://github.com/karust/openserp.git
cd openserp && go build -o openserp . && ./openserp serve
```

Prebuilt binaries for Linux, macOS and Windows are on the [releases page](https://github.com/karust/openserp/releases). Images: [`karust/openserp`](https://hub.docker.com/r/karust/openserp).

</details>

## Use with AI Agents

### MCP: Claude Code, Codex, Cursor, Claude Desktop

Requires Node.js 18+ and a running OpenSERP server. Add the MCP server to your client:

```sh
claude mcp add openserp -- npx -y @openserp/mcp
codex mcp add openserp -- npx -y @openserp/mcp
```

Cursor (`~/.cursor/mcp.json`), Claude Desktop, Windsurf and other clients:

```json
{
  "mcpServers": {
    "openserp": { "command": "npx", "args": ["-y", "@openserp/mcp"] }
  }
}
```

On native Windows, use `cmd /c npx -y @openserp/mcp` after `--` in the CLI commands, or `"command": "cmd", "args": ["/c", "npx", "-y", "@openserp/mcp"]` in JSON.

For a remote server, add `"env": { "OPENSERP_BASE_URL": "http://your-host:7000" }` to the `openserp` JSON entry.

Tools: `search`, `mega_search`, `fast_search`, `any_search`, `image_search`, `mega_image`, `extract`, `batch_extract`, `list_engines`. Then just ask: _"Search Google for the latest Postgres release, read the top 2 results and answer with links."_

Optional for Claude Code: copy [skills/openserp-search](./skills/openserp-search) to `~/.claude/skills/` so the agent searches, reads and falls back to another engine the same way every time.

### Open WebUI

OpenSERP is a built-in web search provider. In **Settings > Admin > Web Search** pick `openserp` and set the URL (`http://host.docker.internal:7000` if Open WebUI runs in Docker). See the [Open WebUI guide](https://docs.openwebui.com/features/chat-conversations/web-search/providers/openserp/).

### In your own agent

A search tool for any LLM framework is a few lines with the SDK:

```python
from openserp import OpenSERP

def web_search(query: str) -> list[dict]:
    """Search Google and return ranked results for the model."""
    with OpenSERP(base_url="http://localhost:7000") as client:
        res = client.search(engine="google", text=query, limit=10)
        return [
            {"rank": r.rank, "title": r.title, "url": r.url, "snippet": r.snippet}
            for r in res.results if r.type == "organic"
        ]
```

```js
import { OpenSERP } from "@openserp/sdk";

const client = new OpenSERP({ baseUrl: "http://localhost:7000" });
const { results } = await client.search({ engine: "google", text: "openserp", limit: 5 });
```

| Package                                                                                      | Install                         | Source                                                              |
| -------------------------------------------------------------------------------------------- | ------------------------------- | ------------------------------------------------------------------- |
| [`@openserp/mcp`](https://www.npmjs.com/package/@openserp/mcp) - MCP server                  | `npx -y @openserp/mcp`          | [openserpapi/mcp](https://github.com/openserpapi/mcp)               |
| [`@openserp/sdk`](https://www.npmjs.com/package/@openserp/sdk) - JS / TS                     | `npm install @openserp/sdk`     | [openserpapi/sdk-js](https://github.com/openserpapi/sdk-js)         |
| [`openserp`](https://pypi.org/project/openserp/) - Python                                    | `pip install openserp`          | [openserpapi/sdk-python](https://github.com/openserpapi/sdk-python) |
| [`@openserp/n8n-nodes-openserp`](https://www.npmjs.com/package/@openserp/n8n-nodes-openserp) | Install via n8n community nodes | [openserpapi/n8n](https://github.com/openserpapi/n8n)               |

More in [examples](./examples): agent tools, RAG context, SEO rank tracking, extraction and image search.

## Response Example

`GET /google/search?text=how+do+mcp+servers+work&extract=1` (trimmed):

```json
{
  "query": { "text": "how do mcp servers work", "engines_requested": ["google"] },
  "meta": { "took_ms": 4353, "engines_failed": [], "version": "2.1" },
  "results": [
    {
      "rank": 1,
      "type": "organic",
      "engine": "google",
      "title": "What Is the Model Context Protocol (MCP) and How It Works",
      "url": "https://www.descope.com/learn/post/mcp",
      "domain": "descope.com",
      "snippet": "MCP is an open standard that lets AI applications connect to external tools, data, and services through a client-server protocol built on JSON-…",
      "position": { "absolute": 1 },
      "extracted": {
        "format": "markdown",
        "content": "The Model Context Protocol (MCP) is an open standard that defines how AI applications connect to external tools, data sources, and services. MCP clients inside AI apps send requests to MCP servers, ..."
      }
    },
    {
      "rank": 2,
      "type": "organic",
      "engine": "google",
      "title": "No Dumb Questions: What is an MCP server and why do I ...",
      "url": "https://stackoverflow.blog/2026/05/08/no-dumb-questions-mcp",
      "domain": "stackoverflow.blog"
    }
  ]
}
```

Results keep their source engine and rank. Ads are typed separately from organic results; `extract=N` attaches up to N organic pages as Markdown and records per-page failures in `extracted.error`. Full schema in the [OpenAPI spec](./docs/openapi.yaml).

## Why OpenSERP

- **Browser-backed search.** Chromium loads the rendered SERP, including supported answer boxes, related searches and AI summaries. Raw HTTP mode is also available for Google, Yandex, Baidu and Ecosia.
- **Six engines, one schema.** Google, Bing, Yandex, Baidu, DuckDuckGo and Ecosia return the same JSON. Engine and rank survive a multi-engine merge, so you can see who ranked what.
- **Search and read in one call.** `extract=N` adds page Markdown under the top organic results, `/extract/batch` reads up to 20 URLs, and a dead link comes back as an error item instead of failing the batch.
- **Yours to run.** One Go binary or Docker image, with HTTP/SOCKS5 proxies, cache, retries, rate limits and circuit breakers. No per-query fees; you cover compute and proxies.

## Compared to Paid Search APIs

| Feature                            | OpenSERP              | [Cloud](https://openserp.org/cloud/) | [Tavily](https://www.tavily.com/pricing) | [SerpApi](https://serpapi.com/pricing) | [Exa](https://exa.ai/pricing) |
| ---------------------------------- | --------------------- | ------------------------------------ | ---------------------------------------- | -------------------------------------- | ----------------------------- |
| Google results                     | ✅                    | ✅                                   | 🟨 Undisclosed                           | ✅                                     | ❌                            |
| Search engines                     | 6                     | 6                                    | Own retrieval                            | Many                                   | Exa index                     |
| Original engine ranks              | ✅                    | ✅                                   | ❌                                       | ✅                                     | ❌                            |
| Page extraction                    | ✅ Markdown, batch    | ✅ Markdown, batch                   | ✅                                       | ✅                                     | ✅                            |
| Self-hosting                       | ✅                    | ❌                                   | ❌                                       | ❌                                     | ❌                            |
| SEO monitoring, rank tracking      | 🟨                    | ✅                                   | ❌                                       | 🟨                                     | ❌                            |
| PR monitoring, competitor tracking | 🟨                    | ✅ Beta                              | 🟨                                       | 🟨                                     | ✅ API                        |
| AI reports and alerts              | 🟨                    | ✅                                   | 🟨                                       | 🟨                                     | 🟨                            |
| Pricing                            | Free + infrastructure | PAYG, **$0.0078/credit**             | $30/mo, 4k basic credits                 | $25/mo, 1k searches                    | PAYG, $4/1k Lite searches     |

✅ Available · 🟨 Partial, unconfirmed or requires integration · ❌ Unavailable

## Search Endpoints

Engine names: `google`, `yandex`, `baidu`, `bing`, `duckduckgo` (also `duck`, `ddg`), `ecosia`.

Use a dedicated endpoint when the source engine matters, `any` for ordered fallback, or `balanced` to compare engines. Search supports JSON, Markdown, text and NDJSON, plus language, region, date, site and file filters.

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

| Mode       | Best for                               | Behavior                                                          |
| ---------- | -------------------------------------- | ----------------------------------------------------------------- |
| `balanced` | Most multi-engine SERP workflows       | Queries engines in parallel and merges results                    |
| `fast`     | One engine selected by latency history | Uses one engine; does not fall back on failure                    |
| `any`      | Ordered fallback                       | Tries engines in order until one succeeds, even with zero results |

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

# Embed extracted content under the top organic results
curl "http://127.0.0.1:7000/google/search?text=llm+observability&extract=2&format=markdown"
```

## CLI Search

No server required - query an engine straight from the terminal. The CLI shares the same engines, formats, and filters as the API.

```sh
openserp search ecosia "weather in london" --format markdown
```

![OpenSERP CLI demo](./docs/demo.gif)

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
```

More CLI examples:

```sh
# JSON is the default format
openserp search google "golang generics" --limit 20

# Plain text, German results
openserp search yandex "wetter berlin" --format text --lang DE --region DE

# Restrict to a site and stream NdJSON
openserp search bing "release notes" --site github.com --format ndjson

# Embed clean page content from the top 2 organic results
openserp search google "llm observability" --extract 2 --format markdown

# Browserless (raw HTTP) mode through a proxy
# (raw mode: google, yandex, baidu, ecosia)
openserp search ecosia "weather in london" --raw --proxy http://user:pass@127.0.0.1:8080
```

</details>

Run `openserp search --help` for the full flag list. Results go to stdout, logs to stderr. Set verbosity with `--log_level error|warn|info|debug|trace` or `OPENSERP_APP_LOG_LEVEL` (CLI default: `warn`). `serve` also writes `./logs.txt` unless `app.save_log: false`.

## Query Parameters

<details>
<summary>Common parameters</summary>

| Parameter      | Description                                                                                                                                                                                                                     | Example                              |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| `text`         | Search query                                                                                                                                                                                                                    | `golang programming`                 |
| `lang`         | Language code                                                                                                                                                                                                                   | `EN`, `DE`, `RU`, `ES`               |
| `region`       | Market/location hint. Countries/locales work across engines; Google also accepts city names via `uule`; Yandex accepts numeric `lr`.                                                                                            | `DE`, `en-GB`, `Berlin`, `213`       |
| `date`         | Date range                                                                                                                                                                                                                      | `20250101..20251231`                 |
| `file`         | File extension                                                                                                                                                                                                                  | `pdf`, `doc`, `xls`                  |
| `site`         | Site-specific search                                                                                                                                                                                                            | `github.com`                         |
| `limit`        | Number of organic results, max 100. When omitted or `<=10`, only the first SERP page is parsed.                                                                                                                                 | `25`, `50`                           |
| `start`        | Pagination offset                                                                                                                                                                                                               | `0`, `10`, `20`                      |
| `format`       | Output format                                                                                                                                                                                                                   | `json`, `markdown`, `text`, `ndjson` |
| `extract`      | Fetch and embed target-page content for organic results. Ads and answer rows are skipped. `0`/`false` off, `true`/`1` top organic result, `N` up to N (1-5). `extract_mode`/`min_runes` imply `extract=true` unless `extract=0` | `1`, `3`, `true`                     |
| `extract_mode` | Extraction strategy: raw HTTP first, raw only, or browser-rendered                                                                                                                                                              | `auto`, `fast`, `rendered`           |

</details>

<details>
<summary>Engine-specific parameters</summary>

| Parameter      | Supported engines | Notes                                                                                                                            |
| -------------- | ----------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `filter`       | `google`          | Duplicate filter: `true` hides similar results, `false` includes them.                                                           |
| `features`     | browser `Search`  | Populate `serp_features[]` from the live page. Defaults to `true`.                                                               |
| `resolve_urls` | `google`          | Turn Google `/goto` redirect links into destination URLs (one redirect hop, ads skipped). `true` (default), `false`, or top `N`. |

</details>

Bing localizes by exit IP. `lang` sets its interface language; `region` sends a country hint except for `US`, `CN` and `RU`, which caused unrelated results in testing. For those locations, use a proxy in the target country.

## Proxy Support

OpenSERP supports HTTP and SOCKS5 proxies.

```bash
./openserp serve --proxy socks5://127.0.0.1:1080
./openserp search bing "query" --proxy http://user:pass@127.0.0.1:8080
```

Engines can return CAPTCHAs, rate limits or unrelated results. Browser mode does not eliminate these; try another engine or a proxy when needed.

Advanced proxy configuration is available in [config.yaml](./config.yaml). You can enable tagged proxy pools and per-request override via `X-Use-Proxy: <tag>` or `X-Use-Proxy: direct`.

## Custom DNS

When the system DNS filters search domains, point direct lookups at another server:

```bash
./openserp serve --dns-server 8.8.8.8:53
./openserp search google "query" --raw --dns-server 8.8.8.8:53
```

Or set `dns_server: 8.8.8.8:53` in [config.yaml](./config.yaml). This covers raw-mode
and direct HTTP traffic; browser rendering still resolves via system or proxy DNS.

## API Docs

Once the server is running, the interactive docs are available locally:

- Swagger UI: `http://127.0.0.1:7000/docs` - full schemas, error shapes, and the `/health`, `/ready`, `/stats/*` endpoints
- OpenAPI YAML: `http://127.0.0.1:7000/openapi.yaml`

To browse the spec without running the server, see [docs/openapi.yaml](./docs/openapi.yaml). For how OpenSERP works internally, see the [architecture docs](https://openserp.org/docs/architecture/).

## Contributing

Contributions are welcome. See [docs/CONTRIBUTING.md](./docs/CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).

## Feedback & Updates

- [GitHub Issues](https://github.com/karust/openserp/issues) - bugs, feature ideas, and reproducible issues.
- [feedback@openserp.org](mailto:feedback@openserp.org) - private notes, longer feedback, or anything that does not fit GitHub Issues.
- [Telegram](https://t.me/openserp_cloud) - OpenSERP news, release notes, and project updates.

> OpenSERP is free and open-source. Only links listed in this repository and on [openserp.org](https://openserp.org) are associated with the project.

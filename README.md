# OpenSERP (Search Engine Results)

![OpenSERP](/logo.svg)

[![Go Report Card](https://goreportcard.com/badge/github.com/karust/openserp)](https://goreportcard.com/report/github.com/karust/openserp)
[![Go Reference](https://pkg.go.dev/badge/github/karust/openserp?style=for-the-badge)](https://pkg.go.dev/github.com/karust/openserp)
[![release](https://img.shields.io/github/release/karust/openserp)](https://github.com/karust/openserp/releases)

<!-- [![Docker Pulls](https://img.shields.io/docker/pulls/karust/openserp)](https://hub.docker.com/repository/docker/karust/openserp) -->

**OpenSERP** provides free API and CLI access to multiple search engines including **Google, Yandex, Baidu, Bing, and DuckDuckGo**. Get comprehensive search results without expensive API subscriptions!

## Features

- 🔍 **Multi-engine** - search with dedicated endpoints for each engine
- 🌐 **Megasearch** - cross-engine aggregation with deduplication
- 🖼 **Images** - image search is also available
- 🎯 **Advanced filters** - language, date range, file type, and site queries
- 🌍 **Configurable** - proxy, cache, and resilient mode support
- 🐳 **Docker-ready** - local and container deployment

## Quick Start⚡️

### Docker (Recommended)

```bash
# Run the API server via prebuilt image
docker run -p 127.0.0.1:7000:7000 -it karust/openserp serve -a 0.0.0.0 -p 7000

# Or use docker-compose
docker compose up --build
```

### From source

```bash
git clone https://github.com/karust/openserp.git
cd openserp
go build -o openserp .
./openserp serve
```

## 🌐 Megasearch & Megaimage

Search all engines at once:

```bash
curl "http://127.0.0.1:7000/mega/search?text=golang&limit=10"
```

Search only selected engines:

```bash
curl "http://127.0.0.1:7000/mega/search?text=golang&engines=duckduckgo,bing&limit=15"
```

Advanced filtering:

```bash
curl "http://127.0.0.1:7000/mega/search?text=Donald+Trump&engines=duckduckgo,bing&limit=20&date=20251005..20251005&lang=EN"
```

API response example:

```json
[
  {
    "rank": 1,
    "url": "https://en.wikipedia.org/wiki/Golden_Retriever",
    "title": "Golden Retriever - Wikipedia",
    "description": "The Golden Retriever is a Scottish breed of retriever dog of medium size. It is characterised by a gentle and affectionate nature and a striking golden coat.",
    "ad": false,
    "engine": "duckduckgo"
  },
  {
    "rank": 2,
    "url": "https://www.bing.com/ck/a?!&&p=6f15ac4589858d0a104cd6f55cc8",
    "title": "Golden Retriever Dog Forums",
    "description": "Oct 20, 2024 · Back in the 1970s, Golden Retrievers routinely lived until 16 and 17 years old, they are now...",
    "ad": false,
    "engine": "bing"
  },
  {
    "rank": 3,
    "url": "http://www.baidu.com/link?url==2544q3ugc68j0scVxdpWCSX-gl2AmuCy1l7uRR3loIfS1",
    "title": "golden retrievers是什么意思",
    "description": "2025年9月21日golden retrievers 读音:美英 golden retrievers基本解释 金毛猎犬 分词解释 golden金(黄)色的...",
    "ad": false,
    "engine": "baidu"
  }
]
```

Image search:

```bash
curl "http://127.0.0.1:7000/mega/image?text=golang logo&limit=20"
```

List available engines:

```bash
curl "http://127.0.0.1:7000/mega/engines"
```

**Available engines:** `google`, `yandex`, `baidu`, `bing`, `duckduckgo`

## 🔍 Individual Engine APIs

Common query parameters:

| Parameter | Description          | Example                           |
| --------- | -------------------- | --------------------------------- |
| `text`    | Search query         | `golang programming`              |
| `lang`    | Language code        | `EN`, `DE`, `RU`, `ES`            |
| `date`    | Date range           | `20230101..20231231`              |
| `file`    | File extension       | `PDF`, `DOC`, `XLS`               |
| `site`    | Site-specific search | `github.com`, `stackoverflow.com` |
| `limit`   | Number of results    | `10`, `25`, `50`                  |

Engine-specific parameters:

| Parameter | Supported engines                   | Notes                                                              |
| --------- | ----------------------------------- | ------------------------------------------------------------------ |
| `start`   | `google`, `bing`, `yandex`, `baidu` | Web search pagination offset.                                      |
| `filter`  | `google`                            | Duplicate filter (`true` hides similar, `false` includes similar). |
| `answers` | `google`                            | Include Google answer boxes in output with negative ranks.         |

Examples:

```bash
curl "http://127.0.0.1:7000/duck/search?text=golang&limit=7"
curl "http://127.0.0.1:7000/google/search?text=golang&lang=EN&limit=10"
curl "http://127.0.0.1:7000/bing/search?text=golang&limit=10&start=20"
curl "http://127.0.0.1:7000/yandex/search?text=golang&limit=10&start=10"
curl "http://127.0.0.1:7000/bing/image?text=golang&limit=20"
```

## 🌍 Proxy Support

OpenSERP supports HTTP and SOCKS5 proxies.

Simple global proxy:

```bash
./openserp serve --proxy socks5://127.0.0.1:1080
./openserp search bing "query" --proxy http://user:pass@127.0.0.1:8080
```

Advanced proxy configuration is available in [config.yaml](./config.yaml).
You can enable tagged proxy pools and per-request override via `X-Use-Proxy: <tag>` or `X-Use-Proxy: direct`.

## Health & Stats

```bash
curl -i "http://127.0.0.1:7000/health"
curl "http://127.0.0.1:7000/stats"
curl "http://127.0.0.1:7000/stats/cache"
curl "http://127.0.0.1:7000/stats/proxy"
curl "http://127.0.0.1:7000/stats/cb"
```

Useful response headers in server mode: `X-Cache`, `X-Fallback-Engine`,`X-Proxy-Mode`, `X-Proxy-Tag`, `X-Proxy-Used`

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).

## 🤝 Contributing

Contributions are welcome. Please feel free to submit a pull request.

## 👾 Issues & Support

If you encounter issues or have questions:

- Open an issue on GitHub
- Check existing issues for similar reports
- Review the documentation and example config

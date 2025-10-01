# OpenSERP (Search Engine Results Page)

![OpenSERP](/logo.svg)

[![Go Report Card](https://goreportcard.com/badge/github.com/karust/openserp)](https://goreportcard.com/report/github.com/karust/openserp)
[![Go Reference](https://pkg.go.dev/badge/github.com/karust/openserp.svg)](https://pkg.go.dev/github.com/karust/openserp)
[![release](https://img.shields.io/github/release-pre/karust/openserp.svg)](https://github.com/karust/openserp/releases)

**OpenSERP** provides free API access to multiple search engines including **[Google, Yandex, Baidu, Bing, DuckDuckGo]**. Get comprehensive search results without expensive API subscriptions!

## Features

- 🔍 **Multi-Engine Support**: Google, Yandex, Baidu, Bing, DuckDuckGo...
- 🌐 **Megasearch**: Aggregate results from multiple engines simultaneously
- 🎯 **Advanced Filtering**: Language, date range, file type, site-specific searches
- 🌍 **Proxy Support**: HTTP/SOCKS5 proxy support
- 🐳 **Docker Ready**: Easy deployment with Docker

## Quick Start⚡️

### Docker (Recommended)

```bash
# Run the API server via prebuilt image
docker run -p 127.0.0.1:7000:7000 -it karust/openserp serve -a 0.0.0.0 -p 7000

# Or use docker-compose
docker compose up --build
```

### From Source

```bash
# Clone and build
git clone https://github.com/karust/openserp.git
cd openserp
go build -o openserp .

# Run the server
./openserp serve
```

## 🌐 Megasearch & Megaimage - Search Everything at Once!

**Megasearch** aggregates results from multiple engines simultaneously with automatic deduplication. **Megaimage** does the same for image searches!

### Megasearch (Web Results)

```bash
# Search ALL engines at once
curl "http://localhost:7000/mega/search?text=golang&limit=10"

# Pick specific engines
curl "http://localhost:7000/mega/search?text=golang&engines=google,bing&limit=5"

# Advanced filtering
curl "http://localhost:7000/mega/search?text=golang&lang=EN&site=github.com&limit=8"
```

### Megaimage (Image Results)

```bash
# Search images across ALL engines
curl "http://localhost:7000/mega/image?text=golang logo&limit=20"

# Pick specific engines for images
curl "http://localhost:7000/mega/image?text=golang&engines=google,bing&limit=15"

# Language-specific image search
curl "http://localhost:7000/mega/image?text=golang&lang=EN&limit=10"
```

### Available Engines

```bash
# Check which engines are available
curl "http://localhost:7000/mega/engines"
```

**Available engines:** `google`, `yandex`, `baidu`, `bing`, `duckduckgo`

## 🔍 Individual Engine APIs

### Search Parameters

| Parameter | Description          | Example                           |
| --------- | -------------------- | --------------------------------- |
| `text`    | Search query         | `golang programming`              |
| `lang`    | Language code        | `EN`, `DE`, `RU`, `ES`            |
| `date`    | Date range           | `20230101..20231231`              |
| `file`    | File extension       | `PDF`, `DOC`, `XLS`               |
| `site`    | Site-specific search | `github.com`, `stackoverflow.com` |
| `limit`   | Number of results    | `10`, `25`, `50`                  |
| `answers` | Include Q&A results  | `true`, `false`                   |

### Individual Engine Examples

```bash
# DuckDuckGo search
curl "http://localhost:7000/duck/search?text=golang&limit=7"

# Google search
curl "http://localhost:7000/google/search?text=golang&lang=EN&limit=10"
```

### Image Search

```bash
# Bing Images
curl "http://localhost:7000/bing/image?text=golang&limit=20"

# Baidu Images
curl "http://localhost:7000/baidu/image?text=golang&limit=15"
```

## 🌐 Proxy Support

OpenSERP supports HTTP and SOCKS5 proxies with authentication:

```bash
# SOCKS5 proxy
./openserp serve --proxy socks5://127.0.0.1:1080

# HTTP proxy with authentication
./openserp search bing "query" --proxy http://user:pass@127.0.0.1:8080
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 👾 Issues & Support

If you encounter any issues or have questions:

- Open an issue on GitHub
- Check existing issues for solutions
- Review the documentation above

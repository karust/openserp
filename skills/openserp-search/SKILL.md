---
name: openserp-search
description: Live web search and page reading through a self-hosted OpenSERP server (Google, Bing, Yandex, Baidu, DuckDuckGo, Ecosia). Use when the answer depends on current information, a specific engine's ranking, or the content of web pages, and the OpenSERP MCP tools or a local OpenSERP API are available.
---

# Web search with OpenSERP

OpenSERP returns real results pages from named engines, with original ranks, plus page content as Markdown. Use the MCP tools (`search`, `mega_search`, `any_search`, `extract`, `batch_extract`, `list_engines`). Without MCP, call the HTTP API on `http://localhost:7000` (or `$OPENSERP_BASE_URL`).

## Workflow

1. **Search one engine first.** `search` with `engine: "google"` for general queries. Use `yandex` for Russian-language, `baidu` for Chinese-language queries. Keep `limit` at 10 unless you need more.
2. **Pick sources by rank and domain.** Prefer official docs, release notes and primary sources over aggregators. Skip `type: "ad"`.
3. **Read with one `batch_extract` call** for the 2-4 URLs you need, not repeated `extract` calls. Avoid archive or changelog index pages: they can be hundreds of KB. Pick the single post or section page instead.
4. **Answer with links.** Cite each claim with the URL you read. Mention the engine and rank when ranking matters (SEO questions).

## When a search fails

- `captcha detected`, `blocked` or `rate_limited` from one engine: do not retry the same engine more than once. Use `any_search` with `engines: ["google", "duckduckgo", "bing"]`, or `search` on another engine.
- Results that do not match the query (unrelated brands, dictionary pages, a single keyword) mean the engine served a degraded page. Treat them as a failure and switch engine; do not use them as evidence.
- An empty result set is not proof that nothing exists. Rephrase once or try another engine.
- In `batch_extract`, an item with `metadata.error` failed; the others are still valid.

## HTTP fallback

```bash
curl "http://localhost:7000/google/search?text=QUERY&limit=10"
curl "http://localhost:7000/mega/search?text=QUERY&engines=google,duckduckgo&mode=any"
curl -X POST "http://localhost:7000/extract/batch" -H "Content-Type: application/json" \
  -d '{"urls":["https://example.com/a","https://example.com/b"],"mode":"auto"}'
```

`extract=N` adds Markdown for up to N organic results (1-5) in one call. Ads and answer rows are skipped.

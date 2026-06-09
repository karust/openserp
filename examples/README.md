# OpenSERP Examples

Short, copy-pasteable examples for using OpenSERP from JavaScript and Python.
Each one runs against a local server by default and works the same against the
[hosted API](#using-the-hosted-api) by swapping in an API key.

## Start a server

Most examples expect OpenSERP running on `http://localhost:7000`.

```bash
# Docker
docker run -p 127.0.0.1:7000:7000 -it karust/openserp serve -a 0.0.0.0 -p 7000

# Or from source
go build -o openserp . && ./openserp serve
```

Prefer not to run a server? Skip ahead to [Using the hosted API](#using-the-hosted-api).

## Try it without code

A search is a single HTTP GET, so you can check the server with `curl`:

```bash
# One engine
curl "http://localhost:7000/google/search?text=open+source+search+api&limit=10"

# Several engines at once
curl "http://localhost:7000/mega/search?text=open+source+search+api&engines=bing,duckduckgo&limit=10"
```

## SDKs and integrations

| Tool | Package | Install |
| --- | --- | --- |
| JavaScript / TypeScript | [`@openserp/sdk`](https://www.npmjs.com/package/@openserp/sdk) | `npm install @openserp/sdk` |
| Python | [`openserp`](https://pypi.org/project/openserp/) | `pip install openserp` |
| MCP server (AI agents) | [`@openserp/mcp`](https://www.npmjs.com/package/@openserp/mcp) | `npx @openserp/mcp` |
| n8n community node | [`@openserp/n8n-nodes-openserp`](https://www.npmjs.com/package/@openserp/n8n-nodes-openserp) | Install via n8n community nodes |

## Examples by question

### Getting started

- **How do I run a search from code?** — [JavaScript](quickstart/js-basic-search) · [Python](quickstart/python-basic-search)
- **How do I compare results across several engines?** — [JavaScript](search/js-multi-engine-compare)
- **How do I search a list of keywords and export them?** — [Python](search/python-keyword-csv)

### AI and LLM grounding

- **How do I ground an LLM answer in fresh search results?** — [JavaScript](ai/js-rag-context-builder)
- **How do I give a search tool to an agent?** — [Python](ai/python-agent-tool)
- **I want this inside Claude, Cursor, or another MCP client.** — [`@openserp/mcp`](https://www.npmjs.com/package/@openserp/mcp)

### SEO

- **Who are my real competitors for a set of keywords?** — [JavaScript](seo/js-competitor-overlap)
- **Where does my domain rank for each keyword?** — [Python](seo/python-rank-tracker)

### Content extraction

- **How do I search and read the page content in one call?** — [JavaScript](content/js-search-with-extract)
- **How do I turn a URL into clean Markdown?** — [Python](content/python-extract-markdown)

### Images

- **How do I search images and preview them?** — [JavaScript](media/js-image-gallery)

### Automation

- **I want to wire OpenSERP into a no-code workflow.** — [`@openserp/n8n-nodes-openserp`](https://www.npmjs.com/package/@openserp/n8n-nodes-openserp)

## Using the hosted API

Every example points at a local server. To use the managed API instead, get a key
from [openserp.org/dashboard/keys](https://openserp.org/dashboard/keys) and construct
the client with it — no base URL needed:

```js
const client = new OpenSERP({ apiKey: "<YOUR_API_TOKEN>" });
```

```python
client = OpenSERP(api_key="<YOUR_API_TOKEN>")
```

The endpoints and response shape are identical, so example code moves between
self-hosted and hosted by changing only that one line. Set **either** `baseUrl`
(self-hosted) **or** `apiKey` (hosted) — not both.

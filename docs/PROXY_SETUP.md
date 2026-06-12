# Proxy Setup

This document is for the **backend engineer building the balancer** in front of OpenSERP. It explains the request/response contract for the SaaS `X-Proxy-URL` path, what each header does, what errors mean, and how to size and operate workers.

If you only need a self-contained OpenSERP with locally configured proxies, scroll to [Configured Proxies (no balancer)](#configured-proxies-no-balancer).

## Architecture

```text
client ─▶ your balancer ─▶ OpenSERP worker ─▶ search engine
            owns:                applies:
              - proxy provider     - the proxy you supply
              - country/class      - sticky cookies/profile per session
              - session minting    - cache keyed by market metadata
              - rotation policy    - stable typed errors
              - usage accounting
```

The balancer is the source of truth for _which_ proxy to use and _when_ to rotate. OpenSERP is stateless w.r.t. provider choice — it just executes the search through whatever proxy URL you hand it and reports back what happened.

## Worker Configuration

Enable the request-proxy-URL path on every worker fronted by your balancer:

```yaml
proxies:
  allow_request_proxy_url: true # required for X-Proxy-URL to be honored
  lanes:
    enabled: true
    max_lanes: 100 # LRU cap on sticky lanes per worker
    drop_cookies_on_challenge: true

app:
  max_processes: 4 # LRU cap on Chrome processes per worker
  idle_ttl: 10m # close a Chrome that has not served traffic for this long
```

Worker rejects `X-Proxy-URL` with `400 bad_request` (`reason=REQUEST_PROXY_URL_DISABLED`) when the flag is off. Keep this off on any worker reachable by untrusted clients.

CORS already lists every header the balancer sends; if you customise `cors.allow_headers`, keep `X-Proxy-URL, X-Proxy-Country, X-Proxy-Class, X-Proxy-Provider, X-Proxy-Session-ID, X-Tenant, X-Use-Proxy, X-Request-ID`.

## Request Contract

### Headers your balancer sends

| Header                | Required | Purpose                                                                                                                                                  |
| --------------------- | :------: | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `X-Proxy-URL`         |    ✅    | The actual proxy URL to route this request through. `http://`, `https://`, or unauthenticated `socks5://`/`socks5h://`. Authenticated SOCKS is rejected. |
| `X-Proxy-Country`     |    ⚠️    | Two-letter market code (`us`, `de`). **Cache key**. Without it, proxied responses bypass cache.                                                          |
| `X-Proxy-Class`       |    ⚠️    | `datacenter`, `residential`, `mobile`, etc. **Cache key**.                                                                                               |
| `X-Proxy-Provider`    |    ⚠️    | Provider id (`webshare`, `brightdata`, `internal`). **Cache key**.                                                                                       |
| `X-Proxy-Session-ID`  |    🟢    | Sticky session id. **Lane key** — same id reuses cookies and profile. Rotate to get a clean lane.                                                        |
| `X-Tenant`            |    🟢    | Multi-tenant scope. Lanes become `tenant + engine + session_id`.                                                                                         |
| `X-Use-Proxy: direct` |    🟢    | Force-disable proxy for this request, even if `X-Proxy-URL` is set.                                                                                      |
| `X-Request-ID`        |    🟢    | Pass-through correlation id (UUID v7 generated if absent).                                                                                               |

✅ required, ⚠️ recommended (cache won't engage without it), 🟢 optional.

### Precedence

1. `X-Use-Proxy: direct`
2. `X-Proxy-URL` (when allowed)
3. `X-Use-Proxy: <tag>`
4. Per-engine configured tag (worker config)
5. `proxies.global` (worker config)
6. Direct (no proxy)

### Endpoints

| Path                   | What                                                       |
| ---------------------- | ---------------------------------------------------------- |
| `GET /{engine}/search` | Single engine: `google`, `yandex`, `baidu`, `bing`, `duck` |
| `GET /{engine}/image`  | Single engine image search                                 |
| `GET /mega/search`     | Parallel across all (or `?engines=...`) engines            |
| `GET /mega/image`      | Parallel image search                                      |
| `GET /stats/proxy`     | Pool, lane, and Chrome-process stats                       |
| `GET /health`          | Engine + circuit-breaker health                            |
| `GET /ready`           | `503 draining` during graceful shutdown                    |

Full schema at `/openapi.yaml` and Swagger UI at `/docs`.

### Example call

```bash
curl -G \
  -H "X-Proxy-URL: http://USER:PASS@proxy.example:8080" \
  -H "X-Proxy-Country: us" \
  -H "X-Proxy-Class: residential" \
  -H "X-Proxy-Provider: webshare" \
  -H "X-Proxy-Session-ID: sid-abc" \
  --data-urlencode "text=golang" \
  --data-urlencode "lang=EN" \
  --data-urlencode "limit=10" \
  http://worker.internal:7000/google/search
```

## Response Contract

Every response carries:

```text
X-Request-ID:   01HXYZ...                       # mirrors meta.request_id
X-Proxy-Mode:   off | tag_pool | request_url    # what actually ran
X-Proxy-Used:   direct | http://proxy.example:8080 | multiple | mixed | pooled
X-Proxy-Tag:    us                              # only when X-Proxy-Mode=tag_pool
X-Cache:        HIT | MISS | BYPASS             # only when cache is enabled
```

A balancer-driven request always sees `X-Proxy-Mode: request_url` (unless overridden via `X-Use-Proxy: direct`). `X-Proxy-Used` is the masked `scheme://host:port` of the proxy you sent — credentials are stripped. Use these headers to confirm OpenSERP actually applied your proxy and didn't quietly fall back.

Successful body is the v1 envelope (`/openapi.yaml#/components/schemas/SearchEnvelope`).

## Error Playbook

All errors are JSON of shape:

```json
{
  "error": "<stable code>",
  "code": 503,
  "message": "<human readable>",
  "reason": "<sub-code, only on 400>",
  "meta": {
    "engine": "google",
    "proxy_used": "http://proxy.example:8080",
    "proxy_country": "us",
    "proxy_class": "residential",
    "proxy_provider": "webshare",
    "proxy_session_id": "sid-abc"
  }
}
```

Credentials are **never** present in `meta.proxy_used`, response headers, logs, or stats.

| HTTP | `error`                                      | What it means                                                                                  | Balancer action                                            |
| ---: | -------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
|  400 | `bad_request`                                | Bad client input. See `reason`.                                                                | Don't retry. Surface to caller.                            |
|  400 | `bad_request` (`REQUEST_PROXY_URL_DISABLED`) | Worker has the feature off.                                                                    | Misconfigured worker. Page oncall.                         |
|  400 | `bad_request` (`UNSUPPORTED_PROXY_SCHEME`)   | Authenticated SOCKS in browser mode.                                                           | Don't send authenticated SOCKS. Use HTTP/HTTPS.            |
|  403 | `blocked`                                    | The search engine returned 403 to the proxy.                                                   | Rotate session id. Maybe rotate proxy.                     |
|  429 | `captcha_detected`                           | Captcha challenge page. Worker has dropped this lane's cookies.                                | Rotate session id. Cool the proxy.                         |
|  429 | `rate_limited`                               | Engine returned 429.                                                                           | Slow the proxy. Rotate session id.                         |
|  502 | `parser_failure`                             | SERP parser drift on a successfully fetched page.                                              | Don't blame the proxy. Page oncall — engine update needed. |
|  502 | `engine_internal`                            | Engine panic recovered, or all engines failed (mega).                                          | Retry once on a different worker.                          |
|  503 | `proxy_connect`                              | TCP/TLS to the proxy failed.                                                                   | Mark proxy bad. Retry on another proxy.                    |
|  503 | `proxy_auth`                                 | 407 Proxy Auth Required.                                                                       | Credentials wrong/expired. Don't retry the same one.       |
|  503 | `proxy_timeout`                              | Network timeout on the proxy path.                                                             | Retry on another proxy.                                    |
|  503 | `proxy_unavailable`                          | No healthy proxy left in the configured tag pool (worker-side, not your concern in SaaS mode). | Worker config issue.                                       |
|  504 | `search_timeout`                             | Required SERP elements never appeared before timeout.                                          | Retry once. If repeated, page oncall.                      |

### Decision rules for the balancer

- **Rotate session id** (mint a new `X-Proxy-Session-ID`) on `captcha_detected`, `blocked`, `rate_limited`. The lane keeps its profile but cookies are dropped on captcha; a new id starts fresh anyway.
- **Mark proxy bad** on `proxy_connect`, `proxy_auth`, `proxy_timeout`. Don't degrade the proxy on captcha/parser/engine errors — those aren't the proxy's fault.
- **Don't retry** on 400. Those are client bugs.

## Sticky Lanes

A **lane** is a (tenant, engine, session) tuple owned by one worker.

```yaml
proxies:
  lanes:
    enabled: true
    max_lanes: 100
    drop_cookies_on_challenge: true
```

What a lane holds:

- The browser **profile** picked at first use (UA, viewport, languages, UA-CH brand list).
- The **cookies** harvested during navigation.

Lane key:

- `tenant + engine + session_id` when `X-Tenant` is present.
- `engine + session_id` otherwise.
- If `X-Proxy-Session-ID` is missing, the lane id is derived from `sha256(host:port|username)[:16]`. The password is never part of the key, so rotating credentials on the same proxy keeps the lane.

What invalidates a lane:

- `X-Proxy-Session-ID` change → fresh lane (the old one stays warm until LRU evicts).
- Captcha response → cookies dropped, profile retained (when `drop_cookies_on_challenge: true`).
- `max_lanes` LRU eviction.

What does **not** invalidate a lane:

- Block, rate-limit, parser, engine, or proxy-network errors. The balancer decides whether to rotate.

If your balancer issues a sticky-session credential pattern (e.g. Bright Data `session-SID`), reuse the same `X-Proxy-Session-ID` for the same upstream sticky window. When you rotate, change both the proxy URL session token AND the `X-Proxy-Session-ID` in the same step.

## Cache

Cache key includes query fields + `country + class + provider`. It does **not** include the proxy URL, username, password, or session id.

Behavior with proxied requests:

- All three of country/class/provider missing → request bypasses cache (`X-Cache: BYPASS`).
- Country missing but `lang` present → falls back to `lang` as a weak market hint.
- Cross-engine fallback responses (`X-Fallback-Engine` set) are not cached.

Translation: **send country/class/provider on every proxied request** if you want cache hits.

## Browser Process Pool

OpenSERP keeps one Chrome process per _authenticated proxy identity_ (`scheme + host + port + username`):

| Upstream proxy                                  | Chrome used                                  |
| ----------------------------------------------- | -------------------------------------------- |
| `http://userA:passA@proxy:8080`                 | Dedicated Chrome `[http\|proxy:8080\|userA]` |
| `http://userA:passB@proxy:8080` (rotated pass)  | Same Chrome (password ignored in key)        |
| `http://userB:pass@proxy:8080` (different user) | New dedicated Chrome                         |
| `socks5://proxy:1080` (unauthenticated)         | Shared "no-auth" Chrome, per-context proxy   |
| no proxy / direct                               | Shared "no-auth" Chrome                      |

Why dedicated processes for authenticated HTTP proxies: Chrome's per-`BrowserContext` auth callback is process-global and only answers the _next_ pending challenge — so concurrent requests with different credentials would race and subresources hang. Launching Chrome with `--proxy-server=...` per identity lets the OS-level Chrome auth path handle 407s natively for the main document AND every subresource.

The pool grows lazily and is bounded by `app.max_processes` with LRU eviction, plus an idle sweeper that closes Chromes idle for `app.idle_ttl`. **One Chrome serves many concurrent requests** via per-page `BrowserContext` isolation — `max_processes` does NOT bound concurrent search requests, only the number of distinct authenticated proxy identities a worker can keep warm.

### Sizing the pool

If you expect `N` distinct authenticated identities to hit one worker concurrently, set `max_processes ≥ N`. Below that, the pool LRU-closes Chromes mid-burst, which adds Chrome-startup latency to the next request that needs the evicted identity.

Rough memory budget: each Chrome ≈ 150-300 MiB resident. `max_processes: 4` → plan for ~1 GiB of Chrome RAM per worker, plus the rest of the Go process.

`/debug/fingerprint-check` (when `app.debug_endpoints: true`) is intentionally **not** pooled — it spawns a fresh Chrome per call so you can verify each profile in isolation.

## Stats Endpoint

`GET /stats/proxy` returns:

```json
{
  "configured_count": 0,
  "healthy_count": 0,
  "unhealthy_count": 0,
  "request_proxy_url_enabled": true,
  "lanes": {
    "active": 12,
    "evicted_lru": 7,
    "cookies_dropped": 20
  },
  "browser_processes": {
    "active": 3,
    "max": 4,
    "evicted_lru": 12,
    "evicted_idle": 5
  },
  "tags": {},
  "entries": []
}
```

Watch for:

- `browser_processes.evicted_lru` rising → bump `max_processes`.
- `browser_processes.evicted_idle` rising while `active` stays low → traffic is bursty; that's healthy.
- `lanes.cookies_dropped` rising → the proxy is hitting captchas; consider rotating the session more aggressively.

## Provider Examples

Bright Data (residential, sticky session):

```text
X-Proxy-URL:        http://brd-customer-CUSTOMER-zone-res-country-us-session-SID:PASS@brd.superproxy.io:22225
X-Proxy-Provider:   brightdata
X-Proxy-Class:      residential
X-Proxy-Country:    us
X-Proxy-Session-ID: SID
```

Webshare:

```text
X-Proxy-URL:      http://USER:PASS@p.webshare.io:80
X-Proxy-Provider: webshare
X-Proxy-Class:    datacenter
X-Proxy-Country:  us
```

Internal HTTP datacenter:

```text
X-Proxy-URL:      http://user:pass@dc-proxy.example:8080
X-Proxy-Provider: internal
X-Proxy-Class:    datacenter
X-Proxy-Country:  de
```

Authenticated SOCKS proxies are rejected by browser mode because Chrome cannot safely answer SOCKS auth challenges via the DevTools auth callback. Unauthenticated `socks5://` and `socks5h://` are accepted on both `X-Proxy-URL` and configured pools.

## Configured Proxies (no balancer)

For OSS and local deployments without a balancer, OpenSERP can manage proxies directly:

```yaml
proxies:
  global: http://user:pass@127.0.0.1:8080  # one proxy for everything

# OR a tagged pool:
proxies:
  entries:
    - url: http://user:pass@proxy-us.example:8080
      tags: [default, us]
    - url: socks5h://127.0.0.1:1080
      tags: [eu]
  health:
    failure_threshold: 3   # disable a proxy after N consecutive network errors

google:
  proxy: default           # opt this engine into the "default" tag pool
```

Per-request override (no balancer needed):

```bash
curl -H "X-Use-Proxy: us"     "http://127.0.0.1:7000/google/search?text=golang"
curl -H "X-Use-Proxy: direct" "http://127.0.0.1:7000/google/search?text=golang"
```

Pool health: a tag pool that exhausts (every member disabled) goes into a 5-minute quarantine. After quarantine, one proxy is re-enabled as a recovery probe. Network errors degrade health; captcha/parser/engine errors do not.

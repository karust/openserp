# Adding a Search Engine

This is the short checklist for adding a new engine. Keep the first PR small:
web search, deterministic parser tests, and registration. Add images or advanced
parameters in follow-up PRs.

## 1. Create the engine package

Use an existing engine package as the template. A complete engine normally has:

- `url.go` - pure URL builders that reject an empty query.
- `selectors.go` - stable selectors shared by browser mode, raw mode, and parser tests.
- `parse_html.go` - `ParseHTML(io.Reader)` using goquery.
- `search.go` - browser-mode implementation using `core.Browser`.
- `search_raw.go` - optional raw HTTP implementation.
- `features.go` - optional SERP feature extraction.
- `*_test.go` and `testdata/` - URL and parser fixtures.

Prefer stable data attributes over generated CSS classes. When a selector is
fragile, add two or three explicit fallbacks in `selectors.go`.

## 2. Implement the search contract

Browser engines implement `core.SearchEngine`:

- `Search(context.Context, core.Query) ([]core.SearchResult, error)`
- `SearchImage(context.Context, core.Query) ([]core.SearchResult, error)`
- `IsInitialized() bool`
- `Name() string`
- `GetRateLimiter() *rate.Limiter`

Raw engines should expose a `ParseHTML(io.Reader)` path so tests and
`POST /{engine}/parse` can use the same parser.

## 3. Register the engine

Update:

- `cmd/serve.go` for server wiring.
- CLI search dispatch when the engine is CLI-visible.
- `config.yaml` with rate limits and optional proxy tag.
- `README.md` and `docs/openapi.yaml` when public endpoints or parameters change.

## 4. Add tests

Required for the first PR:

- Table-driven URL builder tests.
- Parser tests using small sanitized HTML fixtures.
- Integration tests only when needed, gated with `testutil.RequireIntegration(t)`.

Default tests must pass without browser or network access:

```bash
make test
```

Run live/browser checks only for engine behavior:

```bash
make test-integration
```

## 5. Open the PR

Before opening the PR:

```bash
make fmt
make lint
make test
```

Keep the PR focused. A good first engine PR should not refactor shared browser,
server, or response-envelope behavior unless the engine cannot work without it.

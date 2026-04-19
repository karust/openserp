# Contributing to OpenSERP

## Development Setup

### Prerequisites

- Go 1.24+
- Chromium/Chrome (only required for browser-mode work and integration tests)
- Optional: Docker

### Clone, build, run

```bash
git clone https://github.com/karust/openserp.git
cd openserp
go build -o openserp .
./openserp serve
```

### Test commands

Unit tests (default, no browser/network assumptions):

```bash
go test -race ./...
```

Integration tests (explicitly enabled):

```bash
OPENSERP_INTEGRATION_TESTS=1 go test -race -timeout=120s ./...
```

Notes:

- Integration tests are gated by `testutil.RequireIntegration(t)`.
- Do not create browser instances in `init()` or package-level variables.

## Adding a New Search Engine

### 1) Create engine package

Create a new folder (example: `myengine/`) with:

- `myengine/url.go` (`BuildURL`, and `BuildImageURL` when image support exists)
- `myengine/search.go` (browser mode implementation)
- `myengine/search_raw.go` (optional raw mode implementation)

### 2) Implement `core.SearchEngine`

Your engine type must implement:

- `Search(core.Query) ([]core.SearchResult, error)`
- `SearchImage(core.Query) ([]core.SearchResult, error)`
- `IsInitialized() bool`
- `Name() string`
- `GetRateLimiter() *rate.Limiter`

Use the existing engines (for example `google/`) as the reference pattern.

### 3) Register the engine in server wiring

Update [`cmd/serve.go`](cmd/serve.go):

- Add engine spec in `browserEngineSpecs()`
- Add raw-mode handling if raw support exists

### 4) Add config block

Update [`config.yaml`](config.yaml) with your engine section:

- `rate_requests`
- `rate_burst`
- optional `proxy` tag
- optional engine-specific fields

### 5) Add tests

- URL builder tests (table-driven)
- Parser tests (prefer deterministic fixtures in `testdata/`)
- Integration tests guarded by `testutil.RequireIntegration(t)`

## Code Style and Quality Checks

Run these before opening a PR:

```bash
gofmt -w .
go vet ./...
golangci-lint run
go test -race ./...
```

Guidelines:

- Return `error` values instead of panicking in library code.
- Reuse existing patterns in `core/` and existing engines.
- Add comments only for non-obvious decisions (why, not what).

## Test Categories

- Unit tests: deterministic tests that run with `go test ./...` and do not require browser/network.
- Integration tests: live/browser/network dependent tests gated by `OPENSERP_INTEGRATION_TESTS=1`.

When adding tests, keep unit and integration behavior clearly separated.

## Pull Request Process

For each PR:

1. Describe what changed and why.
2. Link the related issue (if available).
3. Include or update tests for behavior changes.
4. Include updated docs when API/config/contracts change.

If you change API behavior, update:

- [`docs/openapi.yaml`](docs/openapi.yaml)
- [`README.md`](README.md)
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) when flow/design changes

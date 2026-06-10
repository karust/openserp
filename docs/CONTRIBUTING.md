# Contributing to OpenSERP

## Fastest Path to a Merged PR

1. Pick a focused issue, ideally one labeled `good first issue`.
2. Comment on the issue with the approach you plan to take.
3. Keep the PR narrow: one bug, one parser fallback, one docs page, or one engine step.
4. Run `make fmt`, `make lint`, and `make test` before opening the PR.
5. Explain what changed, why it changed, and how you tested it.

Good first issues are curated in [`GOOD_FIRST_ISSUES.md`](https://github.com/karust/openserp/blob/main/docs/GOOD_FIRST_ISSUES.md).
New engine work should start with [`ADDING_AN_ENGINE.md`](https://github.com/karust/openserp/blob/main/docs/ADDING_AN_ENGINE.md).

## Development Setup

### Prerequisites

- Go 1.24+
- Chromium/Chrome (only required for browser-mode work and integration tests)
- Optional: Docker
- Optional: `make` and `golangci-lint` for the same local workflow used by CI

### Clone, build, run

```bash
git clone https://github.com/karust/openserp.git
cd openserp
make build
make run
```

### Test commands

Unit tests (default, no browser/network assumptions):

```bash
make test
```

Integration tests (explicitly enabled):

```bash
make test-integration
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

- `Search(context.Context, core.Query) ([]core.SearchResult, error)`
- `SearchImage(context.Context, core.Query) ([]core.SearchResult, error)`
- `IsInitialized() bool`
- `Name() string`
- `GetRateLimiter() *rate.Limiter`

Use the existing engines (for example `google/`) as the reference pattern.

### 3) Register the engine in server wiring

Update [`cmd/serve.go`](../cmd/serve.go):

- Add engine spec in `browserEngineSpecs()`
- Add raw-mode handling if raw support exists

### 4) Add config block

Update [`config.yaml`](../config.yaml) with your engine section:

- `rate_requests`
- `rate_burst`
- optional `proxy` tag
- optional engine-specific fields

### 5) Add tests

- URL builder tests (table-driven)
- Parser tests (prefer deterministic fixtures in `testdata/`)
- Integration tests guarded by `testutil.RequireIntegration(t)`

See [`ADDING_AN_ENGINE.md`](https://github.com/karust/openserp/blob/main/docs/ADDING_AN_ENGINE.md) for the full checklist.

## Code Style and Quality Checks

Run these before opening a PR:

```bash
make fmt
make lint
make test
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

- [`openapi.yaml`](openapi.yaml)
- [`../README.md`](../README.md)
- [`ARCHITECTURE.md`](ARCHITECTURE.md) when flow/design changes

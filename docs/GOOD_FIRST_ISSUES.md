# Good First Issue Backlog

Small, contributor-friendly tasks curated from the OSS roadmap. The first section
tracks issues that are **already filed** on GitHub with the `good first issue`
label. The second section is a backlog of vetted ideas that are **not yet filed** —
open one (or ask a maintainer to) before starting work so effort isn't duplicated.

New contributor? Read [`CONTRIBUTING.md`](CONTRIBUTING.md) first; new engines start
with [`ADDING_AN_ENGINE.md`](ADDING_AN_ENGINE.md).

## Open starter issues

### [#27 Add CLI validation tests for supported engines](https://github.com/karust/openserp/issues/27)

Area: CLI, tests

Make the unknown-engine error list valid engine names, then add a table test for
engine dispatch in both modes: browser mode accepts all six engines (`google`,
`yandex`, `baidu`, `bing`, `duckduckgo`, `ecosia`); raw mode accepts
`google/yandex/baidu/ecosia` and rejects `bing`/`duckduckgo` with a clear message.
Both dispatch switches live in [`cmd/search.go`](../cmd/search.go).

### [#30 Document raw-mode support per engine](https://github.com/karust/openserp/issues/30)

Area: docs

Add a small table showing which engines support browser mode, raw mode, and
`/{engine}/parse`. The table must match current code; link it from the README
search-endpoint section. Start in `cmd/serve.go` and `README.md`.

### [#31 Add release smoke-check script stub](https://github.com/karust/openserp/issues/31)

Area: release tooling

Add a script under `scripts/` (e.g. `scripts/smoke-check.sh`) that builds the
binary, starts the server, polls `/health` until ready, then shuts down and exits
cleanly. It must fail fast with a non-zero exit when the server does not become
healthy. Docker and `go install` checks are follow-ups. Document it in
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Backlog

These are good candidates from the roadmap but have **no GitHub issue yet**. File
one before starting.

### Draft a Brave Search engine skeleton

Area: new engine

Create a non-registered `brave/` package skeleton with URL builder table tests and
a minimal parser fixture (title, URL, snippet). Leave live browser search for a
follow-up. Do not expose the engine in README or API docs until browser search
works. See [`ADDING_AN_ENGINE.md`](ADDING_AN_ENGINE.md).

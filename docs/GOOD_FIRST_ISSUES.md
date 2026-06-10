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

Add a table test verifying every supported engine name is accepted by the CLI and
an unknown engine returns a deterministic, user-friendly error listing valid
engines. Covers `google`, `yandex`, `baidu`, `bing`, `duckduckgo`, `ecosia`.
Start in [`cmd/search.go`](../cmd/search.go).

### [#28 Add Yandex parser fallback micro-fixtures](https://github.com/karust/openserp/issues/28)

Area: parser tests

Add compact Yandex HTML fixtures covering link, title, and snippet fallback
selectors without depending on full saved result pages. No browser or network.
Start in `yandex/selectors.go` and `yandex/parse_html.go`.

### [#29 Add Baidu parser fallback micro-fixtures](https://github.com/karust/openserp/issues/29)

Area: parser tests

Add compact Baidu HTML fixtures for title, URL, and description fallback paths.
Missing optional fields must not panic; result order stays stable. No browser or
network. Start in `baidu/selectors.go` and `baidu/parse_html.go`.

### [#30 Document raw-mode support per engine](https://github.com/karust/openserp/issues/30)

Area: docs

Add a small table showing which engines support browser mode, raw mode, and
`/{engine}/parse`. The table must match current code; link it from the README
search-endpoint section. Start in `cmd/serve.go` and `README.md`.

### [#31 Add release smoke-check script stub](https://github.com/karust/openserp/issues/31)

Area: release tooling

Add a script under `.release/` that builds the binary, starts the server, checks
`/health`, and exits cleanly. It must fail fast with a useful error when the
server does not become healthy. Docker and `go install` checks are follow-ups.
Document it in [`.release/build.md`](../.release/build.md).

## Backlog

These are good candidates from the roadmap but have **no GitHub issue yet**. File
one before starting.

### Draft a Brave Search engine skeleton

Area: new engine

Create a non-registered `brave/` package skeleton with URL builder table tests and
a minimal parser fixture (title, URL, snippet). Leave live browser search for a
follow-up. Do not expose the engine in README or API docs until browser search
works. See [`ADDING_AN_ENGINE.md`](ADDING_AN_ENGINE.md).

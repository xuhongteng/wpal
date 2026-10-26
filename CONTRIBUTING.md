# Contributing to Wpal

Thanks for your interest in improving Wpal! This document explains how to
propose changes, and what we expect from a pull request.

[简体中文](CONTRIBUTING.zh-CN.md)

---

## Quick start

```bash
git clone https://github.com/wpal/wpal.git
cd wpal
make build      # compile to ./bin/wpal
make test       # unit tests
make help       # all targets
```

You need Go 1.23 or newer. No other toolchain is required to build or test.

---

## Ground rules

Wpal's whole point is being **small and dependency-light**. When proposing
a change, please keep these budgets in mind:

| Budget | Limit | Enforced by |
|---|---|---|
| Binary size | ≤ 12 MB per platform | `make cross` + CI size check |
| Idle RSS | ≤ 12 MiB | `make verify` |
| Runtime dependencies | none | no cgo, no external DB |

A pull request that pushes the binary over the size budget will not be merged
without a very good reason.

---

## Development workflow

1. **Open an issue first** for anything beyond a trivial fix, so we can agree on
   the approach before you spend time on it.
2. **Branch** from `main`: `feat/short-description`, `fix/short-description`.
3. **Keep commits focused.** One logical change per commit.
4. **Sign off** your commits (DCO): `git commit -s`.
5. **Open a pull request** and fill in the template.

### Before you push

```bash
make fmt      # gofmt
make vet      # go vet
make lint     # golangci-lint (if installed)
make test     # unit tests with -race
make cross    # all target platforms still build
```

### Verifying the dashboard still behaves

The repository ships two verification scripts that compare the Go implementation
against a reference implementation:

```bash
make verify-a1   # field-by-field comparison of collected metrics
make verify-a2   # the same web UI against two backends, pixel-compared
make verify-a7   # i18n key completeness and language switching
```

`make verify` runs all three. Please run them if you touch the collector, the
JSON contract or the frontend.

---

## Changing the JSON contract

`/api/snapshot`, `/api/history` and `/api/events` are public API. The rules:

- **Additive changes only.** New fields are fine; renaming or removing a field
  breaks other people's scripts.
- Field semantics must match the documented ones (see `internal/collector/collector.go`,
  which is the single source of truth).
- Update `tools/compare.py` if a field's meaning changes.

---

## Adding a platform-specific metric

Metrics that a platform cannot provide must be reported as `0`, never omitted
and never allowed to fail the whole snapshot. Implement the platform variant in
`internal/collector/` behind a build tag and document the difference in
`docs/CONFIGURATION.md`.

---

## Code style

- Idiomatic Go; run `gofmt`.
- Comments explain *why*, not *what*.
- Keep exported identifiers documented.
- No new runtime dependencies without discussion — the dependency budget is
  part of the product's promise.

---

## Reporting bugs and security issues

- **Bugs** → [issue templates](.github/ISSUE_TEMPLATE/)
- **Security issues** → do **not** open a public issue. Follow
  [SECURITY.md](SECURITY.md).

---

## Code of Conduct

Participation in this project is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md). By contributing, you agree to uphold it.

---

## License

Contributions are licensed under the [Apache License 2.0](LICENSE), the same
license as the project itself.

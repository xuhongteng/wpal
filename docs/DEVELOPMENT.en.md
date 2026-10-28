# Development

[中文](DEVELOPMENT.md) · [English](DEVELOPMENT.en.md)

---

## Layout

```
cmd/wpal/        CLI entry point: flag parsing, wiring, graceful shutdown
internal/
  collector/          Metric collection. collector.go is the JSON contract.
    linux.go            Linux: reads /proc directly
    other.go            macOS / Windows: gopsutil
    util.go             Shared helpers (no build tag)
  config/             YAML + env + flag configuration
  history/            Bounded ring file (ring.go) and event log (events.go)
  alerts/             Threshold state machine (engine.go) and rules (rules.go)
  sampler/            Sampling loop, persistence and broadcast
  server/             HTTP routes, WebSocket hub, static assets
  i18n/               Backend-side fallback texts
  version/            Version info (injected via ldflags)
web/                  Frontend (embedded into the binary)
  locales/             zh-CN.json / en-US.json
  embed.go             go:embed directive
packaging/            systemd, launchd, Windows, deb, rpm, install script
tools/                Verification scripts
```

---

## Build environment

> `/tmp` on many CI images is a small tmpfs. Keep the Go caches under `$HOME`.

```bash
export GOROOT="$HOME/sdk/go"
export GOPATH="$HOME/go"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$GOPATH/.cache/go-build"
export GOTMPDIR="$GOPATH/tmp"
export GOPROXY="https://goproxy.cn,direct"
export CGO_ENABLED=0
```

`make` already exports these; `scripts/env.sh` has the same list for manual use.

---

## Common commands

```bash
make build       # ./bin/wpal
make run         # run with ./data as the data directory
make test        # unit tests with -race
make vet         # go vet
make lint        # golangci-lint (skipped if not installed)
make fmt         # gofmt -w
make cross       # cross-compile every target into dist/
make release     # goreleaser: packages, checksums, SBOM
make clean
```

---

## The JSON contract

`internal/collector/collector.go` defines every field that reaches the browser.
Rules for changing it:

- **Additive only.** New fields are fine; renaming or removing one breaks
  consumers.
- Field semantics are documented there and mirrored in the Python reference
  implementation used by `tools/compare.py`.

---

## Verification

The repository keeps two implementations in sync: the Go one and a Python
reference used by the comparison scripts.

| Script | What it proves |
|---|---|
| `tools/compare.py` | 30+ collected fields match the reference implementation |
| `tools/verify_web.py` | DOM assertions + screenshot for a given backend |
| `tools/compare_shots.py` | the same UI renders identically against both backends |
| `tools/verify_i18n.py` | translation keys are complete and language switching works |

```bash
make verify       # all of the above
make verify-a1    # just the field comparison
```

`compare.py` samples both sides for ~10 s and compares windowed means for
rate-like values, because two independent samples taken at different moments
differ for reasons that have nothing to do with correctness.

---

## Writing a collector

1. Add the field to `internal/collector/collector.go`.
2. Implement it in `linux.go` and, if the platform supports it, in `other.go`.
   Platforms that cannot provide a value must report `0` — never omit the
   field and never fail the snapshot.
3. Add a comparison rule to `tools/compare.py`.
4. Document the platform difference in `docs/CONFIGURATION.md`.

---

## Writing a translation

1. Add the key to **both** `web/locales/zh-CN.json` and `web/locales/en-US.json`.
   `tools/verify_i18n.py` fails if the key sets diverge.
2. Use it in the frontend with `t('key', {param: value})`.
3. Backend-side fallback texts live in `internal/i18n/i18n.go`; the backend also
   emits `label_key` / `detail_key` so the frontend can translate them.

---

## Release process

1. Update `CHANGELOG.md`.
2. Tag: `git tag -a v1.1.0 -m "v1.1.0" && git push --tags`.
3. The `Release` workflow builds every platform, publishes the packages with
   checksums and an SBOM, and pushes the container image.
4. Verify the artifacts locally first with `goreleaser release --snapshot --clean`.

---

## Budgets

| Budget | Limit |
|---|---|
| Binary size | ≤ 12 MB per platform |
| Idle RSS | ≤ 12 MiB |
| Runtime dependencies | none |
| New Go dependencies | discuss in an issue first |

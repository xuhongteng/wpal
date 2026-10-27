<div align="center">

# Wpal

**Lightweight, cross-platform server resource monitoring — one static binary, no runtime dependencies.**

[![Go](https://img.shields.io/badge/go-1.23%2B-00ADD8E?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![CI](https://github.com/xuhongteng/wpal/actions/workflows/ci.yml/badge.svg)](https://github.com/xuhongteng/wpal/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/xuhongteng/wpal)](https://goreportcard.com/report/github.com/xuhongteng/wpal)
[![Security](https://img.shields.io/badge/security-policy-blue.svg)](SECURITY.md)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

Wpal is a server monitoring dashboard that ships as **a single static binary**.
Copy it to any Linux, macOS or Windows machine, run it, and you get a live
dashboard in the browser — no Python, no Node, no container runtime, no database.

| | |
|---|---|
| **Binary size** | 7 MB (all platforms, static, no cgo) |
| **Idle memory** | ~11.5 MB RSS (measured) |
| **Dependencies** | none — the web UI is embedded in the binary |
| **Datastore** | bounded ring file on disk, never grows |
| **Refresh** | 1 s sampling, streamed over WebSocket |

---

## Features

- **Live dashboard** — CPU (per core), memory, swap, disks, network, top processes
- **WebSocket streaming** — one shared sampling loop, all clients get the same frame
- **Threshold alerts** — six built-in rules, highlighted in the dashboard with an event stream
- **History** — 7 days by default in a bounded on-disk ring file; instant replay from 5 min to 7 days
- **Bilingual UI** — Chinese by default, English one click away
- **Zero configuration** — sensible defaults, runs with no config file at all
- **Runs anywhere** — bare metal, systemd, launchd, Windows Service, Docker, or `curl | sh`

---

## Screenshots

| Live | Dark | English | History replay |
|---|---|---|---|
| ![Live](docs/images/dashboard-light.png) | ![Dark](docs/images/dashboard-dark.png) | ![English](docs/images/dashboard-en.png) | ![History](docs/images/dashboard-history.png) |

---

## Install

### Linux

```bash
curl -fsSL https://raw.githubusercontent.com/xuhongteng/wpal/main/packaging/scripts/install.sh | sh
```

Or from a release tarball:

```bash
tar -xzf wpal_1.0.0_linux_amd64.tar.gz
sudo install -m 0755 wpal /usr/local/bin/wpal
wpal
```

The dashboard is then available at <http://127.0.0.1:8891/>.

> The service listens on loopback by default. Put it behind nginx / Caddy / IIS
> to expose it, and terminate TLS there.

### macOS

```bash
brew install wpal
wpal
```

### Windows

```powershell
winget install wpal
```

Or download `wpal_1.0.0_windows_amd64.zip`, run `wpal.exe`.

### Docker

```bash
docker run -d --name wpal \
  --pid host --network host \
  -v wpal-data:/data \
  -v /:/host/root:ro \
  ghcr.io/xuhongteng/wpal
```

`--pid host` and `--network host` let the container observe the *host's*
processes and interfaces; the read-only `/` mount gives it the real mount table.

### Packages

`.deb`, `.rpm` and `.tar.gz`/`.zip` archives are attached to every
[release](https://github.com/xuhongteng/wpal/releases), together with
SHA256 checksums and an SBOM.

A Windows MSI installer is also supported — its WiX template lives in
`packaging/windows/` and is enabled in `.goreleaser.yml` for
goreleaser ≥ 2.8 (the `wix` block ships commented out so the config stays
compatible with goreleaser 2.7).

---

## Configuration

Wpal works out of the box. To customise, drop a YAML file at
`/etc/wpal/config.yaml` (Linux) or pass `--config`:

```yaml
server:
  listen: "127.0.0.1:8891"
  base_path: ""            # deploy under a sub-path, e.g. /monitor

collect:
  interval: 1s
  include_virtual_nics: false
  top_processes: 15
  host_root: ""            # container: point at the mounted host root

history:
  enabled: true
  retain: 168h            # 7 days
  persist_interval: 10s
  data_dir: "/var/lib/wpal"

alerts:
  cpu:    { warn: 80,  crit: 93, hold: 30s, recover: 20s }
  memory: { warn: 85,  crit: 94, hold: 20s, recover: 20s }
  swap:   { warn: 50,  crit: 85, hold: 60s, recover: 30s }
  disk:   { warn: 80,  crit: 90, hold: 60s, recover: 60s }
  inode:  { warn: 85,  crit: 95, hold: 60s, recover: 60s }
  load:   { warn: 1.5, crit: 3.0, hold: 45s, recover: 30s }
```

Every value can also be set via environment variable (`WPAL_LISTEN`,
`WPAL_DATA_DIR`, …) or command line flag. See
[docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Metrics

| Area | Metrics |
|---|---|
| CPU | total usage, per-core usage, user / system / iowait / steal breakdown, load 1/5/15, context switch rate, frequency |
| Memory | total / used / available / free / cached / buffers / shared, swap usage |
| Disk | per-mount capacity and inode usage, per-device read & write throughput, IOPS, busy ratio |
| Network | per-interface up/down throughput and packet rate, errors, drops, TCP connection states |
| Processes | top processes by CPU and by memory, with PID, user, threads and command line |
| System | uptime, process/thread counts, file descriptor usage, entropy pool |

### Platform support

| Metric | Linux | macOS | Windows |
|---|:---:|:---:|:---:|
| CPU / memory / load | ✅ | ✅ | ✅ |
| Per-core CPU | ✅ | ✅ | ✅ |
| Disk capacity & IO | ✅ | ✅ | ⚠️ depends on PDH counters |
| Network throughput & TCP states | ✅ | ✅ | ✅ |
| inode usage | ✅ | n/a | n/a |
| steal time | ✅ | n/a | n/a |

Missing platform-specific metrics are reported as `0` rather than failing the
whole snapshot.

---

## HTTP API

| Endpoint | Description |
|---|---|
| `GET /api/health` | health check |
| `GET /api/snapshot` | current snapshot (served from cache, never re-collects) |
| `GET /api/history?range=1h` | history series (`5m`/`1h`/`6h`/`24h`/`7d`) |
| `GET /api/events?limit=100` | alert event stream, newest first |
| `GET /api/procs?sort=mem` | process details on demand |
| `WS /ws/stream` | live frames, one per second |

---

## Build from source

```bash
git clone https://github.com/xuhongteng/wpal.git
cd wpal
make build      # ./bin/wpal
make test       # unit tests
make cross      # cross-compile every target
make release    # goreleaser: all packages + checksums + SBOM
```

Requires Go 1.23 or newer. `make help` lists every target.

---

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
By participating you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE) © Wpal Contributors

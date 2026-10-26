# Configuration

[简体中文](CONFIGURATION.zh-CN.md)

Wpal runs with zero configuration. This page documents every knob.

---

## Precedence

```
built-in defaults  <  config file  <  environment variables  <  command line flags
```

The command line always wins, so a flag can override a packaged config without
editing files.

---

## Command line flags

| Flag | Default | Description |
|---|---|---|
| `--config <path>` | – | path to a YAML config file |
| `--listen <addr>` | `127.0.0.1:8891` | listen address |
| `--base-path <path>` | – | deploy under a sub-path, e.g. `/monitor` |
| `--data-dir <dir>` | `data` | directory for history and event files |
| `--host-root <dir>` | – | host root when running in a container |
| `--label <name>` | – | override the displayed host name |
| `--top <n>` | `15` | number of entries in the process tables |
| `--dump` | – | print one snapshot as JSON and exit |
| `--dump-all` | – | print every sampled frame as a JSON array and exit |
| `--repeat <n>` | `1` | number of frames for `--dump` |
| `--interval <dur>` | `1s` | delay between `--dump` frames |
| `--version` | – | print version information and exit |

---

## Environment variables

| Variable | Maps to |
|---|---|
| `LITESIGHT_LISTEN` | `server.listen` |
| `LITESIGHT_BASE_PATH` | `server.base_path` |
| `LITESIGHT_INTERVAL` | `collect.interval` |
| `LITESIGHT_DATA_DIR` | `history.data_dir` |
| `LITESIGHT_RETAIN` | `history.retain` |
| `LITESIGHT_HOST_ROOT` | `collect.host_root` |
| `LITESIGHT_LABEL` | `collect.label` |
| `LITESIGHT_TOP_PROCESSES` | `collect.top_processes` |
| `LITESIGHT_VIRTUAL_NICS` | `collect.include_virtual_nics` |

---

## File format

```yaml
server:
  listen: "127.0.0.1:8891"
  base_path: ""

collect:
  interval: 1s
  include_virtual_nics: false
  top_processes: 15
  host_root: ""
  label: ""

history:
  enabled: true
  retain: 168h
  persist_interval: 10s
  data_dir: "/var/lib/wpal"

alerts:
  cpu:    { warn: 80,  crit: 93, hold: 30s, recover: 20s }
  memory: { warn: 85,  crit: 94, hold: 20s, recover: 20s }
  swap:   { warn: 50,  crit: 85, hold: 60s, recover: 30s }
  inode:  { warn: 85,  crit: 95, hold: 60s, recover: 60s }
  disk:   { warn: 80,  crit: 90, hold: 60s, recover: 60s }
  load:   { warn: 1.5, crit: 3.0, hold: 45s, recover: 30s }
```

Durations accept Go's syntax: `s`, `m`, `h` (`30s`, `5m`, `168h`).

---

## Reference

### `server`

| Key | Type | Default | Description |
|---|---|---|---|
| `listen` | string | `127.0.0.1:8891` | Listen address. Keep it on loopback unless a proxy protects it. |
| `base_path` | string | `""` | Sub-path prefix, e.g. `/monitor`. Must match the proxy's path handling. |

### `collect`

| Key | Type | Default | Description |
|---|---|---|---|
| `interval` | duration | `1s` | Sampling period. Faster means more CPU and more traffic. |
| `include_virtual_nics` | bool | `false` | Include `docker0` / `veth*` / `br-*` in network totals. |
| `top_processes` | int | `15` | Length of the CPU and memory process tables. |
| `host_root` | string | `""` | Path to the mounted host root in a container (e.g. `/host/root`). |
| `label` | string | `""` | Overrides the displayed host name. |

### `history`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Disable to run without writing anything to disk. |
| `retain` | duration | `168h` | How much history to keep. Disk usage ≈ `retain / persist_interval × 344 bytes`. |
| `persist_interval` | duration | `10s` | How often a sample is written to disk. |
| `data_dir` | string | `data` | Where `history.db` and `events.jsonl` live. |

### `alerts`

Each rule accepts:

| Key | Meaning |
|---|---|
| `warn` | Warning threshold (percentage, or a multiplier for `load`) |
| `crit` | Critical threshold; must be ≥ `warn` |
| `hold` | How long the value must stay above the threshold before firing |
| `recover` | How long it must stay below the threshold before the alert resolves |

The state machine per rule:

```
normal ──value ≥ warn for `hold`──▶ warning
warning ──value ≥ crit──▶ critical (fires immediately)
warning/critical ──value < warn for `recover`──▶ resolved
```

---

## Platform differences

| Metric | Linux | macOS | Windows |
|---|---|---|---|
| inode usage | ✅ | n/a (reported as 0) | n/a (reported as 0) |
| steal time | ✅ | n/a | n/a |
| disk busy ratio | ✅ | partial | depends on PDH counters |
| swap | ✅ | ✅ (swapfile) | ✅ (pagefile) |

Unavailable metrics are reported as `0`; they never abort a snapshot.

---

## Files on disk

| File | Contents |
|---|---|
| `history.db` | fixed-size ring of samples; size is constant regardless of uptime |
| `events.jsonl` | append-only alert event log, trimmed to the most recent 500 entries |

Both can be deleted while the service is stopped; they are recreated on start.

---

## Tuning notes

**Lower memory / less overhead** — raise `collect.interval` to `2s` or `5s`.
Sampling is the only recurring cost; the dashboard itself is idle.

**Less disk** — shorten `history.retain` (e.g. `24h` instead of `168h`) or set
`history.enabled: false`.

**Fewer alerts** — raise the `warn` thresholds. The built-ins are deliberately
conservative because they also fire on sustained load, not just spikes.

**Smoother charts** — lower `history.persist_interval` to `5s` for finer
history resolution (at the cost of a larger file).

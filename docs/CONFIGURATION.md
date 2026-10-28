# 配置说明

[中文](CONFIGURATION.md) · [English](CONFIGURATION.en.md)

Wpal 零配置即可运行。本页列出所有可调项。

---

## 优先级

```
内置默认值  <  配置文件  <  环境变量  <  命令行参数
```

命令行永远最高，所以可以用参数临时覆盖安装包里的配置，不必改文件。

---

## 命令行参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--config <路径>` | – | YAML 配置文件路径 |
| `--listen <地址>` | `127.0.0.1:8891` | 监听地址 |
| `--base-path <路径>` | – | 部署在子路径下，如 `/monitor` |
| `--data-dir <目录>` | `data` | 历史与事件文件所在目录 |
| `--host-root <目录>` | – | 容器场景下的宿主根目录 |
| `--label <名称>` | – | 覆盖展示用的主机名 |
| `--top <n>` | `15` | 进程榜长度 |
| `--dump` | – | 打印一次快照 JSON 后退出 |
| `--dump-all` | – | 打印所有采样帧（JSON 数组）后退出 |
| `--repeat <n>` | `1` | `--dump` 的采样轮数 |
| `--interval <时长>` | `1s` | `--dump` 每轮之间的间隔 |
| `--version` | – | 打印版本信息后退出 |

---

## 环境变量

| 变量 | 对应配置 |
|---|---|
| `WPAL_LISTEN` | `server.listen` |
| `WPAL_BASE_PATH` | `server.base_path` |
| `WPAL_INTERVAL` | `collect.interval` |
| `WPAL_DATA_DIR` | `history.data_dir` |
| `WPAL_RETAIN` | `history.retain` |
| `WPAL_HOST_ROOT` | `collect.host_root` |
| `WPAL_LABEL` | `collect.label` |
| `WPAL_TOP_PROCESSES` | `collect.top_processes` |
| `WPAL_VIRTUAL_NICS` | `collect.include_virtual_nics` |

---

## 配置文件格式

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

时长用 Go 的写法：`s` / `m` / `h`（如 `30s`、`5m`、`168h`）。

---

## 配置项详解

### `server`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `listen` | string | `127.0.0.1:8891` | 监听地址。除非有代理保护，否则请保持回环。 |
| `base_path` | string | `""` | 子路径前缀，如 `/monitor`，需与代理的路径处理一致。 |

### `collect`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `interval` | duration | `1s` | 采样周期。越快越耗 CPU、越费流量。 |
| `include_virtual_nics` | bool | `false` | 是否把 `docker0`/`veth*`/`br-*` 计入网络总量。 |
| `top_processes` | int | `15` | CPU / 内存进程榜长度。 |
| `host_root` | string | `""` | 容器场景下宿主根目录的挂载路径（如 `/host/root`）。 |
| `label` | string | `""` | 覆盖展示用的主机名。 |

### `history`

| 键 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `enabled` | bool | `true` | 关掉则完全不落盘。 |
| `retain` | duration | `168h` | 历史留存时长。磁盘占用 ≈ `retain / persist_interval × 344 字节`。 |
| `persist_interval` | duration | `10s` | 落盘周期。 |
| `data_dir` | string | `data` | `history.db` 与 `events.jsonl` 所在目录。 |

### `alerts`

每条规则支持：

| 键 | 含义 |
|---|---|
| `warn` | 警告阈值（百分比；`load` 规则是倍数） |
| `crit` | 严重阈值，必须 ≥ `warn` |
| `hold` | 超过阈值需持续多久才触发 |
| `recover` | 回落到阈值以下需持续多久才判定恢复 |

每条规则的状态机：

```
正常 ──持续 ≥ warn 达 hold──▶ warning
warning ──≥ crit──▶ critical（立即触发）
warning/critical ──持续 < warn 达 recover──▶ resolved
```

---

## 平台差异

| 指标 | Linux | macOS | Windows |
|---|---|---|---|
| inode 使用率 | ✅ | 不适用（记 0） | 不适用（记 0） |
| steal time | ✅ | 不适用 | 不适用 |
| 磁盘繁忙率 | ✅ | 部分支持 | 取决于 PDH 计数器 |
| swap | ✅ | ✅（交换文件） | ✅（页面文件） |

平台不具备的指标一律记 `0`，绝不会导致整次采集失败。

---

## 磁盘上的文件

| 文件 | 内容 |
|---|---|
| `history.db` | 定长采样环形文件，体积与运行时长无关，恒定 |
| `events.jsonl` | 追加写的告警事件日志，最多保留最近 500 条 |

停服务后可以直接删，动时会重建。

---

## 调优建议

**更省内存 / 更省开销** —— 把 `collect.interval` 调到 `2s` 或 `5s`。
采样是唯一的周期性开销，看板本身是空闲的。

**更省磁盘** —— 缩短 `history.retain`（例如 `24h` 代替 `168h`），或直接
`history.enabled: false`。

**减少告警噪音** —— 调高 `warn` 阈值。内置阈值偏保守，因为它对"持续高负载"
也会告警，而不只是瞬时尖峰。

**曲线更细腻** —— 把 `history.persist_interval` 调到 `5s`，历史分辨率更高
（代价是文件更大）。

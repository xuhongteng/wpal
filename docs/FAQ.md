# 常见问题

[中文](FAQ.md) · [English](FAQ.en.md)

---

### 为什么容器里显示的是容器自身的指标，而不是宿主机的？

因为容器内的 `/proc` 反映的是容器的 PID 命名空间。用 `--pid host --network host` 启动，
并把宿主根目录以只读方式挂载进来：

```bash
docker run -d --pid host --network host -v /:/host/root:ro -v data:/data \
  --entrypoint /wpal ghcr.io/xuhongteng/wpal \
  --listen 127.0.0.1:8891 --data-dir /data --host-root /host/root
```

`--host-root` 让 Wpal 能读到**宿主机**的挂载表，因此分区容量和 inode 用量是真实值，
而不是 overlay 文件系统的数值。

---

### 为什么磁盘占用和 `df` 显示的不一致？

Wpal 沿用 `free` 对 "available" 的定义：百分比按 `used / (used + 用户可用)` 计算，
其中 `available` 不含为 root 预留的部分。在默认预留 5% 的 ext4 上，这个数值会比 `df`
**低**大约 5 个百分点。`total`、`used` 和 `free` 是分别上报的，与 `df` 完全一致。

---

### 为什么刚重启完 swap 占用率就很高？

进程退出时，swap 并不会归还给操作系统——页面会一直留在 swap 里，直到有别的进程
需要那块内存为止。因此「swap 占用高但内存压力低」属于正常现象，本身不是问题。
告警规则的作用是捕捉 swap 持续**增长**的情况。

---

### 看板显示"系统正常"，但我预期应该告警，为什么？

常见原因有两个：

1. 规则带 `hold` 窗口——数值必须持续高于阈值达到该时长才会触发。
2. 数值在 `hold` 耗尽前就恢复了，计时器被重置。

观察右下角的事件流，可以确认实际触发过什么。

---

### 看板有身份认证吗？

没有。Wpal 默认绑定 `127.0.0.1`，由前置的反向代理提供 TLS 和认证。
详见[安装说明](INSTALL.md#对外暴露)。

如果确实要直接暴露，请在 nginx 后面启用 `auth_basic`、OAuth 代理，或 IP 白名单。

---

### 没有公网 IP 的机器能跑吗？

能——只要能被需要访问它的浏览器访问到即可。用 SSH 隧道：

```bash
ssh -N -L 8891:127.0.0.1:8891 user@server
# 然后在本地打开 http://127.0.0.1:8891/
```

---

### 历史数据文件占多少磁盘？

`留存时长 / 落盘间隔 × 344 字节`，一次性分配：

| 留存时长 | 落盘间隔 | 占用 |
|---|---|---|
| 7 天 | 10s | ~20 MB |
| 24 小时 | 10s | ~7 MB |
| 7 天 | 60s | ~3.4 MB |

无论机器运行多久，该文件都不会超出这个大小。

---

### `load` 和 `load per core` 有什么区别？

`load` 规则会把 `load1` 与 CPU 核数做比较，所以在 4 核机器上负载均值达到 4 就会触发。
这通常正是你想要的语义："所有核心都忙"。

---

### 一个 Wpal 能监控多台机器吗？

暂时不能——一个进程监控一台机器。目前的预期做法是每台机器跑一个实例，
再用带路径前缀（或子域名）的代理聚合。中心化/agent 架构在路线图上，但尚未实现。

---

### 怎么确认 Wpal 实际采到了什么？

```bash
wpal --dump | python3 -m json.tool | head -40
```

`--dump` 输出的快照结构与 `/api/snapshot` 完全一致，是判断某个指标是"没采到"
还是"只是渲染方式不同"最快的方式。

---

### WebSocket 每隔几分钟就断一次

检查代理的读超时。nginx 需要：

```nginx
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
proxy_read_timeout 600s;
```

缺少 `Upgrade` 头时连接根本不是 WebSocket；`proxy_read_timeout` 过短则空闲连接会被切断。

---

### 怎么彻底卸载？

见[安装说明](INSTALL.md#卸载)。注意历史数据文件位于 `history.data_dir`，
除非执行 purge，否则包管理器不会删除它。

---

### API 契约在哪里看？

`internal/collector/collector.go` 是 JSON 结构的唯一真源，
HTTP 接口说明见 [CONFIGURATION.md](CONFIGURATION.md)。

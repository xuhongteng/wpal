# FAQ

[中文](FAQ.md) · [English](FAQ.en.md)

---

### Why does the container show the container's metrics instead of the host's?

Because `/proc` inside a container reflects the container's PID namespace. Run
it with `--pid host --network host` and mount the host root read-only:

```bash
docker run -d --pid host --network host -v /:/host/root:ro -v data:/data \
  --entrypoint /wpal ghcr.io/xuhongteng/wpal \
  --listen 127.0.0.1:8891 --data-dir /data --host-root /host/root
```

The `--host-root` flag is what lets Wpal read the *host's* mount table, so
partition sizes and inode usage are correct rather than the overlay's.

---

### Why does the disk show a different size than `df` shows?

Wpal follows `free`'s definition of "available": the percentage is
`used / (used + available-to-user)`, where `available` excludes the blocks
reserved for root. On ext4 with the default 5% reservation this makes the
number about 5 points *lower* than `df`. `total`, `used` and `free` are
reported separately and match `df` exactly.

---

### Why is the swap percentage so high right after a restart?

Swap is not released back to the OS when a process exits — the pages stay in
swap until something else needs that memory. A high swap percentage with low
memory pressure is therefore normal and not, by itself, a problem. The alert
rule exists to catch *growing* swap.

---

### The dashboard says "系统正常" but I expected an alert. Why?

Two common reasons:

1. The rule has a `hold` window — the value must stay above the threshold
   continuously for that long before it fires.
2. The value recovered before `hold` elapsed, so the timer reset.

Watch the event stream (bottom-right) to see what actually fired.

---

### Does the dashboard have authentication?

No. Wpal binds to `127.0.0.1` and expects a reverse proxy to provide
TLS and authentication. See [INSTALL.md](INSTALL.en.md#exposing-the-dashboard).

If you must expose it directly, put it behind nginx with
`auth_basic`, an OAuth proxy, or an IP allow-list.

---

### Can I run it on a machine without a public IP?

Yes — it only needs to be reachable by the browser that should see it.
Use an SSH tunnel:

```bash
ssh -N -L 8891:127.0.0.1:8891 user@server
# then open http://127.0.0.1:8891/ locally
```

---

### How much disk does the history file use?

`retain / persist_interval × 344 bytes`, allocated once:

| retain | persist_interval | size |
|---|---|---|
| 7 days | 10s | ~20 MB |
| 24 hours | 10s | ~7 MB |
| 7 days | 60s | ~3.4 MB |

The file never grows beyond that, regardless of uptime.

---

### What is the difference between `load` and `load per core`?

The `load` rule compares `load1` against the number of CPU cores, so a load
average of 4 on a 4-core machine trips it. That is usually what you want:
"all cores are busy".

---

### Can I monitor multiple machines with one Wpal?

Not yet — one process monitors one machine. Running one instance per machine
and putting them behind a proxy that adds a path prefix (or a subdomain) is
the intended workaround. A central/agent architecture is on the roadmap but
not implemented.

---

### How do I check what Wpal actually sees?

```bash
wpal --dump | python3 -m json.tool | head -40
```

`--dump` prints one snapshot in exactly the same shape as `/api/snapshot`,
which is the fastest way to tell whether a metric is missing or just rendered
differently.

---

### The WebSocket disconnects every few minutes

Check the proxy's read timeout. nginx needs:

```nginx
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
proxy_read_timeout 600s;
```

Without the `Upgrade` headers the connection is not a WebSocket at all; without
a long `proxy_read_timeout` an idle connection gets cut.

---

### How do I uninstall completely?

See [INSTALL.md](INSTALL.en.md#uninstall). Remember that the history file lives in
`history.data_dir` and is not removed by the package manager unless you purge.

---

### Where can I see the API contract?

`internal/collector/collector.go` is the single source of truth for the JSON
shape, and `docs/CONFIGURATION.md` documents the HTTP endpoints.

# Installation

[简体中文](INSTALL.zh-CN.md)

Wpal ships as a single static binary. Pick the method that matches your
platform; every method ends with the same thing — one process listening on
`127.0.0.1:8891`.

---

## Requirements

| | |
|---|---|
| OS | Linux (kernel 2.6+), macOS 11+, Windows 10/Server 2016+ |
| Arch | amd64, arm64, 386 (Linux only) |
| Runtime | none — no Python, Node, database or container runtime |
| Privileges | none for a bare-metal run; read access to the metrics of the user it runs as |

---

## Linux

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/wpal/wpal/main/packaging/scripts/install.sh | sh
```

The script installs the binary to `/usr/local/bin/wpal`, creates a
`wpal` system user, writes `/etc/wpal/config.yaml` and registers a
systemd unit.

### From a release tarball

```bash
VERSION=1.0.0
ARCH=amd64   # or arm64 / 386
curl -fsSLO "https://github.com/wpal/wpal/releases/download/v${VERSION}/wpal_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "wpal_${VERSION}_linux_${ARCH}.tar.gz"

sudo install -m 0755 wpal /usr/local/bin/wpal
wpal
```

### Debian / Ubuntu (.deb)

```bash
curl -fsSLO "https://github.com/wpal/wpal/releases/download/v1.0.0/wpal_1.0.0_linux_amd64.deb"
sudo dpkg -i wpal_1.0.0_linux_amd64.deb
sudo systemctl enable --now wpal
```

### RHEL / Rocky / Fedora (.rpm)

```bash
sudo rpm -Uvh wpal_1.0.0_linux_amd64.rpm
sudo systemctl enable --now wpal
```

### systemd

The packaged unit runs the service as the unprivileged `wpal` user with a
hardened sandbox:

```bash
sudo systemctl status wpal
sudo journalctl -u wpal -f
```

To run it yourself without installing the package:

```bash
sudo install -m 0644 packaging/systemd/wpal.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now wpal
```

---

## macOS

```bash
brew install wpal
wpal
```

Or manually:

```bash
VERSION=1.0.0
ARCH=arm64   # or amd64
curl -fsSLO "https://github.com/wpal/wpal/releases/download/v${VERSION}/wpal_${VERSION}_darwin_${ARCH}.tar.gz"
tar -xzf "wpal_${VERSION}_darwin_${ARCH}.tar.gz"
sudo install -m 0755 wpal /usr/local/bin/wpal
```

### launchd

```bash
sudo install -m 0644 packaging/launchd/com.wpal.agent.plist \
  /Library/LaunchDaemons/com.wpal.agent.plist
sudo launchctl load -w /Library/LaunchDaemons/com.wpal.agent.plist
```

---

## Windows

### MSI installer

Download `wpal_1.0.0_windows_amd64.msi` and run it. The installer writes
`C:\ProgramData\Wpal\` and can register the service.

> The MSI is produced by goreleaser ≥ 2.8 (WiX template in
> `packaging/windows/`). If your release only ships the `.zip`, use the
> portable instructions below.

### Portable

```powershell
# unzip wpal_1.0.0_windows_amd64.zip
.\wpal.exe --config .\config.yaml
```

### Register as a Windows service

```powershell
# 管理员权限的 PowerShell
wpal-service.exe install
wpal-service.exe start

# 卸载
wpal-service.exe stop
wpal-service.exe remove
```

### winget

```powershell
winget install wpal
```

---

## Docker

```bash
docker run -d --name wpal \
  --pid host --network host \
  -v wpal-data:/data \
  -v /:/host/root:ro \
  ghcr.io/wpal/wpal
```

or with compose:

```bash
docker compose up -d
```

### Why those flags

| Flag | Why |
|---|---|
| `--pid host` | the container reads the **host's** `/proc`, so process and CPU stats are real |
| `--network host` | `/proc/net` reflects the host's network namespace, so TCP state counts are real |
| `-v /:/host/root:ro` | gives access to the real mount table (partition sizes, inode usage) |

Without them you would be monitoring the container, not the machine.

---

## Exposing the dashboard

Wpal binds to loopback on purpose. To share it, put a reverse proxy in
front:

```nginx
server {
    listen 80;
    server_name monitor.example.com;

    location / {
        proxy_pass http://127.0.0.1:8891;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket 支持
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 600s;
    }
}
```

Deploying under a sub-path works too — set `server.base_path` in the config and
strip the prefix in `proxy_pass`:

```yaml
server:
  base_path: "/monitor"
```

```nginx
location ^~ /monitor/ {
    proxy_pass http://127.0.0.1:8891/;
    # ...同上的 websocket 头
}
```

> There is no built-in authentication. Always terminate TLS and add
> authentication at the proxy before exposing the dashboard publicly.

---

## Uninstall

**Linux (deb/rpm)**

```bash
sudo systemctl disable --now wpal
sudo apt remove wpal        # or: sudo rpm -e wpal
sudo rm -rf /var/lib/wpal   # 保留历史数据时删除此行
```

**macOS**

```bash
brew uninstall wpal
sudo rm -f /Library/LaunchDaemons/com.wpal.agent.plist
```

**Windows**

```powershell
wpal-service.exe stop
wpal-service.exe remove
```

---

## Verify the installation

```bash
wpal --version
curl -s http://127.0.0.1:8891/api/health
wpal --dump | head -20     # 打印一次采集快照，便于排查
```

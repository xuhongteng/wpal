# 安装说明

[English](INSTALL.md)

Wpal 以**单个静态二进制**的形式分发。挑一个适合你平台的安装方式，
最终得到的都是同一个东西——一个监听在 `127.0.0.1:8891` 的进程。

---

## 环境要求

| | |
|---|---|
| 操作系统 | Linux（内核 2.6+）、macOS 11+、Windows 10 / Server 2016+ |
| 架构 | amd64、arm64、386（仅 Linux） |
| 运行时 | 无——不需要 Python、Node、数据库或容器运行时 |
| 权限 | 裸机运行不需要特权；以哪个用户启动就只能读该用户可读的指标 |

---

## Linux

### 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/xuhongteng/wpal/main/packaging/scripts/install.sh | sh
```

脚本会把二进制装到 `/usr/local/bin/wpal`，创建 `wpal` 系统用户，
写入 `/etc/wpal/config.yaml`，并注册 systemd 服务。

### 手动安装（发布包）

```bash
VERSION=1.0.0
ARCH=amd64   # 或 arm64 / 386
curl -fsSLO "https://github.com/xuhongteng/wpal/releases/download/v${VERSION}/wpal_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "wpal_${VERSION}_linux_${ARCH}.tar.gz"

sudo install -m 0755 wpal /usr/local/bin/wpal
wpal
```

### Debian / Ubuntu（.deb）

```bash
curl -fsSLO "https://github.com/xuhongteng/wpal/releases/download/v1.0.0/wpal_1.0.0_linux_amd64.deb"
sudo dpkg -i wpal_1.0.0_linux_amd64.deb
sudo systemctl enable --now wpal
```

### RHEL / Rocky / Fedora（.rpm）

```bash
sudo rpm -Uvh wpal_1.0.0_linux_amd64.rpm
sudo systemctl enable --now wpal
```

### systemd

安装包内置的 unit 会以**非特权用户** `wpal` + 加固沙箱运行：

```bash
sudo systemctl status wpal
sudo journalctl -u wpal -f
```

不用安装包、想自己跑 systemd：

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

或手动安装：

```bash
VERSION=1.0.0
ARCH=arm64   # 或 amd64
curl -fsSLO "https://github.com/xuhongteng/wpal/releases/download/v${VERSION}/wpal_${VERSION}_darwin_${ARCH}.tar.gz"
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

### MSI 安装包

下载 `wpal_1.0.0_windows_amd64.msi` 运行即可，
安装程序会写入 `C:\ProgramData\Wpal\` 并可注册服务。

> MSI 由 goreleaser ≥ 2.8 生成（WiX 模板在 `packaging/windows/`）。
> 若某个版本只提供了 `.zip`，用下面的便携版方式即可。

### 便携版

```powershell
# 解压 wpal_1.0.0_windows_amd64.zip
.\wpal.exe --config .\config.yaml
```

### 注册为 Windows 服务

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
  ghcr.io/xuhongteng/wpal
```

或用 compose：

```bash
docker compose up -d
```

### 为什么需要这些参数

| 参数 | 原因 |
|---|---|
| `--pid host` | 容器内读到的 `/proc` 是**宿主的**，进程与 CPU 数据才是真实值 |
| `--network host` | `/proc/net` 反映宿主网络命名空间，TCP 状态统计才是真实的 |
| `-v /:/host/root:ro` | 拿到真实挂载表，分区容量与 inode 才准确（否则是 overlay 的） |

缺任何一个，你监控到的都只是容器自己。

---

## 对外暴露

Wpal 默认只监听回环地址，这是刻意的。要共享，请用反向代理：

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

部署在子路径下也可以，在配置里设 `server.base_path`，并在 `proxy_pass` 里去掉前缀：

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

> 内置**没有鉴权**。对外暴露前务必在代理层做 TLS 与鉴权。

---

## 卸载

**Linux（deb/rpm）**

```bash
sudo systemctl disable --now wpal
sudo apt remove wpal        # 或：sudo rpm -e wpal
sudo rm -rf /var/lib/wpal   # 想保留历史数据就删掉这行
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

## 验证安装

```bash
wpal --version
curl -s http://127.0.0.1:8891/api/health
wpal --dump | head -20     # 打印一次采集快照，便于排查
```

#!/bin/sh
# wpal 一键安装脚本（Linux / macOS）
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/xuhongteng/wpal/main/packaging/scripts/install.sh | sh
#
# 环境变量：
#   WPAL_VERSION   指定版本（默认最新 release）
#   WPAL_PREFIX    安装前缀（默认 /usr/local）
#   WPAL_REPO      GitHub 仓库（默认 xuhongteng/wpal）

set -eu

REPO="${WPAL_REPO:-xuhongteng/wpal}"
PREFIX="${WPAL_PREFIX:-/usr/local}"
VERSION="${WPAL_VERSION:-latest}"

case "$(uname -s)" in
    Linux)  OS=linux ;;
    Darwin) OS=darwin ;;
    *) echo "不支持的操作系统: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    armv7l) ARCH=arm ;;
    i386|i686) ARCH=386 ;;
    *) echo "不支持的架构: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/wpal_latest_${OS}_${ARCH}.tar.gz"
else
    URL="https://github.com/${REPO}/releases/download/v${VERSION}/wpal_${VERSION}_${OS}_${ARCH}.tar.gz"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "下载 $URL"
curl -fsSL "$URL" -o "$TMP/wpal.tar.gz"

tar -xzf "$TMP/wpal.tar.gz" -C "$TMP"

BIN="$TMP/wpal"
[ -f "$BIN" ] || { echo "压缩包内未找到 wpal 可执行文件" >&2; exit 1; }

if [ "$(id -u)" -ne 0 ]; then
    echo "需要 root 权限（安装到 $PREFIX 并注册 systemd 服务）" >&2
    exit 1
fi

echo "安装到 $PREFIX/bin/wpal"
install -m 0755 "$BIN" "$PREFIX/bin/wpal"

if [ "$OS" = "linux" ] && [ -d /etc/systemd/system ]; then
    install -m 0644 "$TMP/systemd/wpal.service" /etc/systemd/system/wpal.service

    if ! id wpal >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin wpal
    fi
    mkdir -p /etc/wpal /var/lib/wpal
    if [ ! -f /etc/wpal/config.yaml ]; then
        install -m 0644 "$TMP/debian/config.yaml" /etc/wpal/config.yaml
    fi
    chown -R wpal:wpal /var/lib/wpal

    systemctl daemon-reload
    systemctl enable wpal
    systemctl restart wpal
    echo "已注册并启动 systemd 服务：systemctl status wpal"
    echo "默认监听 127.0.0.1:8891，如需对外请用 nginx 反代"
else
    echo "已安装。运行 'wpal' 启动服务。"
fi

"$PREFIX/bin/wpal" --version

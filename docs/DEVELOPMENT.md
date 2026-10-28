# 开发指南

[中文](DEVELOPMENT.md) · [English](DEVELOPMENT.en.md)

---

## 目录结构

```
cmd/wpal/        CLI 入口：flag 解析、装配各组件、优雅退出
internal/
  collector/          指标采集。collector.go 是 JSON 契约
    linux.go            Linux：直读 /proc
    other.go            macOS / Windows：gopsutil
    util.go             公共辅助函数（无 build tag）
  config/             YAML + 环境变量 + flag 配置
  history/            有界环形文件（ring.go）与事件日志（events.go）
  alerts/             阈值状态机（engine.go）与规则定义（rules.go）
  sampler/            采样循环、落盘与广播
  server/             HTTP 路由、WebSocket 集线器、静态资源
  i18n/               后端侧兜底文案
  version/            版本信息（通过 ldflags 注入）
web/                  前端（嵌入二进制）
  locales/             zh-CN.json / en-US.json
  embed.go             go:embed 指令
packaging/            systemd、launchd、Windows、deb、rpm、安装脚本
tools/                验证脚本
```

---

## 构建环境

> 很多 CI 镜像上的 `/tmp` 是容量很小的 tmpfs。请把 Go 缓存放在 `$HOME` 下。

```bash
export GOROOT="$HOME/sdk/go"
export GOPATH="$HOME/go"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$GOPATH/.cache/go-build"
export GOTMPDIR="$GOPATH/tmp"
export GOPROXY="https://goproxy.cn,direct"
export CGO_ENABLED=0
```

`make` 已经导出这些变量；`scripts/env.sh` 里是同一份清单，供手动使用。

---

## 常用命令

```bash
make build       # ./bin/wpal
make run         # 以 ./data 为数据目录运行
make test        # 带 -race 的单元测试
make vet         # go vet
make lint        # golangci-lint（未安装则跳过）
make fmt         # gofmt -w
make cross       # 交叉编译所有目标平台到 dist/
make release     # goreleaser：安装包、校验和、SBOM
make clean
```

---

## JSON 契约

`internal/collector/collector.go` 定义了所有会到达浏览器的字段。修改规则：

- **只增不改。** 新增字段没问题；重命名或删除字段会破坏使用方。
- 字段语义在该文件里有说明，并与 `tools/compare.py` 使用的 Python 参考实现保持一致。

---

## 验证

仓库里同时维护两套实现：Go 实现，以及供对比脚本使用的 Python 参考实现。

| 脚本 | 验证内容 |
|---|---|
| `tools/compare.py` | 30+ 个采集字段与参考实现一致 |
| `tools/verify_web.py` | 对指定后端做 DOM 断言 + 截图 |
| `tools/compare_shots.py` | 同一套 UI 在两个后端下渲染完全一致 |
| `tools/verify_i18n.py` | 翻译键完整、语言切换正常 |

```bash
make verify       # 以上全部
make verify-a1    # 只跑字段对比
```

`compare.py` 会对两端各采样约 10 秒，并对速率类指标比较窗口均值——
两次独立采样发生在不同时刻，本身就会因与正确性无关的原因产生差异。

---

## 新增一个采集指标

1. 在 `internal/collector/collector.go` 中添加字段。
2. 在 `linux.go` 中实现，并在平台支持时同步在 `other.go` 中实现。
   无法提供该值的平台必须上报 `0`——既不能省略字段，也不能让整个快照失败。
3. 在 `tools/compare.py` 中添加对应的对比规则。
4. 在 `docs/CONFIGURATION.md` 中说明该平台差异。

---

## 新增一条文案

1. 在 **两个** 文件 `web/locales/zh-CN.json` 和 `web/locales/en-US.json` 中添加键。
   两边键集合不一致时 `tools/verify_i18n.py` 会失败。
2. 在前端通过 `t('key', {param: value})` 使用它。
3. 后端侧兜底文案放在 `internal/i18n/i18n.go`；后端同时会输出
   `label_key` / `detail_key`，供前端翻译。

---

## 发布流程

1. 更新 `CHANGELOG.md`。
2. 打标签：`git tag -a v1.1.0 -m "v1.1.0" && git push --tags`。
3. `Release` workflow 会构建所有平台，发布带校验和与 SBOM 的安装包，并推送容器镜像。
4. 先在本地用 `goreleaser release --snapshot --clean` 验证产物。

---

## 预算

| 预算项 | 上限 |
|---|---|
| 二进制体积 | 每平台 ≤ 12 MB |
| 空闲 RSS | ≤ 12 MiB |
| 运行时依赖 | 无 |
| 新增 Go 依赖 | 先在 issue 里讨论 |

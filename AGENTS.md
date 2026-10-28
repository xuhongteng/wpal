# AGENTS.md

Wpal —— 轻量级跨平台服务器资源监控看板。单个静态二进制即可交付，前端通过 `embed.FS` 嵌入其中，WebSocket 每秒推送一帧。Go 1.23+。

## 语言约定（优先级最高）

- **代码注释一律用中文撰写**：Go、JS、CSS、Python、Shell、YAML 均适用。注释解释*为什么*，而非重复代码本身在做什么。
- **说明文档以中文为主**：文件名不带语言后缀的文档，其正文必须是中文。
- 保留英文副本时统一用 `.en.md` 后缀（`README.md` 为中文主文件，`README.en.md` 为英文副本），文件顶部提供 `[中文](x.md) · [English](x.en.md)` 互链。新增文档遵循同一约定，不要再创建 `.zh-CN.md`。
- **以下内容保持原样，不得翻译**：Go 编译指令 `//go:build`、`//go:embed`；Shell shebang；命令行示例与代码块；标识符、函数名、文件路径、环境变量名。
- **法律与治理类文档保持英文**：`CODE_OF_CONDUCT.md`、`SECURITY.md`、`NOTICE`。
- 前端界面文案（`web/locales/*.json`）是产品功能而非注释，双语必须保持键集合一致（`tools/verify_i18n.py` 会校验），不受本约定影响。

## 产品命名

- 正式产品名只有 **Wpal**，标识符一律小写 `wpal`（二进制、systemd 服务、容器镜像、镜像 tag）。
- 中文描述跟在名字后面，形式统一为 `Wpal - <中文描述>`；**不要给产品名加第二个名字**（如 `Wpal（XX）`），历史上出现过的中文别名均已废弃。
- 前端界面标题以 `web/locales/*.json` 的 `app.title` 为准（`Wpal 监控` / `Wpal Monitor`），页面 `<title>` 与之一致。
- `go.mod` 的 module 路径沿用 `mon`，它只是内部导入路径，不属于对外产品名，不要拿它当品牌。

## Setup commands

- 安装依赖：`go mod download`
- 构建：`make build`（产物 `./bin/wpal`）
- 本地运行：`make run`
- 测试：`make test`（带 `-race`，需要 cgo）
- 静态检查：`make vet`、`make lint`
- 交叉编译全平台：`make cross`
- 验收：`make verify`（A1 字段对比 / A2 前端截图对比 / A7 i18n 完整度）

> 若环境中 `git` 不可用，`go build` 会因 VCS stamping 失败，加 `-buildvcs=false` 绕过。`make` 会自行导出 `GOROOT`/`GOPATH`/`CGO_ENABLED=0` 等变量，手工构建时注意保持一致。

## Project layout

- `cmd/wpal/` — CLI 入口：flag 解析、组件装配、优雅退出
- `internal/collector/` — 指标采集；`collector.go` 是 JSON 契约的唯一真源
- `internal/sampler/` — 单采样循环与多订阅者广播
- `internal/history/` — 有界环形文件与事件日志
- `internal/alerts/` — 阈值状态机与规则
- `internal/server/` — HTTP 路由、WebSocket 集线器、静态资源
- `internal/config/`、`internal/i18n/`、`internal/version/` — 配置、文案、版本信息
- `web/` — 前端（嵌入二进制），`web/locales/` 为界面文案
- `reference/python-impl/` — Python 参考实现，仅用于本地比对，不进发布包
- `tools/` — 验收脚本
- `packaging/` — deb/rpm/systemd/launchd/Windows/Docker 打包
- `docs/` — 长文档，中文为主 + `.en.md` 英文副本

## Code style

- 遵循 Go 惯例，提交前执行 `make fmt`（gofmt）。
- 导出标识符保持有文档说明。
- 不引入新的运行时依赖——依赖预算是产品承诺的一部分。
- 体积预算：每个平台二进制 ≤ 12 MB，空闲 RSS ≤ 12 MiB。
- 平台无法提供的指标必须上报 `0`，既不能省略字段，也不能让整个快照失败。

## Testing instructions

- 单元测试：`make test`，新增行为必须配套测试，参考同包下已有的 `*_test.go`。
- 改动了采集器、JSON 契约或前端时，必须跑 `make verify`。
- `tools/compare.py` 做字段级 parity；`tools/a2/run.sh` + `compare_shots.py` 让同一套前端分别连 Go 与 Python 后端做截图比对。
- 所有测试通过后再提 PR。

## 契约规则

- `internal/collector/collector.go` 定义了所有到达浏览器的字段，**只增不改**：新增字段可以，重命名或删除会破坏使用方。
- 字段语义变更时，同步更新 `tools/compare.py`，并在 `docs/CONFIGURATION.md` 说明平台差异。

## PR & commit conventions

- 从 `main` 切分支：`feat/short-description`、`fix/short-description`。
- 提交信息遵循 conventional commits（`feat:` / `fix:` / `docs:` / `refactor:` / `chore:`）。
- 提交需签署 DCO：`git commit -s`。
- CI 变绿后用 `gh pr create` 开 PR。

## Security

- 绝不提交密钥；`.env` 已在 `.gitignore` 中。
- 服务默认只监听回环地址，对外暴露由反向代理负责 TLS 与认证。
- 安全问题不要开公开 issue，按 `SECURITY.md` 的流程私下报告。

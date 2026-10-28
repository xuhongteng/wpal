# 参与 Wpal 开发

感谢你对改进 Wpal 的关注！本文说明如何提交改动，以及我们对 Pull Request 的期望。

[中文](CONTRIBUTING.md) · [English](CONTRIBUTING.en.md)

---

## 快速开始

```bash
git clone https://github.com/xuhongteng/wpal.git
cd wpal
make build      # 编译到 ./bin/wpal
make test       # 单元测试
make help       # 查看全部目标
```

需要 Go 1.23 或更高版本。构建和测试不依赖其他工具链。

---

## 基本原则

Wpal 的核心价值在于**体积小、依赖少**。提改动时请守住这些预算：

| 预算项 | 上限 | 校验方式 |
|---|---|---|
| 二进制体积 | 每平台 ≤ 12 MB | `make cross` + CI 体积检查 |
| 空闲 RSS | ≤ 12 MiB | `make verify` |
| 运行时依赖 | 无 | 不用 cgo，不引入外部数据库 |

把二进制体积推超预算的 Pull Request 不会合入，除非有充分理由。

---

## 开发流程

1. 除极小修复外，**先开 issue** 对齐方案，再动手实现，避免浪费你的时间。
2. 从 `main` 切分支：`feat/short-description`、`fix/short-description`。
3. **保持提交聚焦。** 一次提交只做一件逻辑上的事。
4. **签署**你的提交（DCO）：`git commit -s`。
5. **发起 Pull Request** 并填写模板。

### 推送前

```bash
make fmt      # gofmt
make vet      # go vet
make lint     # golangci-lint（已安装时）
make test     # 带 -race 的单元测试
make cross    # 确认所有目标平台仍可编译
```

### 验证看板行为未退化

仓库自带两个验证脚本，用于把 Go 实现与参考实现做对比：

```bash
make verify-a1   # 逐字段对比采集到的指标
make verify-a2   # 同一套 Web UI 对两个后端做像素级比对
make verify-a7   # i18n 键完整性与语言切换
```

`make verify` 会跑完这三项。改动了采集器、JSON 契约或前端时，请务必执行。

---

## 修改 JSON 契约

`/api/snapshot`、`/api/history` 和 `/api/events` 是公开 API。规则：

- **只增不改。** 新增字段没问题；重命名或删除字段会破坏别人的脚本。
- 字段语义必须与文档一致（见 `internal/collector/collector.go`，它是唯一真源）。
- 字段含义发生变化时，同步更新 `tools/compare.py`。

---

## 新增平台专属指标

平台无法提供的指标必须上报 `0`，既不能省略，也不能因此让整个快照失败。
把平台实现放在 `internal/collector/` 下，用 build tag 隔离，
并在 `docs/CONFIGURATION.md` 中说明差异。

---

## 代码风格

- 遵循 Go 惯例；执行 `gofmt`。
- 注释解释*为什么*，而不是*做了什么*。
- 导出标识符保持有文档说明。
- 未经讨论不引入新的运行时依赖——依赖预算是产品承诺的一部分。

---

## 报告缺陷与安全问题

- **缺陷** → 使用 [issue 模板](.github/ISSUE_TEMPLATE/)
- **安全问题** → **不要**开公开 issue，请按 [SECURITY.md](SECURITY.md) 的流程处理。

---

## 行为准则

参与本项目即受[行为准则](CODE_OF_CONDUCT.md)约束。参与贡献即表示你同意遵守它。

---

## 许可证

贡献内容以 [Apache License 2.0](LICENSE) 授权，与项目本身一致。

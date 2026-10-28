# 变更日志

本文件记录本项目所有值得关注的变更。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循[语义化版本](https://semver.org/lang/zh-CN/spec/v2.0.0.html)。

[中文](CHANGELOG.md) · [English](CHANGELOG.en.md)

## [未发布]

## [1.0.0] - 2026-10-27

### 新增

- Linux、macOS、Windows 跨平台指标采集。
  - Linux 直接读取 `/proc`（含逐核分解的 CPU、内存、swap、diskstats、网络、TCP 状态、进程）。
  - macOS 与 Windows 以 gopsutil 作为跨平台后端。
- 单个静态二进制，Web 看板通过 `embed.FS` 一并嵌入。
- 实时看板通过 WebSocket 每秒推送一帧。
- 六条内置阈值规则（CPU、内存、swap、磁盘、inode、按核负载），带 hold/recover 窗口，
  在看板内以高亮呈现，并提供事件流。
- 有界的磁盘环形文件保存历史（默认 7 天，大小固定，永不增长），
  支持从 5 分钟到 7 天的即时回看。
- 中英双语界面：默认中文，一键切换英文。
- 接口清单：`/api/health`、`/api/snapshot`、`/api/history`、`/api/events`、
  `/api/procs`、`WS /ws/stream`。
- 打包形态：Linux（`.deb`、`.rpm`、tarball、systemd unit）、macOS（tarball、launchd agent）、
  Windows（`.zip`、服务注册助手）以及 Docker。
- Linux 与 macOS 的 `curl | sh` 一键安装脚本。
- CI：lint、Linux/macOS/Windows 三平台测试、带 12 MB 体积预算的交叉编译矩阵、
  actionlint 工作流校验、CodeQL 扫描。
- 字段级验证脚本，将采集器与参考实现逐字段对比；另有截图级前端比对。

### 界面

- macOS 27 Retina 风格看板：统一标题栏配红绿灯窗口控件、毛玻璃卡片，
  浅色/深色主题跟随系统外观。
- 2×2 实时象限矩阵——CPU（逐核分解）、内存与 swap、网络吞吐、磁盘 I/O
  同屏流式展示，无需切换标签页。
- 以按需抽屉替代常驻面板：iStat Menus 风格的资源占用 Top 榜，
  以及通知中心风格的告警/事件流。
- 环境变量统一使用 `WPAL_*` 前缀；改名前的 `LITESIGHT_*` 仍可作为兼容回退。

### 说明

- 服务默认监听回环地址。需要对外暴露看板时请在前置反向代理。
- 平台本身不存在的指标（macOS/Windows 的 inode、非 Linux 的 steal time）
  上报为 `0`，而不是让快照失败。

[未发布]: https://github.com/xuhongteng/wpal/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/xuhongteng/wpal/releases/tag/v1.0.0

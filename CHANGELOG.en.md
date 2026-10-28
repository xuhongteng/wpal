# Changelog

[中文](CHANGELOG.md) · [English](CHANGELOG.en.md)

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-10-27

### Added

- Cross-platform metric collection for Linux, macOS and Windows.
  - Linux reads `/proc` directly (CPU with per-core breakdown, memory, swap,
    diskstats, network, TCP states, processes).
  - macOS and Windows use gopsutil as the cross-platform backend.
- Single static binary with the web dashboard embedded (`embed.FS`).
- Live dashboard streamed over WebSocket at one frame per second.
- Six built-in threshold rules (CPU, memory, swap, disk, inode, load per core)
  with hold/recover windows, rendered as in-dashboard highlights and an event
  stream.
- Bounded on-disk ring file for history (7 days by default, fixed size, never
  grows), with instant replay from 5 minutes to 7 days.
- Bilingual UI: Chinese by default, English one click away.
- API surface: `/api/health`, `/api/snapshot`, `/api/history`, `/api/events`,
  `/api/procs`, `WS /ws/stream`.
- Packaging for Linux (`.deb`, `.rpm`, tarball, systemd unit), macOS (tarball,
  launchd agent), Windows (`.zip`, service registration helper) and Docker.
- `curl | sh` installer for Linux and macOS.
- CI: lint, tests on Linux/macOS/Windows, cross-compile matrix with a 12 MB
  binary size budget, actionlint workflow validation, CodeQL scanning.
- Field-level verification scripts comparing the collector against a reference
  implementation, plus screenshot-level front-end comparison.

### Interface

- macOS 27 Retina inspired dashboard: unified title bar with traffic-light
  window controls, frosted-glass cards, and a light/dark theme that follows the
  system appearance.
- 2x2 live quadrant matrix — CPU (per-core breakdown), memory & swap, network
  throughput and disk I/O all stream on one screen, no tab switching required.
- On-demand drawers instead of permanent panels: a top-resource-consumer list in
  the style of iStat Menus, and a notification-centre style alert/event stream.
- Environment variables are prefixed `WPAL_*`; the pre-rename `LITESIGHT_*`
  names still work as a fallback.

### Notes

- The service listens on loopback by default. Put a reverse proxy in front of
  it to expose the dashboard.
- Platform metrics that do not exist (inode on macOS/Windows, steal time on
  non-Linux) are reported as `0` instead of failing the snapshot.

[Unreleased]: https://github.com/xuhongteng/wpal/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/xuhongteng/wpal/releases/tag/v1.0.0

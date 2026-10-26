"""阈值告警引擎：看板内标红 + 事件流（不出站）。

状态机：
  正常 → 持续超阈 hold 秒 → 触发（warning / critical）
  持续低于阈值 recover 秒 → 自动恢复（resolved）
  触发中等级升级（warning → critical）→ 立即出事件
"""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Any, Callable

from app.config import Settings


@dataclass
class Rule:
    key: str
    label: str
    source: str
    warn: float
    crit: float
    unit: str = "%"
    hold: float = 30.0
    recover: float = 20.0
    extractor: Callable[[dict[str, Any]], float] = field(default=lambda s: 0.0)
    detail: Callable[[dict[str, Any], float], str] = field(default=lambda s, v: "")


def _max_partition_percent(snap: dict[str, Any]) -> float:
    parts = (snap.get("disk") or {}).get("partitions") or []
    return max((p.get("percent", 0.0) for p in parts), default=0.0)


def _max_inode_percent(snap: dict[str, Any]) -> float:
    parts = (snap.get("disk") or {}).get("partitions") or []
    return max((p.get("inode_percent", 0.0) for p in parts), default=0.0)


def _max_disk_mount(snap: dict[str, Any]) -> str:
    parts = (snap.get("disk") or {}).get("partitions") or []
    if not parts:
        return ""
    return max(parts, key=lambda p: p.get("percent", 0.0)).get("mount", "")


def _load_per_core(snap: dict[str, Any]) -> float:
    cpu = snap.get("cpu") or {}
    cores = cpu.get("cores") or 1
    return float(cpu.get("load1", 0.0)) / max(1, cores)


def _cpu_total(snap: dict[str, Any]) -> float:
    return float((snap.get("cpu") or {}).get("total", 0.0))


def _mem_pct(snap: dict[str, Any]) -> float:
    return float((snap.get("mem") or {}).get("percent", 0.0))


def _swap_pct(snap: dict[str, Any]) -> float:
    return float((snap.get("mem") or {}).get("swap_percent", 0.0))


DEFAULT_RULES: list[Rule] = [
    Rule(
        key="cpu", label="CPU 使用率", source="cpu", warn=80.0, crit=93.0,
        hold=30.0, recover=20.0, extractor=_cpu_total,
        detail=lambda s, v: f"负载 {((s.get('cpu') or {}).get('load1', 0)):.2f}"
                              f" / {(s.get('cpu') or {}).get('cores', 0)} 核",
    ),
    Rule(
        key="mem", label="内存使用率", source="mem", warn=85.0, crit=94.0,
        hold=20.0, recover=20.0, extractor=_mem_pct,
        detail=lambda s, v: f"可用 {((s.get('mem') or {}).get('available', 0)) / 2**30:.1f} GiB",
    ),
    Rule(
        key="swap", label="Swap 使用率", source="mem", warn=50.0, crit=85.0,
        hold=60.0, recover=30.0, extractor=_swap_pct,
        detail=lambda s, v: f"Swap 已用 {((s.get('mem') or {}).get('swap_used', 0)) / 2**30:.1f} GiB",
    ),
    Rule(
        key="disk", label="磁盘使用率", source="disk", warn=80.0, crit=90.0,
        hold=60.0, recover=60.0, extractor=_max_partition_percent,
        detail=lambda s, v: f"挂载点 {_max_disk_mount(s)}",
    ),
    Rule(
        key="inode", label="inode 使用率", source="disk", warn=85.0, crit=95.0,
        hold=60.0, recover=60.0, extractor=_max_inode_percent,
        detail=lambda s, v: f"挂载点 {_max_disk_mount(s)}",
    ),
    Rule(
        key="load", label="负载/核数", source="cpu", warn=1.5, crit=3.0,
        unit="×", hold=45.0, recover=30.0, extractor=_load_per_core,
        detail=lambda s, v: f"load1 {((s.get('cpu') or {}).get('load1', 0)):.2f}",
    ),
]


class AlertEngine:
    def __init__(self, settings: Settings, rules: list[Rule] | None = None) -> None:
        self.settings = settings
        self.rules = rules if rules is not None else list(DEFAULT_RULES)
        self.state: dict[str, dict[str, Any]] = {
            r.key: {"active": None, "since": None, "value": 0.0, "level": None} for r in self.rules
        }

    def _target_level(self, rule: Rule, value: float) -> str | None:
        if value >= rule.crit:
            return "critical"
        if value >= rule.warn:
            return "warning"
        return None

    def update(self, snap: dict[str, Any], now: float | None = None) -> list[dict[str, Any]]:
        now = now if now is not None else time.time()
        events: list[dict[str, Any]] = []

        for rule in self.rules:
            try:
                value = float(rule.extractor(snap))
            except Exception:  # noqa: BLE001 - 取值失败按 0 处理，不影响其它规则
                value = 0.0

            st = self.state[rule.key]
            target = self._target_level(rule, value)

            if target is not None:
                if st["active"] is None:
                    if st["since"] is None:
                        st["since"] = now
                    if now - st["since"] >= rule.hold:
                        st["active"] = target
                        st["since"] = None
                        events.append(self._event(rule, value, target, "firing", snap, now))
                elif target != st["active"]:
                    # 等级升级，立即出事件
                    st["active"] = target
                    st["since"] = None
                    events.append(self._event(rule, value, target, "firing", snap, now))
            else:
                if st["active"] is not None:
                    if st["since"] is None:
                        st["since"] = now
                    if now - st["since"] >= rule.recover:
                        events.append(self._event(rule, value, st["active"], "resolved", snap, now))
                        st["active"] = None
                        st["since"] = None
                else:
                    st["since"] = None

            st["value"] = value
        return events

    def _event(
        self, rule: Rule, value: float, level: str, status: str,
        snap: dict[str, Any], now: float,
    ) -> dict[str, Any]:
        return {
            "ts": now,
            "key": rule.key,
            "label": rule.label,
            "source": rule.source,
            "level": level,
            "status": status,
            "value": round(value, 2),
            "unit": rule.unit,
            "detail": rule.detail(snap, value),
        }

    def active_alerts(self) -> list[dict[str, Any]]:
        """当前处于触发状态的告警（供看板标红）。"""
        out = []
        for rule in self.rules:
            st = self.state[rule.key]
            if st["active"]:
                out.append({
                    "key": rule.key,
                    "label": rule.label,
                    "level": st["active"],
                    "value": round(st["value"], 2),
                    "unit": rule.unit,
                    "source": rule.source,
                })
        return out

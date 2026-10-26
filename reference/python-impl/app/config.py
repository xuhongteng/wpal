"""配置项：全部支持环境变量覆盖。"""

from __future__ import annotations

import os


def _f(name: str, default: float) -> float:
    try:
        return float(os.environ.get(name, default))
    except (TypeError, ValueError):
        return default


def _i(name: str, default: int) -> int:
    try:
        return int(os.environ.get(name, default))
    except (TypeError, ValueError):
        return default


def _b(name: str, default: bool) -> bool:
    raw = os.environ.get(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


class Settings:
    def __init__(self) -> None:
        self.data_dir = os.environ.get("MON_DATA_DIR", "/app/data")
        self.db_path = os.path.join(self.data_dir, "metrics.db")

        # 采样与落盘节奏
        self.sample_interval = _f("MON_SAMPLE_INTERVAL", 1.0)
        self.persist_interval = _f("MON_PERSIST_INTERVAL", 10.0)
        self.retention_days = _i("MON_RETENTION_DAYS", 7)

        # 宿主根目录挂载点，用于读取宿主真实挂载表
        self.host_root = os.environ.get("MON_HOST_ROOT", "/host/root")
        # 额外 bind 的宿主分区路径（如 /boot/efv），用于测量独立文件系统
        self.alt_roots = [
            p for p in os.environ.get("MON_ALT_ROOTS", "/host/mnt").split(",") if p.strip()
        ]
        self.host_label = os.environ.get("MON_HOST_LABEL", "")

        # 进程榜长度
        self.top_processes = _i("MON_TOP_PROCESSES", 15)

        # 是否把 docker0 / veth / br-* 等虚拟网卡计入总量
        self.include_virtual_nics = _b("MON_VIRTUAL_NICS", False)

        # 事件流上限
        self.max_events = _i("MON_MAX_EVENTS", 500)

        # 历史回看档位（秒）
        self.history_ranges = {
            "5m": 300,
            "1h": 3600,
            "6h": 21600,
            "24h": 86400,
            "7d": 604800,
        }

        # TCP 连接状态刷新间隔（秒），全量扫描 /proc/net/tcp 略贵
        self.tcp_refresh = _f("MON_TCP_REFRESH", 2.0)


settings = Settings()

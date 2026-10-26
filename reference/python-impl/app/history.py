"""SQLite 历史存储：指标采样落盘 + 告警事件流。

只存"画图用"的精简字段，避免 7 天留存把库撑大。
"""

from __future__ import annotations

import json
import os
import sqlite3
import threading
import time
from typing import Any

from app.config import Settings


class HistoryStore:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        os.makedirs(settings.data_dir, exist_ok=True)
        self._lock = threading.Lock()
        self.conn = sqlite3.connect(settings.db_path, check_same_thread=False)
        self.conn.row_factory = sqlite3.Row
        self.conn.execute("PRAGMA journal_mode=WAL")
        self.conn.execute("PRAGMA synchronous=NORMAL")
        self._init_schema()

    def _init_schema(self) -> None:
        with self._lock:
            self.conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS samples (
                    ts          INTEGER PRIMARY KEY,
                    cpu         REAL,
                    mem         REAL,
                    mem_used    REAL,
                    swap        REAL,
                    load1       REAL,
                    cpu_percore TEXT,
                    cpu_brk      TEXT,
                    net         TEXT,
                    disk        TEXT,
                    disk_free   TEXT,
                    diskio      TEXT
                );
                CREATE INDEX IF NOT EXISTS idx_samples_ts ON samples(ts);

                CREATE TABLE IF NOT EXISTS events (
                    id      INTEGER PRIMARY KEY AUTOINCREMENT,
                    ts      INTEGER NOT NULL,
                    key     TEXT NOT NULL,
                    label   TEXT NOT NULL,
                    level   TEXT NOT NULL,
                    status  TEXT NOT NULL,
                    value   REAL,
                    unit    TEXT,
                    detail  TEXT
                );
                CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
                """
            )
            self.conn.commit()

    # ---------- 采样落盘 ----------

    def persist(self, snap: dict[str, Any]) -> None:
        cpu = snap.get("cpu") or {}
        mem = snap.get("mem") or {}
        disk = snap.get("disk") or {}
        net = snap.get("net") or {}

        percore = json.dumps([c.get("busy", 0.0) for c in cpu.get("percore", [])])
        net_map = json.dumps({
            i["name"]: [round(i["rx_bps"], 1), round(i["tx_bps"], 1)]
            for i in net.get("interfaces", [])
        })
        disk_map = json.dumps({p["mount"]: p["percent"] for p in disk.get("partitions", [])})
        disk_free = json.dumps({p["mount"]: p["free"] for p in disk.get("partitions", [])})
        diskio_map = json.dumps({
            d["name"]: [round(d["read_bps"], 1), round(d["write_bps"], 1)]
            for d in disk.get("io", [])
        })

        with self._lock:
            self.conn.execute(
                """
                INSERT OR REPLACE INTO samples
                    (ts, cpu, mem, mem_used, swap, load1, cpu_percore, cpu_brk, net, disk, disk_free, diskio)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
                """,
                (
                    int(snap["ts"]), cpu.get("total", 0.0), mem.get("percent", 0.0),
                    mem.get("used", 0.0), mem.get("swap_percent", 0.0), cpu.get("load1", 0.0),
                    percore, json.dumps(cpu.get("breakdown") or {}), net_map, disk_map, disk_free, diskio_map,
                ),
            )
            self.conn.commit()

    # ---------- 事件 ----------

    def add_event(self, event: dict[str, Any]) -> None:
        with self._lock:
            self.conn.execute(
                """
                INSERT INTO events (ts, key, label, level, status, value, unit, detail)
                VALUES (?,?,?,?,?,?,?,?)
                """,
                (
                    int(event["ts"]), event["key"], event["label"], event["level"],
                    event["status"], float(event.get("value") or 0.0), event.get("unit", ""),
                    event.get("detail", ""),
                ),
            )
            self.conn.commit()

    def recent_events(self, limit: int = 100) -> list[dict[str, Any]]:
        with self._lock:
            rows = self.conn.execute(
                "SELECT * FROM events ORDER BY ts DESC, id DESC LIMIT ?", (limit,)
            ).fetchall()
        return [dict(r) for r in rows]

    def event_count(self) -> int:
        with self._lock:
            row = self.conn.execute("SELECT COUNT(*) AS c FROM events").fetchone()
        return int(row["c"]) if row else 0

    # ---------- 历史回看 ----------

    def history(self, seconds: int, max_points: int = 720) -> dict[str, Any]:
        since = int(time.time()) - seconds
        with self._lock:
            rows = self.conn.execute(
                """
                SELECT ts, cpu, mem, mem_used, swap, load1, cpu_percore, cpu_brk, net, disk, disk_free, diskio
                FROM samples WHERE ts >= ? ORDER BY ts ASC
                """,
                (since,),
            ).fetchall()

        rows = list(rows)
        if len(rows) > max_points:
            step = len(rows) / max_points
            rows = [rows[int(i * step)] for i in range(max_points)]

        def _loads(v: Any) -> list[float]:
            try:
                return [float(x) for x in json.loads(v or "[]")]
            except (TypeError, ValueError):
                return []

        def _map(v: Any) -> dict:
            try:
                return json.loads(v or "{}")
            except (TypeError, ValueError):
                return {}

        return {
            "ts": [r["ts"] for r in rows],
            "cpu": [r["cpu"] for r in rows],
            "mem": [r["mem"] for r in rows],
            "mem_used": [r["mem_used"] for r in rows],
            "swap": [r["swap"] for r in rows],
            "load1": [r["load1"] for r in rows],
            "cpu_percore": [_loads(r["cpu_percore"]) for r in rows],
            "cpu_brk": [_map(r["cpu_brk"]) for r in rows],
            "net": [_map(r["net"]) for r in rows],
            "disk": [_map(r["disk"]) for r in rows],
            "disk_free": [_map(r["disk_free"]) for r in rows],
            "diskio": [_map(r["diskio"]) for r in rows],
            "count": len(rows),
        }

    # ---------- 清理 ----------

    def prune(self) -> int:
        """删除过期采样与事件，返回删除行数。"""
        cutoff = int(time.time()) - self.settings.retention_days * 86400
        with self._lock:
            cur = self.conn.execute("DELETE FROM samples WHERE ts < ?", (cutoff,))
            deleted = cur.rowcount or 0
            # 事件保留更久一点，方便回溯
            ev_cutoff = cutoff - self.settings.retention_days * 86400
            self.conn.execute("DELETE FROM events WHERE ts < ?", (ev_cutoff,))
            self.conn.execute(
                "DELETE FROM events WHERE id NOT IN "
                "(SELECT id FROM events ORDER BY id DESC LIMIT ?)",
                (self.settings.max_events,),
            )
            self.conn.commit()
        return deleted

    def close(self) -> None:
        with self._lock:
            self.conn.close()

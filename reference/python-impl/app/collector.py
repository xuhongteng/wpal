"""宿主机指标采集器。

运行前提（见 docker-compose.yml）：
  * 容器以 ``--pid host --network host`` 启动 → /proc 与 /proc/net 都是宿主的
  * 宿主根目录挂载到 ``MON_HOST_ROOT``（默认 /host/root）
    → 挂载点/分区信息从 HOST_ROOT/proc/mounts 读取，得到宿主真实挂载表

采集内容：CPU（含每核、steal、iowait、上下文切换）、内存/Swap、磁盘（容量+inode+读写吞吐）、
网卡（速率/错包/丢包）、TCP 连接状态、进程 Top 榜、系统健康（运行时长/fd/熵池）。
"""

from __future__ import annotations

import os
import time
from typing import Any

import psutil

from app.config import Settings

# 视为"真实磁盘"的文件系统，其余（tmpfs/overlay/squashfs/…）不计入容量看板
REAL_FS = {
    "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "f2fs", "jfs", "reiserfs",
    "ufs", "vfat", "ntfs", "ntfs3", "exfat", "apfs",
}

# 这些前缀下的挂载点跳过（伪文件系统 / 容器内部视图）
SKIP_MOUNT_PREFIX = ("/proc", "/sys", "/dev", "/run", "/snap", "/host")

# 虚拟网卡前缀，默认不计入总流量
VIRTUAL_NIC_PREFIX = ("docker", "veth", "br-", "virbr", "cni", "flannel", "tun", "tap")


def _read_text(path: str, default: str = "") -> str:
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as fh:
            return fh.read()
    except OSError:
        return default


def _safe(fn, default=None):
    """调用可能抛异常的探测函数，失败返回默认值。"""
    try:
        return fn()
    except Exception:  # noqa: BLE001 - 采集层不允许因单项探测失败整体崩掉
        return default


class Collector:
    def __init__(self, settings: Settings) -> None:
        self.s = settings
        self._proc_root = os.path.join(settings.host_root, "proc")
        self._prev_cpu = _safe(lambda: psutil.cpu_times(percpu=True), []) or []
        self._prev_net = _safe(lambda: psutil.net_io_counters(pernic=True), {}) or {}
        self._prev_disk_io = _safe(lambda: psutil.disk_io_counters(perdisk=True), {}) or {}
        self._prev_ts = time.monotonic()
        self._prev_ctxt = self._read_ctxt()
        self._tcp_cache: tuple[float, dict[str, int]] = (0.0, {})
        self._boot_time = _safe(psutil.boot_time, time.time())

    # ---------- 基础读取 ----------

    def _read_ctxt(self) -> float:
        """累计上下文切换次数。"""
        for line in _read_text(os.path.join(self._proc_root, "stat")).splitlines():
            if line.startswith("ctxt "):
                try:
                    return float(line.split()[1])
                except (IndexError, ValueError):
                    return 0.0
        return 0.0

    def _proc_stat_fields(self) -> dict[str, float]:
        """从 /proc/stat 取 ctxt / processes / procs_running。"""
        out: dict[str, float] = {}
        for line in _read_text(os.path.join(self._proc_root, "stat")).splitlines():
            if line.startswith("ctxt "):
                out["ctxt"] = float(line.split()[1])
            elif line.startswith("processes "):
                out["processes"] = float(line.split()[1])
            elif line.startswith("procs_running "):
                out["procs_running"] = float(line.split()[1])
            elif line.startswith("procs_blocked "):
                out["procs_blocked"] = float(line.split()[1])
        return out

    def _host_mounts(self) -> list[dict[str, str]]:
        """宿主真实挂载表。

        注意：/proc/mounts 等价于 /proc/self/mounts，反映的是**读取者自己**的挂载命名空间，
        在容器里读到的是容器的挂载表。宿主挂载表要读 /proc/<宿主init>/mountinfo
        （容器以 --pid host 运行时，PID 1 即宿主 systemd）。

        挂载点路径通过 bind mount 的宿主根目录（/host/root）访问；
        独立文件系统（如 /boot/efv）在 bind mount 里量不准，用 major:minor 校验后跳过，
        除非通过 MON_ALT_ROOTS 挂载了对应路径。
        """
        raw = self._read_mountinfo()
        best: dict[str, dict[str, str]] = {}
        for entry in raw:
            majmin = entry["majmin"]
            path = os.path.join(self.s.host_root, entry["mount"].lstrip("/"))
            if not self._dev_matches(path, majmin):
                alt = self._find_alt_path(majmin)
                if alt is None:
                    continue
                path = alt
            cur = best.get(majmin)
            if cur is None or len(entry["mount"]) < len(cur["mount"]):
                best[majmin] = {
                    "device": entry["source"] or majmin,
                    "mount": entry["mount"],
                    "fstype": entry["fstype"],
                    "path": path,
                }
        return sorted(best.values(), key=lambda x: x["mount"])

    def _read_mountinfo(self) -> list[dict[str, str]]:
        """解析 mountinfo，返回 mountpoint / fstype / source / major:minor。"""
        raw = ""
        for cand in ("/proc/1/mountinfo", os.path.join(self._proc_root, "1/mountinfo")):
            raw = _read_text(cand)
            if raw:
                break
        out: list[dict[str, str]] = []
        for line in raw.splitlines():
            parts = line.split()
            if len(parts) < 10:
                continue
            # 字段：id parent maj:min root mountpoint opts [optional...] - fstype source superopts
            try:
                sep = parts.index("-")
            except ValueError:
                continue
            if sep < 6 or sep + 2 >= len(parts):
                continue
            mountpoint = parts[4].replace("\\040", " ").replace("\\011", "\t")
            fstype = parts[sep + 1]
            source = parts[sep + 2]
            if fstype not in REAL_FS:
                continue
            if any(mountpoint == p or mountpoint.startswith(p.rstrip("/") + "/")
                   for p in SKIP_MOUNT_PREFIX):
                continue
            out.append({
                "mount": mountpoint,
                "fstype": fstype,
                "source": source,
                "majmin": parts[2],
            })
        return out

    def _dev_matches(self, path: str, majmin: str) -> bool:
        """bind mount 出来的路径与挂载表里的 major:minor 是否一致。"""
        try:
            st = os.stat(path)
        except OSError:
            return False
        return f"{os.major(st.st_dev)}:{os.minor(st.st_dev)}" == majmin

    def _find_alt_path(self, majmin: str) -> str | None:
        """在 MON_ALT_ROOTS 下找同设备的可测量路径（如额外 bind 的独立分区）。"""
        for root in self.s.alt_roots:
            try:
                entries = os.listdir(root)
            except OSError:
                continue
            for name in entries:
                p = os.path.join(root, name)
                if self._dev_matches(p, majmin):
                    return p
        return None

    # ---------- 各维度采集 ----------

    def _cpu(self, dt: float) -> dict[str, Any]:
        times = _safe(lambda: psutil.cpu_times(percpu=True), None) or []
        prev = self._prev_cpu
        self._prev_cpu = times

        def pct(delta: dict[str, float]) -> dict[str, float]:
            total = sum(delta.values())
            if total <= 0:
                return {"busy": 0.0, "user": 0.0, "system": 0.0, "iowait": 0.0,
                        "steal": 0.0, "idle": 100.0}
            idle = delta["idle"]
            user = delta["user"] + delta["nice"]
            system = delta["system"] + delta["irq"] + delta["softirq"]
            return {
                "busy": 100.0 * (total - idle) / total,
                "user": 100.0 * user / total,
                "system": 100.0 * system / total,
                "iowait": 100.0 * delta["iowait"] / total,
                "steal": 100.0 * delta["steal"] / total,
                "idle": 100.0 * idle / total,
            }

        keys = ("user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal")
        percore: list[dict[str, float]] = []
        for idx, t1 in enumerate(times):
            if idx >= len(prev):
                percore.append(pct({k: 0.0 for k in keys}))
                continue
            t0 = prev[idx]
            delta = {
                k: float(getattr(t1, k, 0.0)) - float(getattr(t0, k, 0.0))
                for k in keys
            }
            percore.append(pct(delta))

        # 汇总：把所有核的 delta 相加后统一算比例
        agg = {k: 0.0 for k in ("user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal")}
        if times and len(prev) == len(times):
            for t0, t1 in zip(prev, times):
                for k in agg:
                    agg[k] += float(getattr(t1, k, 0.0)) - float(getattr(t0, k, 0.0))
        else:
            agg = {k: 0.0 for k in agg}
        total_pct = pct(agg)

        ctxt = self._read_ctxt()
        ctx_rate = max(0.0, (ctxt - self._prev_ctxt) / dt)
        self._prev_ctxt = ctxt

        try:
            load1, load5, load15 = os.getloadavg()
        except OSError:
            load1 = load5 = load15 = 0.0

        freq = _safe(lambda: psutil.cpu_freq().current, 0.0) or 0.0

        return {
            "total": round(total_pct["busy"], 2),
            "breakdown": {k: round(v, 2) for k, v in total_pct.items()},
            "percore": [
                {
                    "busy": round(c["busy"], 2),
                    "user": round(c["user"], 2),
                    "system": round(c["system"], 2),
                    "iowait": round(c["iowait"], 2),
                    "steal": round(c["steal"], 2),
                }
                for c in percore
            ],
            "cores": psutil.cpu_count(logical=True) or len(percore),
            "physical": psutil.cpu_count(logical=False),
            "load1": round(load1, 2),
            "load5": round(load5, 2),
            "load15": round(load15, 2),
            "ctx_switch_rate": round(ctx_rate, 1),
            "freq_mhz": round(freq, 1),
        }

    def _mem(self) -> dict[str, Any]:
        vm = _safe(psutil.virtual_memory)
        sw = _safe(psutil.swap_memory)
        if vm is None:
            return {}
        total = float(vm.total)
        out = {
            "total": total,
            "used": float(vm.used),
            "available": float(vm.available),
            "free": float(vm.free),
            "percent": round(float(vm.percent), 2),
            "cached": float(getattr(vm, "cached", 0.0)),
            "buffers": float(getattr(vm, "buffers", 0.0)),
            "shared": float(getattr(vm, "shared", 0.0)),
            "swap_total": float(sw.total) if sw else 0.0,
            "swap_used": float(sw.used) if sw else 0.0,
            "swap_free": float(sw.free) if sw else 0.0,
            "swap_percent": round(float(sw.percent), 2) if sw else 0.0,
        }
        return out

    def _disk(self, dt: float) -> dict[str, Any]:
        partitions = []
        for mp in self._host_mounts():
            path = mp.get("path") or os.path.join(self.s.host_root, mp["mount"].lstrip("/"))
            usage = _safe(lambda p=path: psutil.disk_usage(p))
            if usage is None:
                continue
            st = _safe(lambda p=path: os.statvfs(p))
            inode_percent = 0.0
            inode_total = inode_free = 0
            if st is not None and st.f_files:
                inode_total = st.f_files
                inode_free = st.f_favail
                inode_percent = round(100.0 * (st.f_files - st.f_favail) / st.f_files, 2)
            partitions.append({
                "device": mp["device"],
                "mount": mp["mount"],
                "fstype": mp["fstype"],
                "total": usage.total,
                "used": usage.used,
                "free": usage.free,
                "percent": round(float(usage.percent), 2),
                "inode_percent": inode_percent,
                "inode_total": inode_total,
                "inode_free": inode_free,
            })

        io_list = []
        cur_io = _safe(lambda: psutil.disk_io_counters(perdisk=True), {}) or {}
        prev_io = self._prev_disk_io
        for name, c in cur_io.items():
            if name.startswith(("loop", "ram", "zram")) or name[:4] == "srw":
                continue
            p = prev_io.get(name)
            if p is None:
                continue
            read_bps = (c.read_bytes - p.read_bytes) / dt
            write_bps = (c.write_bytes - p.write_bytes) / dt
            read_iops = (c.read_count - p.read_count) / dt
            write_iops = (c.write_count - p.write_count) / dt
            busy = 0.0
            if p.read_time or p.write_time:
                busy = min(100.0, ((c.read_time - p.read_time) + (c.write_time - p.write_time)) / 1000.0 / dt * 100.0)
            io_list.append({
                "name": name,
                "read_bps": max(0.0, read_bps),
                "write_bps": max(0.0, write_bps),
                "read_iops": max(0.0, read_iops),
                "write_iops": max(0.0, write_iops),
                "busy": round(busy, 2),
            })
        self._prev_disk_io = cur_io
        io_list.sort(key=lambda x: x["read_bps"] + x["write_bps"], reverse=True)

        return {"partitions": partitions, "io": io_list[:8]}

    def _net(self, dt: float) -> dict[str, Any]:
        cur = _safe(lambda: psutil.net_io_counters(pernic=True), {}) or {}
        prev = self._prev_net
        self._prev_net = cur

        ifaces = []
        for name, c in cur.items():
            virtual = name.startswith(VIRTUAL_NIC_PREFIX)
            if virtual and not self.s.include_virtual_nics:
                continue
            p = prev.get(name)
            if p is None:
                p = c
            rx_bps = (c.bytes_recv - p.bytes_recv) / dt
            tx_bps = (c.bytes_sent - p.bytes_sent) / dt
            rx_pps = (c.packets_recv - p.packets_recv) / dt
            tx_pps = (c.packets_sent - p.packets_sent) / dt
            ifaces.append({
                "name": name,
                "rx_bps": max(0.0, rx_bps),
                "tx_bps": max(0.0, tx_bps),
                "rx_pps": max(0.0, rx_pps),
                "tx_pps": max(0.0, tx_pps),
                "rx_total": c.bytes_recv,
                "tx_total": c.bytes_sent,
                "err_in": c.errin,
                "err_out": c.errout,
                "drop_in": c.dropin,
                "drop_out": c.dropout,
                "virtual": virtual,
            })
        ifaces.sort(key=lambda x: x["rx_bps"] + x["tx_bps"], reverse=True)

        # TCP 连接状态（扫 /proc/net/tcp 略贵，节流）
        now = time.monotonic()
        tcp = self._tcp_cache[1]
        if now - self._tcp_cache[0] >= self.s.tcp_refresh:
            counts: dict[str, int] = {}
            try:
                for conn in psutil.net_connections(kind="tcp"):
                    counts[conn.status] = counts.get(conn.status, 0) + 1
            except Exception:  # noqa: BLE001 - 权限不足时降级为空
                counts = {}
            tcp = counts
            self._tcp_cache = (now, tcp)

        return {
            "interfaces": ifaces,
            "tcp": tcp,
            "tcp_total": sum(tcp.values()),
        }

    def _procs(self) -> dict[str, Any]:
        rows = []
        threads = 0
        total_mem = float(_safe(lambda: psutil.virtual_memory().total, 0) or 0) or 1.0
        for p in psutil.process_iter(
            ["pid", "name", "username", "status", "memory_info", "num_threads", "create_time", "cmdline"]
        ):
            try:
                info = p.info
                pid = info["pid"]
                cpu = p.cpu_percent(None)
                rss = float(info["memory_info"].rss) if info.get("memory_info") else 0.0
                threads += int(info.get("num_threads") or 0)
                cmd = " ".join(info.get("cmdline") or [])[:180]
                rows.append({
                    "pid": pid,
                    "name": info.get("name") or "?",
                    "user": info.get("username") or "",
                    "status": info.get("status") or "",
                    "cpu": round(cpu, 1),
                    "rss": rss,
                    "mem_pct": round(100.0 * rss / total_mem, 3),
                    "threads": int(info.get("num_threads") or 0),
                    "cmd": cmd,
                })
            except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess):
                continue
            except Exception:  # noqa: BLE001
                continue

        by_cpu = sorted(rows, key=lambda r: r["cpu"], reverse=True)[: self.s.top_processes]
        by_mem = sorted(rows, key=lambda r: r["rss"], reverse=True)[: self.s.top_processes]
        return {
            "procs": by_cpu,
            "procs_mem": by_mem,
            "count": len(rows),
            "threads": threads,
        }

    def _system(self, stat: dict[str, float]) -> dict[str, Any]:
        now = time.time()
        uptime = max(0.0, now - self._boot_time)

        file_nr = _read_text(os.path.join(self._proc_root, "sys/fs/file-nr")).split()
        fd_used = fd_max = 0
        if len(file_nr) >= 3:
            fd_used = max(0, int(file_nr[0]) - int(file_nr[1]))
            fd_max = int(file_nr[2])
        entropy = 0.0
        try:
            entropy = float(_read_text(os.path.join(self._proc_root, "sys/kernel/random/entropy_avail")).strip())
        except ValueError:
            entropy = 0.0

        return {
            "uptime": uptime,
            "boot_time": self._boot_time,
            "procs_running": int(stat.get("procs_running", 0)),
            "procs_blocked": int(stat.get("procs_blocked", 0)),
            "processes_started": int(stat.get("processes", 0)),
            "fd_used": fd_used,
            "fd_max": fd_max,
            "entropy": entropy,
            "hostname": os.uname().nodename,
            "kernel": os.uname().release,
        }

    # ---------- 对外入口 ----------

    def sample(self) -> dict[str, Any]:
        now = time.monotonic()
        dt = max(1e-6, now - self._prev_ts)
        self._prev_ts = now

        stat = self._proc_stat_fields()
        cpu = self._cpu(dt)
        mem = self._mem()
        disk = self._disk(dt)
        net = self._net(dt)
        procs = self._procs()
        system = self._system(stat)

        return {
            "ts": time.time(),
            "host": {
                "hostname": system["hostname"],
                "kernel": system["kernel"],
                "label": self.s.host_label,
                "uptime": system["uptime"],
                "boot_time": system["boot_time"],
            },
            "cpu": cpu,
            "mem": mem,
            "disk": disk,
            "net": net,
            "procs": procs["procs"],
            "procs_mem": procs["procs_mem"],
            "sys": {
                "procs": procs["count"],
                "threads": procs["threads"],
                "procs_running": system["procs_running"],
                "procs_blocked": system["procs_blocked"],
                "fd_used": system["fd_used"],
                "fd_max": system["fd_max"],
                "entropy": system["entropy"],
                "processes_started": system["processes_started"],
            },
        }

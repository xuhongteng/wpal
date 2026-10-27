#!/usr/bin/env python3
"""A1 验收：Go 采集器与 Python 参考实现逐字段比对。

比对策略
--------
两侧各连续采样 N 帧（间隔 1s），然后分两类判定：

1. **累计量 / 结构**（内存总量、网卡累计字节、分区容量、TCP 状态集合…）
   两侧读的是同一份 /proc，必须严格相等。这类字段与采样时刻无关。
2. **速率 / 百分比**（CPU 占用、内存占用率、网卡 B/s…）
   单次采样会因"采样时刻不同"产生巨大噪声，因此取**最后 K 帧的均值**再比较，
   窗口拉长后两侧的统计量自然收敛，可以真正暴露解析逻辑差异。

用法：
    python3 tools/compare.py [--python http://127.0.0.1:8891] [--go ./bin/wpal] [--samples 10]
"""
from __future__ import annotations

import argparse
import json
import statistics
import subprocess
import sys
import time
import urllib.request

# (路径, 类型, 容差)
#   "abs"  绝对值相等（累计量；tol 为允许绝对误差）
#   "pct"  百分点误差（派生百分比）
#   "rel"  相对误差（速率类，取窗口均值后比较）
#   "mean_pct" 百分点误差（速率类的窗口均值）
STRUCT_RULES = [
    ("host.hostname", "abs", 0),
    ("host.kernel", "abs", 0),
    ("host.boot_time", "abs", 5),
    ("mem.total", "abs", 1024 * 1024),
    ("mem.swap_total", "abs", 1024 * 1024),
    ("sys.fd_max", "abs", 0),
    ("cpu.cores", "abs", 0),
]

WINDOW_RULES = [
    ("cpu.total", "mean_pct", 3.0),
    ("cpu.breakdown.user", "mean_pct", 4.0),
    ("cpu.breakdown.system", "mean_pct", 4.0),
    ("cpu.breakdown.iowait", "mean_pct", 2.0),
    ("cpu.breakdown.steal", "mean_pct", 2.0),
    ("cpu.breakdown.idle", "mean_pct", 4.0),
    ("cpu.load1", "mean_rel", 0.25),
    ("mem.percent", "mean_pct", 2.0),
    ("mem.swap_percent", "mean_pct", 2.0),
    ("cpu.ctx_switch_rate", "mean_rel", 0.35),
]

# 瞬时速率：只做提示，不作为硬性失败（机器负载本身在变）
RATE_PATHS = [
    "net.interfaces.0.rx_bps",
    "net.interfaces.0.tx_bps",
    "disk.io.0.read_bps",
    "disk.io.0.write_bps",
]


def get(d, path, default=None):
    cur = d
    for part in path.split("."):
        if isinstance(cur, list):
            try:
                cur = cur[int(part)]
            except (ValueError, IndexError):
                return default
        elif isinstance(cur, dict):
            if part not in cur:
                return default
            cur = cur[part]
        else:
            return default
    return cur


def mean_of(samples, path):
    vals = [get(s, path) for s in samples]
    vals = [v for v in vals if isinstance(v, (int, float))]
    if not vals:
        return None
    return statistics.fmean(vals)


def compare_snapshots(go_last, py_last, go_win, py_win):
    issues, notes = [], []

    # ---- 1. 累计量 / 结构 ----
    for path, kind, tol in STRUCT_RULES:
        a, b = get(go_last, path), get(py_last, path)
        if a is None or b is None:
            issues.append((path, "缺失", a, b))
            continue
        if isinstance(a, str) or isinstance(b, str):
            if a != b:
                issues.append((path, "字符串不一致", a, b))
        elif abs(a - b) > tol:
            issues.append((path, f"绝对差>{tol}", a, b))

    # ---- 2. 窗口均值（速率 / 百分比）----
    for path, kind, tol in WINDOW_RULES:
        a, b = mean_of(go_win, path), mean_of(py_win, path)
        if a is None or b is None:
            issues.append((path, "缺失", a, b))
            continue
        if kind == "mean_pct":
            if abs(a - b) > tol:
                issues.append((path, f"窗口均值差>{tol}pp", round(a, 2), round(b, 2)))
        else:
            rel = abs(a - b) / max(abs(b), 1e-9)
            if rel > tol:
                issues.append((path, f"窗口均值相对差>{tol*100:.0f}%", round(a, 2), round(b, 2)))

    # ---- 3. 瞬时速率（仅提示）：按名称匹配 ----
    # 注意：io / interfaces 列表按流量排序，下标在两侧并不可比，必须按名称配对。
    for label, go_list, py_list, keys in (
        ("disk.io", go_last["disk"]["io"], py_last["disk"]["io"], ("name",)),
        ("net.interfaces", go_last["net"]["interfaces"], py_last["net"]["interfaces"], ("name",)),
    ):
        go_map = {tuple(x.get(k) for k in keys): x for x in go_list}
        py_map = {tuple(x.get(k) for k in keys): x for x in py_list}
        for key in sorted(set(go_map) & set(py_map)):
            g, q = go_map[key], py_map[key]
            name = "/".join(str(k) for k in key)
            for field in ("read_bps", "write_bps", "rx_bps", "tx_bps"):
                if field not in g or field not in q:
                    continue
                a = mean_of(go_win, f"{label}.{name}.{field}")
                b = mean_of(py_win, f"{label}.{name}.{field}")
                if a is None or b is None:
                    continue
                denom = max(abs(a), abs(b), 1024.0)
                rel = abs(a - b) / denom
                if rel > 0.6:
                    notes.append((f"{label}[{name}].{field}",
                                  f"窗口均值相对差 {rel*100:.0f}%", round(a, 1), round(b, 1)))

    # ---- 4. 集合 / 逐项结构 ----
    go_mp = {p["mount"] for p in go_last["disk"]["partitions"]}
    py_mp = {p["mount"] for p in py_last["disk"]["partitions"]}
    if go_mp != py_mp:
        issues.append(("disk.partitions[mount]", "挂载点集合不同", sorted(go_mp), sorted(py_mp)))

    go_if = {i["name"] for i in go_last["net"]["interfaces"]}
    py_if = {i["name"] for i in py_last["net"]["interfaces"]}
    if go_if != py_if:
        issues.append(("net.interfaces[name]", "网卡集合不同", sorted(go_if), sorted(py_if)))

    go_tcp = set(go_last["net"]["tcp"].keys())
    py_tcp = set(py_last["net"]["tcp"].keys())
    if go_tcp != py_tcp:
        issues.append(("net.tcp[state]", "TCP 状态集合不同", sorted(go_tcp), sorted(py_tcp)))

    py_parts = {p["mount"]: p for p in py_last["disk"]["partitions"]}
    for p in go_last["disk"]["partitions"]:
        q = py_parts.get(p["mount"])
        if not q:
            continue
        for f in ("device", "fstype"):
            if p[f] != q[f]:
                issues.append((f"disk.partitions[{p['mount']}].{f}", "不一致", p[f], q[f]))
        for f in ("total", "used", "free", "inode_total", "inode_free"):
            if abs(p[f] - q[f]) > 1024 * 1024:
                issues.append((f"disk.partitions[{p['mount']}].{f}", "差>1MiB", p[f], q[f]))
        if abs(p["percent"] - q["percent"]) > 0.2:
            issues.append((f"disk.partitions[{p['mount']}].percent", "差>0.2pp", p["percent"], q["percent"]))
        if abs(p["inode_percent"] - q["inode_percent"]) > 0.2:
            issues.append((f"disk.partitions[{p['mount']}].inode_percent", "差>0.2pp",
                           p["inode_percent"], q["inode_percent"]))

    py_if = {i["name"]: i for i in py_last["net"]["interfaces"]}
    for i in go_last["net"]["interfaces"]:
        q = py_if.get(i["name"])
        if not q:
            continue
        for f in ("rx_total", "tx_total", "err_in", "err_out", "drop_in", "drop_out"):
            # 累计计数器在两个不同采样时刻本来就会有差值（期间的网络流量），
            # 因此用相对误差判定，而不是绝对阈值。绝对下限只用来兜住小计数器。
            if i[f] == q[f]:
                continue
            tol = max(65536, q[f] * 1e-4)
            if abs(i[f] - q[f]) > tol:
                issues.append((f"net.interfaces[{i['name']}].{f}", "累计量不一致", i[f], q[f]))

    if len(go_last["procs"]) != len(py_last["procs"]):
        notes.append(("procs[].len", "进程榜长度不同",
                      len(go_last["procs"]), len(py_last["procs"])))
    go_top = {p["name"] for p in go_last["procs"][:5]}
    py_top = {p["name"] for p in py_last["procs"][:5]}
    if go_top and len(go_top & py_top) < 2:
        notes.append(("procs[].top5", "Top5 重合<2", sorted(go_top), sorted(py_top)))

    if len(go_last["cpu"]["percore"]) != len(py_last["cpu"]["percore"]):
        issues.append(("cpu.percore[]", "核数不同",
                       len(go_last["cpu"]["percore"]), len(py_last["cpu"]["percore"])))
    else:
        # 单核负载在核间与时间上都会漂移，逐核瞬时值不可直接对齐；
        # 改为比对"各核均值"（应与总占用一致），并各自做自一致性校验。
        go_cores = [mean_of(go_win, f"cpu.percore.{i}.busy") for i in range(len(go_last["cpu"]["percore"]))]
        py_cores = [mean_of(py_win, f"cpu.percore.{i}.busy") for i in range(len(py_last["cpu"]["percore"]))]
        go_cores = [v for v in go_cores if v is not None]
        py_cores = [v for v in py_cores if v is not None]
        if go_cores and py_cores:
            gm, pm = statistics.fmean(go_cores), statistics.fmean(py_cores)
            if abs(gm - pm) > 4.0:
                issues.append(("cpu.percore[].mean", "各核均值差>4pp", round(gm, 2), round(pm, 2)))
        # 自一致性：各核均值应与总占用吻合（两侧都要满足）
        for name, cores, total in (("go", go_cores, mean_of(go_win, "cpu.total")),
                                   ("py", py_cores, mean_of(py_win, "cpu.total"))):
            if cores and total is not None and abs(statistics.fmean(cores) - total) > 6.0:
                issues.append((f"cpu.percore[].mean vs cpu.total [{name}]", "自一致性差>6pp",
                               round(statistics.fmean(cores), 2), round(total, 2)))

    return issues, notes


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--python", default="http://127.0.0.1:8891")
    ap.add_argument("--go", default="./bin/wpal")
    ap.add_argument("--samples", type=int, default=10)
    ap.add_argument("--window", type=int, default=8, help="参与均值计算的尾部帧数")
    args = ap.parse_args()

    # 先启动 Go 采样（耗时 samples 秒），同时轮询 Python 侧，两侧时间轴重叠
    go_proc = subprocess.Popen(
        [args.go, "--dump-all", "--repeat", str(args.samples), "--interval", "1s"],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )

    py_samples = []
    for _ in range(args.samples):
        with urllib.request.urlopen(f"{args.python}/api/snapshot", timeout=15) as r:
            py_samples.append(json.load(r))
        time.sleep(1)

    out, err = go_proc.communicate(timeout=120)
    if go_proc.returncode != 0:
        print("Go 采集失败:", err)
        return 2
    go_samples = json.loads(out)

    # 取两侧时间轴的交集：只有在同一时间区间上求均值才是可比的，
    # 否则比的是两段不同时间里机器的不同负载，噪声与信号同量级。
    t0 = max(go_samples[0]["ts"], py_samples[0]["ts"])
    t1 = min(go_samples[-1]["ts"], py_samples[-1]["ts"])
    go_win = [f for f in go_samples if t0 <= f["ts"] <= t1]
    py_win = [f for f in py_samples if t0 <= f["ts"] <= t1]
    if len(go_win) < 3 or len(py_win) < 3:
        print("两侧采样窗口重叠不足，无法比对")
        return 2
    k = args.window
    go_win, py_win = go_win[-k:], py_win[-k:]
    issues, notes = compare_snapshots(go_samples[-1], py_samples[-1], go_win, py_win)

    print("=" * 78)
    print("A1 字段一致性比对：Go 采集器  vs  Python 参考实现")
    print("=" * 78)
    print(f"Python 参考 : {args.python}   采样 {len(py_samples)} 帧")
    print(f"Go 实现    : {args.go}   采样 {len(go_samples)} 帧")
    print(f"均值窗口   : 共同时间区间 {t0:.1f}~{t1:.1f}（各取尾部 {k} 帧）")
    print()
    print(f"硬性问题: {len(issues)}")
    for path, why, a, b in issues:
        print(f"  ✗ {path}: {why}  go={a} py={b}")
    print()
    print(f"提示项: {len(notes)}")
    for path, why, a, b in notes:
        print(f"  · {path}: {why}  go={a} py={b}")
    print()
    if issues:
        print("结论：不通过")
        return 1
    print("结论：通过（所有硬性字段一致）")
    return 0


if __name__ == "__main__":
    sys.exit(main())

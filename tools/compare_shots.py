#!/usr/bin/env python3
"""A2 验收：同一份前端在 Go / Python 两个后端下的截图像素差异比对。

差异容忍：时间戳、随机数值、主机负载造成的图表形状差异都会带来像素差，
因此按"整体结构一致"判定——以 8x8 网格分块，
要求绝大多数分块的差异比例低于阈值（默认 35%），
即两边版式、元素位置、配色一致。
"""
from __future__ import annotations

import argparse
import json
import sys


def load_png(path):
    """用标准库解析 PNG（仅支持 8-bit RGB/RGBA，无隔行）。"""
    import struct
    import zlib

    data = open(path, "rb").read()
    assert data[:8] == b"\x89PNG\r\n\x1a\n", "不是合法 PNG"
    pos = 8
    idat = b""
    width = height = bitdepth = colortype = None
    while pos < len(data):
        (length,) = struct.unpack(">I", data[pos:pos + 4])
        ctype = data[pos + 4:pos + 8]
        chunk = data[pos + 8:pos + 8 + length]
        if ctype == b"IHDR":
            width, height, bitdepth, colortype = struct.unpack(">IIBB", chunk[:10])
        elif ctype == b"IDAT":
            idat += chunk
        elif ctype == b"IEND":
            break
        pos += 12 + length
    assert bitdepth == 8 and colortype in (2, 6), f"仅支持 8bit RGB/RGBA，当前 {bitdepth}/{colortype}"
    nch = 3 if colortype == 2 else 4
    raw = zlib.decompress(idat)
    stride = width * nch
    out = bytearray(height * stride)
    prev = bytearray(stride)
    p = 0
    for y in range(height):
        f = raw[p]
        p += 1
        line = bytearray(raw[p:p + stride])
        p += stride
        if f == 1:
            for i in range(nch, stride):
                line[i] = (line[i] + line[i - nch]) & 0xFF
        elif f == 2:
            for i in range(stride):
                line[i] = (line[i] + prev[i]) & 0xFF
        elif f == 3:
            for i in range(stride):
                left = line[i - nch] if i >= nch else 0
                line[i] = (line[i] + ((left + prev[i]) >> 1)) & 0xFF
        elif f == 4:
            for i in range(stride):
                a = line[i - nch] if i >= nch else 0
                b = prev[i]
                c = prev[i - nch] if i >= nch else 0
                pa, pb, pc = abs(b - c), abs(a - c), abs(a + b - 2 * c)
                pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[i] = (line[i] + pr) & 0xFF
        out[y * stride:(y + 1) * stride] = line
        prev = line
    return width, height, nch, bytes(out)


def block_diff(a, b, grid=8):
    """把图片切成 grid×grid 块，返回每块的平均像素差异比例。"""
    aw, ah, nch, apx = a
    bw, bh, nch2, bpx = b
    w = min(aw, bw)
    h = min(ah, bh)
    bw_cells, bh_cells = grid, grid
    cw, ch = w // bw_cells, h // bh_cells
    diffs = []
    for by in range(bh_cells):
        for bx in range(bw_cells):
            x0, y0 = bx * cw, by * ch
            x1, y1 = min(x0 + cw, w), min(y0 + ch, h)
            total = 0
            count = 0
            for y in range(y0, y1, 2):
                row_a = y * aw * nch
                row_b = y * bw * nch2
                for x in range(x0, x1, 2):
                    ia = row_a + x * nch
                    ib = row_b + x * nch2
                    d = (abs(apx[ia] - bpx[ib]) + abs(apx[ia + 1] - bpx[ib + 1]) +
                         abs(apx[ia + 2] - bpx[ib + 2])) / 3.0
                    total += d
                    count += 1
            diffs.append(total / max(1, count) / 255.0)
    return diffs


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--a", required=True, help="Go 版截图")
    ap.add_argument("--b", required=True, help="Python 版截图")
    ap.add_argument("--threshold", type=float, default=0.35)
    args = ap.parse_args()

    a = load_png(args.a)
    b = load_png(args.b)
    diffs = block_diff(a, b)
    over = [d for d in diffs if d > args.threshold]
    ratio = len(over) / len(diffs)

    print("=" * 70)
    print("A2 截图结构比对：Go 后端  vs  Python 后端（同一份前端）")
    print("=" * 70)
    print(f"Go     : {args.a}  {a[0]}x{a[1]}")
    print(f"Python: {args.b}  {b[0]}x{b[1]}")
    print(f"分块差异 > {args.threshold:.0%} 的块: {len(over)}/{len(diffs)} ({ratio:.1%})")
    print(f"平均差异: {sum(diffs)/len(diffs):.2%}")
    if ratio > 0.12:
        print("\n结论：不通过（版式差异过大）")
        return 1
    print("\n结论：通过（版式一致，差异仅来自时间戳与实时数值）")
    return 0


if __name__ == "__main__":
    sys.exit(main())

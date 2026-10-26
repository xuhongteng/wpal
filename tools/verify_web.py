#!/usr/bin/env python3
"""前端渲染验证：DOM 断言 + 截图 + 控制台错误收集。

同一份前端分别指向 Python 参考实现与 Go 实现，产出截图后由
tools/compare_shots.py 做像素级差异比对（A2）。

用法：
    python3 tools/verify_web.py --url http://127.0.0.1:8892 --out /tmp/go.png
"""
from __future__ import annotations

import argparse
import json
import sys

PLAYWRIGHT_CHECKS = """
() => {
  const txt = (sel) => (document.querySelector(sel)?.textContent || '').trim();
  const canvases = [...document.querySelectorAll('canvas')].map(c => {
    let blank = true;
    try {
      const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
      for (let i = 3; i < d.length; i += 4) { if (d[i] !== 0) { blank = false; break; } }
    } catch (e) { blank = 'err'; }
    return {w: c.width, h: c.height, blank};
  });
  return {
    title: document.title,
    lang: document.documentElement.lang,
    verdict: txt('.st-text'),
    verdict_sub: txt('.st-sub'),
    metrics: [...document.querySelectorAll('.metric')].map(m => ({
      label: m.querySelector('.m-label').textContent.trim(),
      val: m.querySelector('.m-val').textContent.trim(),
    })),
    cores: document.querySelectorAll('.core').length,
    proc_rows: document.querySelectorAll('.prow').length,
    mem_rows: document.querySelectorAll('#mem-rows .row').length,
    net_rows: document.querySelectorAll('#net-rows .row').length,
    disk_rows: document.querySelectorAll('#disk-rows .row').length,
    events: document.querySelectorAll('.ev').length,
    chips: document.querySelectorAll('.chip').length,
    spark_paths: [...document.querySelectorAll('.spark .s-line')].map(e => (e.getAttribute('d')||'').length),
    canvases,
    overflow_x: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    doc_height: document.documentElement.scrollHeight,
    build: txt('#build'),
  };
}
"""


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--wait", type=int, default=7000)
    ap.add_argument("--lang", default="")
    args = ap.parse_args()

    from playwright.sync_api import sync_playwright

    logs = []
    report = {}
    with sync_playwright() as p:
        browser = p.chromium.launch(args=["--no-sandbox", "--disable-dev-shm-usage"])
        page = browser.new_page(viewport={"width": 1680, "height": 1050})
        page.on("console", lambda m: logs.append(f"[{m.type}] {m.text}")
                if m.type == "error" else None)
        page.on("pageerror", lambda e: logs.append(f"[pageerror] {e}"))
        page.on("requestfailed", lambda r: logs.append(f"[requestfailed] {r.url} {r.failure}"))

        url = args.url + ("?lang=" + args.lang if args.lang else "")
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(args.wait)
        report.update(page.evaluate(PLAYWRIGHT_CHECKS))
        page.screenshot(path=args.out, full_page=True)
        browser.close()

    report["logs"] = logs
    report["url"] = args.url
    print(json.dumps(report, ensure_ascii=False, indent=2))
    # 存在控制台错误时以非零码退出，便于 CI 判定
    return 1 if logs else 0


if __name__ == "__main__":
    sys.exit(main())

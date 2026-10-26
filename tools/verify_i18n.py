#!/usr/bin/env python3
"""A7 验收：i18n 完整度与切换行为。

1. 中英字典 key 集合必须完全一致（0 缺失、0 多余）；
2. 英文模式下页面可见文本不得残留中文；
3. 右上角切换按钮能在中英之间来回切换。
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

CJK = re.compile(r"[一-鿿　-〿＀-￯]")


def check_dicts(locales_dir: pathlib.Path):
    zh = json.loads((locales_dir / "zh-CN.json").read_text(encoding="utf-8"))
    en = json.loads((locales_dir / "en-US.json").read_text(encoding="utf-8"))
    zh_only = sorted(set(zh) - set(en))
    en_only = sorted(set(en) - set(zh))
    return {
        "zh_keys": len(zh),
        "en_keys": len(en),
        "zh_only": zh_only,
        "en_only": en_only,
        "ok": not zh_only and not en_only,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", default="http://127.0.0.1:8892")
    ap.add_argument("--locales", default="web/locales")
    ap.add_argument("--out", default="/tmp/i18n")
    args = ap.parse_args()

    issues = []
    dicts = check_dicts(pathlib.Path(args.locales))
    print("=" * 70)
    print("A7 i18n 验收")
    print("=" * 70)
    print(f"字典 key：zh={dicts['zh_keys']} en={dicts['en_keys']}")
    if dicts["zh_only"]:
        issues.append(f"仅中文有的 key: {dicts['zh_only']}")
    if dicts["en_only"]:
        issues.append(f"仅英文有的 key: {dicts['en_only']}")
    print(f"key 集合一致: {'✓' if dicts['ok'] else '✗'}")

    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.launch(args=["--no-sandbox", "--disable-dev-shm-usage"])
        page = browser.new_page(viewport={"width": 1680, "height": 1050})
        errors = []
        page.on("pageerror", lambda e: errors.append(str(e)))
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)

        # 英文模式
        page.goto(args.url + "/?lang=en-US", wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(6000)
        en_text = page.evaluate("document.body.innerText")
        en_html = page.inner_html("body")
        lang_attr = page.get_attribute("html", "lang")

        # 中文模式
        page.goto(args.url + "/?lang=zh-CN", wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(6000)
        zh_text = page.evaluate("document.body.innerText")
        zh_html = page.inner_html("body")

        # 按钮切换
        page.click("#lang-toggle")
        page.wait_for_timeout(1500)
        after_toggle = page.get_attribute("html", "lang")
        after_text = page.evaluate("document.body.innerText")

        browser.close()

    print(f"英文模式 html lang = {lang_attr}")
    en_cjk = sorted(set(CJK.findall(en_text)))
    zh_cjk = sorted(set(CJK.findall(zh_text)))
    print(f"英文模式残留中文（正文）: {''.join(en_cjk) if en_cjk else '无 ✓'}")
    print(f"中文模式中文数量: {len(zh_cjk)} 字种")
    print(f"点击切换后 html lang = {after_toggle}")

    if en_cjk:
        # 允许技术性专有名词残留（如 Swap/inode 不算中文）；任何中文字符都视为残留
        issues.append(f"英文模式残留中文: {''.join(en_cjk)}")
    if not zh_cjk:
        issues.append("中文模式没有任何中文，疑似字典未生效")
    if after_toggle != "en":
        issues.append(f"切换按钮未生效，html lang={after_toggle}（期望 en）")
    if errors:
        issues.append(f"控制台错误: {errors[:3]}")

    print()
    if issues:
        print("结论：不通过")
        for i in issues:
            print(f"  ✗ {i}")
        return 1
    print("结论：通过（字典一致、英文无残留中文、切换生效）")
    return 0


if __name__ == "__main__":
    sys.exit(main())

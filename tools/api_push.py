#!/usr/bin/env python3
"""通过 GitHub Git Data API 推送提交（绕过 git-over-HTTPS 的网络超时）。

用法：
    GITHUB_TOKEN=<pat> python3 tools/api_push.py <base_sha> <commit_sha> [repo]
"""
import base64
import json
import os
import pathlib
import subprocess
import sys
import urllib.request

TOKEN = os.environ.get("GITHUB_TOKEN", "")
REPO = os.environ.get("GITHUB_REPO", "xuhongteng/wpal")
API = f"https://api.github.com/repos/{REPO}"


def api(method, path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(
        API + path,
        data=data,
        method=method,
        headers={
            "Authorization": f"token {TOKEN}",
            "Accept": "application/vnd.github.v3+json",
            "Content-Type": "application/json",
            "User-Agent": "wpal-push",
        },
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        body = r.read().decode()
    return json.loads(body) if body else {}


def main():
    base_sha = sys.argv[1]
    commit_sha = sys.argv[2]

    # 读取 base..commit 之间所有变更文件
    out = subprocess.run(
        ["git", "diff", "--name-status", base_sha, commit_sha],
        capture_output=True, text=True, check=True,
    ).stdout.strip()

    entries = []
    for line in out.splitlines():
        if not line.strip():
            continue
        status, path = line.split("\t", 1)
        mode = subprocess.run(
            ["git", "ls-tree", commit_sha, "--", path],
            capture_output=True, text=True, check=True,
        ).stdout.split()[0]

        raw = pathlib.Path(path).read_bytes()
        blob = api("POST", "/git/blobs", {
            "content": base64.b64encode(raw).decode(),
            "encoding": "base64",
        })
        entries.append({
            "path": path,
            "mode": mode,
            "type": "blob",
            "sha": blob["sha"],
        })
        print(f"blob {path} -> {blob['sha'][:8]}")

    # 基于远程 base 创建新 tree
    tree = api("POST", "/git/trees", {
        "base_tree": base_sha,
        "tree": entries,
    })
    print(f"tree -> {tree['sha']}")

    msg = subprocess.run(
        ["git", "log", "-1", "--format=%B", commit_sha],
        capture_output=True, text=True, check=True,
    ).stdout.strip()

    new_commit = api("POST", "/git/commits", {
        "message": msg,
        "tree": tree["sha"],
        "parents": [base_sha],
    })
    print(f"commit -> {new_commit['sha']}")

    api("PATCH", "/git/refs/heads/main", {
        "sha": new_commit["sha"],
        "force": False,
    })
    print(f"ref main -> {new_commit['sha']}")


if __name__ == "__main__":
    main()

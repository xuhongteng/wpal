"""FastAPI 服务：REST 快照 + 历史 + 事件，WebSocket 实时推送。

对外通过宿主 nginx 以 /monitor/ 前缀暴露，服务本身只监听 127.0.0.1。
"""

from __future__ import annotations

import asyncio
import contextlib
import hashlib
import os
import time
from pathlib import Path
from typing import Any

from fastapi import FastAPI, Query, WebSocket, WebSocketDisconnect
from fastapi.responses import FileResponse, JSONResponse, Response
from fastapi.staticfiles import StaticFiles

from app.alerts import AlertEngine
from app.collector import Collector
from app.config import settings
from app.history import HistoryStore

STATIC_DIR = Path(__file__).resolve().parent / "static"

# 静态资源禁止缓存：FastAPI 默认不发 Cache-Control，浏览器会按 Last-Modified
# 做启发式缓存，导致前端改版后用户仍加载旧 CSS/JS（结构与脚本错配 → 页面崩）。
NO_CACHE = {
    "Cache-Control": "no-cache, no-store, must-revalidate",
    "Pragma": "no-cache",
    "Expires": "0",
}


def build_id() -> str:
    """按静态文件内容算指纹：文件一改，引用 URL 自动变，浏览器必然拉新版。"""
    h = hashlib.sha1()
    for p in sorted(STATIC_DIR.rglob("*")):
        if p.is_file():
            h.update(p.name.encode("utf-8"))
            h.update(p.read_bytes())
    return h.hexdigest()[:8]


BUILD = build_id()


class NoCacheStaticFiles(StaticFiles):
    """带 no-cache 头的静态文件服务。"""

    def file_response(self, *args: Any, **kwargs: Any) -> Response:
        resp = super().file_response(*args, **kwargs)
        resp.headers.update(NO_CACHE)
        return resp

collector = Collector(settings)
history = HistoryStore(settings)
alerts = AlertEngine(settings)


class Hub:
    """极简广播中心：每个客户端一个队列，满则丢旧数据（保实时性）。"""

    def __init__(self) -> None:
        self._clients: set[asyncio.Queue] = set()
        self.latest: dict[str, Any] | None = None

    async def publish(self, payload: dict[str, Any]) -> None:
        self.latest = payload
        for q in list(self._clients):
            try:
                q.put_nowait(payload)
            except asyncio.QueueFull:
                with contextlib.suppress(asyncio.QueueEmpty):
                    q.get_nowait()
                with contextlib.suppress(asyncio.QueueFull):
                    q.put_nowait(payload)

    def subscribe(self) -> asyncio.Queue:
        q: asyncio.Queue = asyncio.Queue(maxsize=2)
        self._clients.add(q)
        return q

    def unsubscribe(self, q: asyncio.Queue) -> None:
        self._clients.discard(q)

    @property
    def client_count(self) -> int:
        return len(self._clients)


hub = Hub()


async def _sampler() -> None:
    """按固定节奏采集 → 判定告警 → 广播 → 定期落盘。"""
    last_persist = 0.0
    while True:
        t0 = time.monotonic()
        try:
            snap = await asyncio.to_thread(collector.sample)
            events = alerts.update(snap)
            for ev in events:
                history.add_event(ev)
            snap["events"] = events
            snap["active_alerts"] = alerts.active_alerts()
            await hub.publish(snap)

            if t0 - last_persist >= settings.persist_interval:
                await asyncio.to_thread(history.persist, snap)
                last_persist = t0
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # noqa: BLE001 - 单次采样失败不能停掉采集循环
            await hub.publish({"type": "error", "message": str(exc), "ts": time.time()})

        elapsed = time.monotonic() - t0
        await asyncio.sleep(max(0.05, settings.sample_interval - elapsed))


async def _janitor() -> None:
    while True:
        await asyncio.sleep(300)
        try:
            await asyncio.to_thread(history.prune)
        except Exception:  # noqa: BLE001
            pass


@contextlib.asynccontextmanager
async def lifespan(app: FastAPI):
    tasks = [asyncio.create_task(_sampler()), asyncio.create_task(_janitor())]
    try:
        yield
    finally:
        for t in tasks:
            t.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await asyncio.gather(*tasks, return_exceptions=True)


app = FastAPI(title="Server Monitor", version="1.0.0", lifespan=lifespan)


@app.get("/api/health")
async def health() -> dict[str, Any]:
    return {
        "ok": True,
        "ts": time.time(),
        "clients": hub.client_count,
        "sample_interval": settings.sample_interval,
        "retention_days": settings.retention_days,
    }


@app.get("/api/snapshot")
async def snapshot() -> dict[str, Any]:
    snap = await asyncio.to_thread(collector.sample)
    snap["events"] = []
    snap["active_alerts"] = alerts.active_alerts()
    return snap


@app.get("/api/history")
async def history_api(range: str = Query("1h", pattern="^(5m|1h|6h|24h|7d)$")) -> dict[str, Any]:
    seconds = settings.history_ranges.get(range, 3600)
    data = await asyncio.to_thread(history.history, seconds)
    data["range"] = range
    return data


@app.get("/api/events")
async def events_api(limit: int = Query(100, ge=1, le=500)) -> dict[str, Any]:
    rows = await asyncio.to_thread(history.recent_events, limit)
    return {"events": rows, "total": history.event_count()}


@app.websocket("/ws/stream")
async def ws_stream(ws: WebSocket) -> None:
    await ws.accept()
    queue = hub.subscribe()
    try:
        if hub.latest is not None:
            await ws.send_json(hub.latest)
        while True:
            payload = await queue.get()
            await ws.send_json(payload)
    except WebSocketDisconnect:
        pass
    except Exception:  # noqa: BLE001
        pass
    finally:
        hub.unsubscribe(queue)


@app.get("/")
async def index() -> Response:
    html = (STATIC_DIR / "index.html").read_text(encoding="utf-8")
    html = html.replace("__BUILD__", BUILD)
    return Response(html, media_type="text/html; charset=utf-8", headers=NO_CACHE)


@app.get("/favicon.ico")
async def favicon() -> JSONResponse:
    return JSONResponse(status_code=204, content=None)


if STATIC_DIR.exists():
    app.mount("/static", NoCacheStaticFiles(directory=str(STATIC_DIR)), name="static")

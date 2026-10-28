package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// wsHandler 负责把采样帧推送给浏览器。
//
// 浏览器通过 /ws/stream 建立连接，之后每秒收到一帧 JSON。
// 服务端不做写缓冲：客户端消费不过来时丢弃旧帧，保证实时性。
type wsHandler struct {
	sampler samplerSource
}

// samplerSource 是 WebSocket 需要的最小采样器接口，便于测试替身注入。
type samplerSource interface {
	Subscribe() (int, chan []byte)
	Unsubscribe(int)
	LatestJSON() []byte
}

// isSameOrigin 检查请求的 Origin 是否与当前 Host 一致，防御跨站 WebSocket 劫持 (CSWSH)。
func isSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// 非浏览器或同源直接发起的请求无 Origin 请求头，直接放行
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// 严格校验 Origin 的 Host 与请求的 Host 一致（忽略大小写）
	return strings.EqualFold(u.Host, r.Host)
}

// ServeWS 处理 WebSocket 升级与推送。
func (h *wsHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	// 1. Origin 跨站校验
	if !isSameOrigin(r) {
		http.Error(w, "Forbidden: Cross-site WebSocket request rejected", http.StatusForbidden)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 先补发当前帧，客户端不用等下一秒
	if raw := h.sampler.LatestJSON(); len(raw) > 0 {
		_ = conn.Write(ctx, websocket.MessageText, raw)
	}

	id, ch := h.sampler.Subscribe()
	defer h.sampler.Unsubscribe(id)

	// 写协程与心跳保活
	done := make(chan struct{})
	go func() {
		defer close(done)
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					return
				}
			case msg, ok := <-ch:
				if !ok {
					return
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, msg)
				writeCancel()
				if err != nil {
					return
				}
			}
		}
	}()

	// 读循环：等待感知客户端断开（移除错误的 60s 读超时，避免无业务帧时人为掐断长连接）
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			break
		}
	}
	cancel()
	<-done
}

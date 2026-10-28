package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"mon/internal/sampler"
)

// wsHandler 负责把采样帧推送给浏览器。
//
// 浏览器通过 /ws/stream 建立连接，之后每秒收到一帧 JSON。
// 服务端只保留每个客户端的最新一帧：消费不过来时旧帧直接被覆盖，
// 既保证实时性，也不会反压采样循环。
type wsHandler struct {
	sampler samplerSource
}

// samplerSource 是 WebSocket 需要的最小采样器接口，便于测试替身注入。
type samplerSource interface {
	Subscribe() (int, *sampler.Subscriber)
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

	id, sub := h.sampler.Subscribe()
	defer h.sampler.Unsubscribe(id)

	// 写协程与心跳保活。
	//
	// 任何异常退出都必须 cancel()：否则读循环会一直阻塞在 conn.Read(ctx)，
	// 直到 TCP 层自己超时才发现连接已死，白白拖住一个 goroutine 和一条连接。
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()

		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.Closed():
				return
			case <-pingTicker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					return
				}
			case <-sub.Notify():
				frame := sub.Take()
				if frame == nil {
					continue
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, frame)
				writeCancel()
				if err != nil {
					return
				}
			}
		}
	}()

	// 读循环：只为感知客户端断开（浏览器不会发业务消息）。
	// 不设超时——浏览器本来就不发帧，设超时只会人为掐断健康的长连接。
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	cancel()
	<-done
}

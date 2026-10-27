package server

import (
	"context"
	"net/http"
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

// ServeWS 处理 WebSocket 升级与推送。
func (h *wsHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// 工具默认只监听回环地址，由反向代理负责 TLS 时同样不需要 Origin 校验。
		InsecureSkipVerify: true,
	})
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

	// 写协程
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
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

	// 读循环：只为感知客户端断开（浏览器不会发业务消息）
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 60*time.Second)
		_, _, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			break
		}
	}
	cancel()
	<-done
}

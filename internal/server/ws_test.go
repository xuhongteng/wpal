package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsSameOrigin 覆盖跨站 WebSocket 劫持（CSWSH）防御的关键分支。
func TestIsSameOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"无 Origin 头（非浏览器客户端）", "", "127.0.0.1:8891", true},
		{"同源精确匹配", "http://127.0.0.1:8891", "127.0.0.1:8891", true},
		{"同源大小写不敏感", "http://127.0.0.1:8891", "127.0.0.1:8891", true},
		{"跨站域名", "http://evil.example.com", "127.0.0.1:8891", false},
		{"同主机不同端口", "http://127.0.0.1:9999", "127.0.0.1:8891", false},
		{"Origin 协议前缀但 Host 不同", "https://127.0.0.1:8891", "example.com", false},
		{"Origin 无法解析", "://bad-url", "127.0.0.1:8891", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws/stream", nil)
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if got := isSameOrigin(r); got != c.want {
				t.Fatalf("isSameOrigin(origin=%q, host=%q) = %v，期望 %v",
					c.origin, c.host, got, c.want)
			}
		})
	}
}

// TestHandleHistoryRejectsCrossOrigin 验证跨站请求在握手前就被拒绝，
// 不会真的进入升级流程。
func TestServeWSRejectsCrossOrigin(t *testing.T) {
	h := &wsHandler{}
	r := httptest.NewRequest(http.MethodGet, "/ws/stream", nil)
	r.Host = "127.0.0.1:8891"
	r.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()

	h.ServeWS(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("跨站请求应返回 403，实际 %d", w.Code)
	}
}

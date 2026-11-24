package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"apitool/server/auth"
)

func randomToken(n int) string { return auth.RandomToken(n) }

// ---- SSE Hub（按用户分频道）----

// hub 事件总线：userID → 订阅者集合。沿用 webui.go 的非阻塞投递策略，
// 消费慢的订阅者丢事件（抓包/日志等高频流宁可丢也不阻塞业务 goroutine）。
type hub struct {
	mu    sync.Mutex
	subs  map[string]map[chan []byte]struct{}
	drops map[string]int // userID → 累计丢弃数（诊断）
}

func newHub() *hub {
	return &hub{subs: map[string]map[chan []byte]struct{}{}, drops: map[string]int{}}
}

// subscribe 注册订阅者，返回取消函数。
func (h *hub) subscribe(userID string) (chan []byte, func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan []byte]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	off := func() {
		h.mu.Lock()
		if set, ok := h.subs[userID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, userID)
			}
		}
		h.mu.Unlock()
	}
	return ch, off
}

// push 向指定用户的所有订阅者推送（JSON 已在调用方序列化）。
func (h *hub) push(userID string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[userID] {
		select {
		case ch <- msg:
		default:
			h.drops[userID]++
		}
	}
}

// pushUserEvent 序列化并推送一个 Wails 风格事件到用户频道。
func (h *hub) pushUserEvent(userID string, event string, data []interface{}) {
	var payload interface{}
	switch len(data) {
	case 0:
		payload = nil
	case 1:
		payload = data[0]
	default:
		payload = data
	}
	msg, err := json.Marshal(map[string]interface{}{"event": event, "data": payload})
	if err != nil {
		return
	}
	h.push(userID, msg)
}

// pushAll 向所有在线用户广播（系统级公告用）。
func (h *hub) pushAll(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for uid := range h.subs {
		for ch := range h.subs[uid] {
			select {
			case ch <- msg:
			default:
				h.drops[uid]++
			}
		}
	}
}

// subscriberCount 在线订阅者总数（诊断）。
func (h *hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for uid := range h.subs {
		n += len(h.subs[uid])
	}
	return n
}

// serveSSE 处理 /events SSE 长连接（userID 来自中间件注入的上下文）。
func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, userID string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE 不支持", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, off := s.hub.subscribe(userID)
	defer off()

	// 建立即发一条注释（防止代理缓冲）与 retry 提示
	_, _ = w.Write([]byte(": connected\nretry: 3000\n\n"))
	fl.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			fl.Flush()
		case msg := <-ch:
			if _, err := w.Write([]byte("data: " + string(msg) + "\n\n")); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

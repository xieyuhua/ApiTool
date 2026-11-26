package agent

// SSE 事件广播：Agent 运行时的流式输出（delta/step/thinking/usage…）通过这里
// 推给所有已连接的浏览器。单租户场景下不需要按用户分频道。

import (
	"encoding/json"
	"sync"
)

// hub 维护所有 SSE 订阅者。发送缓冲区满说明该客户端过慢，直接丢弃该条
// （与 server/app/hub.go 的非阻塞丢弃策略一致），避免拖慢整个 Agent 运行。
type hub struct {
	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	closed bool
}

func newHub() *hub {
	return &hub{subs: map[chan []byte]struct{}{}}
}

// subscribe 返回事件通道与取消函数。
func (h *hub) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// broadcast 向所有订阅者推送一条事件。
func (h *hub) broadcast(event string, data ...interface{}) {
	// 与 Wails 的 Emit(event, data ...interface{}) 一致：data 为空时传 null，
	// 单元素时直接传该元素，多元素时传数组。前端 EventsOn 的回调据此取参。
	var payload interface{}
	switch len(data) {
	case 0:
		payload = nil
	case 1:
		payload = data[0]
	default:
		payload = data
	}
	raw, err := json.Marshal(map[string]interface{}{"event": event, "data": payload})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- raw:
		default: // 客户端过慢：丢弃本条
		}
	}
}

// subscriberCount 当前订阅者数量（健康检查用）。
func (h *hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// close 关闭所有订阅（服务停止时调用）。
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.subs {
		close(ch)
	}
	h.subs = map[chan []byte]struct{}{}
}

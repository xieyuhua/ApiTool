package agent

// 对话接口：RunAgent（含流式事件）与 PolishText。

import (
	"net/http"

	agentcore "apitool/core/agent"
)

// handleRun 执行一次完整 Agent 对话。
// 流式过程通过 SSE（/api/agent/events）推送，本接口只返回最终结果。
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var args agentcore.RunAgentArgs
	if err := decodeJSON(r, &args); err != nil {
		writeErr(w, http.StatusBadRequest, "请求解析失败: "+err.Error())
		return
	}
	if args.BaseURL == "" {
		// 未显式传 AI 接口配置时，回退到服务端保存的设置
		st := s.rt.ReadData().Settings
		args.BaseURL = st.AIBaseURL
		args.APIKey = st.AIKey
		if args.Model == "" {
			args.Model = st.AIModel
		}
		if args.Timeout <= 0 {
			args.Timeout = st.TimeoutSec
		}
	}
	writeOK(w, s.core.RunAgent(args))
}

// handlePolish AI 润色输入文本。
func (s *Server) handlePolish(w http.ResponseWriter, r *http.Request) {
	var args agentcore.RunAgentArgs
	if err := decodeJSON(r, &args); err != nil {
		writeErr(w, http.StatusBadRequest, "请求解析失败: "+err.Error())
		return
	}
	if args.BaseURL == "" {
		st := s.rt.ReadData().Settings
		args.BaseURL = st.AIBaseURL
		args.APIKey = st.AIKey
		if args.Model == "" {
			args.Model = st.AIModel
		}
		if args.Timeout <= 0 {
			args.Timeout = st.TimeoutSec
		}
	}
	out, err := s.core.PolishText(args)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, out)
}

// handleEvents SSE 事件流：Agent 运行过程中的 delta/step/thinking/usage 等。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "当前服务器不支持 SSE")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, cancel := s.hub.subscribe()
	defer cancel()

	// 建立即发注释（防代理缓冲）与重连间隔
	_, _ = w.Write([]byte(": connected\nretry: 3000\n\n"))
	fl.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return // 服务关闭
			}
			if _, err := w.Write([]byte("data: " + string(msg) + "\n\n")); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

package agent

// HTTP 服务装配：路由 + 中间件 + 静态 UI。

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	agentcore "apitool/core/agent"
)

// Server 独立 Agent 服务。
type Server struct {
	cfg     *Config
	rt      *Runtime
	hub     *hub
	core    *agentcore.Manager
	version string
	mux     *http.ServeMux
}

// New 创建服务实例。
func New(cfg *Config, version string) (*Server, error) {
	h := newHub()
	rt, err := newRuntime(cfg, h, version)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:     cfg,
		rt:      rt,
		hub:     h,
		core:    agentcore.NewManager(rt, rt),
		version: version,
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

// Handler 返回已装配中间件的处理器。
func (s *Server) Handler() http.Handler {
	return s.withCommonHeaders(s.mux)
}

// ListenAndServe 启动 HTTP 服务。
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("ApiTool Agent 独立服务已启动：%s（db=%s）", s.cfg.Addr, s.cfg.DBType)
	log.Printf("访问地址：%s", s.tokenLink(displayHost(s.cfg.Addr)))
	if s.cfg.Token == "" {
		log.Printf("⚠️ 未设置 -token：仅允许本机访问。对外提供服务请务必配置令牌。")
	}
	return srv.ListenAndServe()
}

// Close 释放资源。
func (s *Server) Close() error {
	if s.hub != nil {
		s.hub.close()
	}
	if s.rt != nil {
		return s.rt.Close()
	}
	return nil
}

// routes 注册全部路由。
func (s *Server) routes() {
	m := s.mux

	// 健康检查（无需令牌，便于容器探活）
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "version": s.version, "subscribers": s.hub.subscriberCount(),
			"authRequired": s.cfg.Token != "",
		})
	})

	auth := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }

	// 数据
	m.Handle("GET /api/agent/data", auth(s.handleLoadData))
	m.Handle("GET /api/agent/bootstrap", auth(s.handleBootstrap))
	m.Handle("PUT /api/agent/config", auth(s.handleSaveConfig))
	m.Handle("PUT /api/agent/skills", auth(s.handleSaveSkills))
	m.Handle("PUT /api/agent/mcpservers", auth(s.handleSaveMCPServers))
	m.Handle("PUT /api/agent/users", auth(s.handleSaveUsers))

	// 会话
	m.Handle("POST /api/agent/sessions", auth(s.handleCreateSession))
	m.Handle("POST /api/agent/sessions/{id}/switch", auth(s.handleSwitchSession))
	m.Handle("PATCH /api/agent/sessions/{id}", auth(s.handleRenameSession))
	m.Handle("DELETE /api/agent/sessions/{id}", auth(s.handleDeleteSession))
	m.Handle("POST /api/agent/messages/clear", auth(s.handleClearMessages))
	m.Handle("GET /api/agent/sessions/{id}/html", auth(s.handleSessionHTML))
	m.Handle("GET /api/agent/sessions/{id}/export", auth(s.handleSessionExport))

	// 对话
	m.Handle("POST /api/agent/run", auth(s.handleRun))
	m.Handle("POST /api/agent/polish", auth(s.handlePolish))

	// MCP / 工具
	m.Handle("GET /api/agent/tools", auth(s.handleBuiltinTools))
	m.Handle("GET /api/agent/mcp/tools", auth(s.handleMCPTools))
	m.Handle("POST /api/agent/mcp/test", auth(s.handleMCPTest))

	// 日志
	m.Handle("GET /api/agent/logs", auth(s.handleQueryLogs))
	m.Handle("GET /api/agent/logs/facets", auth(s.handleLogFacets))
	m.Handle("GET /api/agent/logs/{id}", auth(s.handleGetLog))
	m.Handle("DELETE /api/agent/logs", auth(s.handleClearLogs))

	// SSE 事件流
	m.Handle("GET /api/agent/events", auth(s.handleEvents))

	// 静态 UI（兜底，放最后）
	s.mountUI(m)
}

// withCommonHeaders 统一响应头与 panic 兜底。
func (s *Server) withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v %s %s", rec, r.Method, r.URL.Path)
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "服务内部错误"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- 公共工具 ----

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, result interface{}) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "result": result})
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"ok": false, "error": msg})
}

// decodeJSON 解析请求体（容忍空体）。
func decodeJSON(r *http.Request, v interface{}) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// displayHost 把监听地址转成可点击的展示地址。
func displayHost(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	return addr
}

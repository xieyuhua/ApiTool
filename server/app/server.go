package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"apitool/internal/model"
	"apitool/server/auth"
	"apitool/server/store"
)

// Server ApiTool 服务端。
type Server struct {
	cfg      *Config
	assets   fs.FS // 网页端静态资源（frontend/dist 的 embed.FS；nil=纯 API）
	db       *store.DB
	jwt      *auth.Manager
	hub      *hub
	sessions *sessionManager
	version  string
	mux      *http.ServeMux
}

// New 装配服务端。webAssets 为内嵌的网页端资源（embed.FS of frontend/dist），
// 传 nil 表示纯 API 服务端。
func New(cfg *Config, version string, webAssets fs.FS) (*Server, error) {
	db, err := store.Open(cfg.DBType, cfg.DSN)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:      cfg,
		assets:   webAssets,
		db:       db,
		jwt:      auth.NewManager(cfg.JWTSecret),
		hub:      newHub(),
		sessions: newSessionManager(nil, cfg.SessionTTL),
		version:  version,
	}
	s.sessions.s = s // 打破循环：sessionManager 需要 Server 引用
	s.buildRoutes()
	return s, nil
}

// Close 释放资源。
func (s *Server) Close() error { return s.db.Close() }

// ListenAndServe 启动 HTTP 服务。
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("ApiTool 服务端已启动：%s（db=%s, register=%v, plugins=%v, agent=%v）",
		s.cfg.Addr, s.cfg.DBType, s.cfg.AllowRegister, s.cfg.EnablePlugins, s.cfg.EnableAgent)
	return srv.ListenAndServe()
}

// buildRoutes 注册全部路由。
func (s *Server) buildRoutes() {
	m := http.NewServeMux()
	s.mux = m

	// 鉴权 API（匿名可访问）
	m.HandleFunc("POST /api/register", s.handleRegister)
	m.HandleFunc("POST /api/login", s.handleLogin)
	m.HandleFunc("POST /api/token/refresh", s.handleRefresh)

	// 需登录 API
	m.Handle("GET /api/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	m.Handle("GET /api/projects", s.requireAuth(http.HandlerFunc(s.handleProjectsList)))
	m.Handle("POST /api/projects", s.requireAuth(http.HandlerFunc(s.handleProjectCreate)))
	m.Handle("GET /api/projects/{id}", s.requireAuth(http.HandlerFunc(s.handleProjectGet)))
	m.Handle("PUT /api/projects/{id}", s.requireAuth(http.HandlerFunc(s.handleProjectPut)))
	m.Handle("DELETE /api/projects/{id}", s.requireAuth(http.HandlerFunc(s.handleProjectDelete)))
	m.Handle("GET /api/teams", s.requireAuth(http.HandlerFunc(s.handleTeamsList)))
	m.Handle("POST /api/teams", s.requireAuth(http.HandlerFunc(s.handleTeamCreate)))
	m.Handle("GET /api/teams/{id}/members", s.requireAuth(http.HandlerFunc(s.handleTeamMembers)))
	m.Handle("POST /api/teams/{id}/members", s.requireAuth(http.HandlerFunc(s.handleTeamAddMember)))
	m.Handle("DELETE /api/teams/{id}/members/{uid}", s.requireAuth(http.HandlerFunc(s.handleTeamRemoveMember)))
	m.Handle("PUT /api/projects/{id}/share", s.requireAuth(http.HandlerFunc(s.handleProjectShare)))
	m.Handle("DELETE /api/projects/{id}/share/{teamID}", s.requireAuth(http.HandlerFunc(s.handleProjectUnshare)))

	// RPC 与事件（需登录；RPC 反射调用 appcore 会话）
	m.Handle("POST /rpc/", s.requireAuth(http.HandlerFunc(s.handleRPC)))
	m.Handle("GET /events", s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveSSE(w, r, userIDOf(r.Context()))
	})))

	// 静态资源与 SPA（网页端主应用；登录态由前端处理，无需服务端门禁）
	s.mountStatic(m)

	// 健康检查
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "version": s.version, "sessions": s.sessions.Count(), "subscribers": s.hub.subscriberCount(),
		})
	})
}

// ---- 中间件 ----

type ctxKey string

const uidKey ctxKey = "uid"
const unameKey ctxKey = "uname"

// userIDOf 从请求上下文取用户 ID。
func userIDOf(ctx context.Context) string {
	v, _ := ctx.Value(uidKey).(string)
	return v
}

// usernameOf 从请求上下文取用户名。
func usernameOf(ctx context.Context) string {
	v, _ := ctx.Value(unameKey).(string)
	return v
}

// tokenOf 依次从 Authorization Header / Cookie 提取令牌。
func tokenOf(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(auth.CookieName); err == nil {
		return c.Value
	}
	return ""
}

// requireAuth JWT 鉴权中间件。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := tokenOf(r)
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "未登录")
			return
		}
		claims, err := s.jwt.Verify(tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "登录已过期，请重新登录")
			return
		}
		ctx := context.WithValue(r.Context(), uidKey, claims.UserID)
		ctx = context.WithValue(ctx, unameKey, claims.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- 响应辅助 ----

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr 统一错误响应 {"ok":false,"error":"..."}。
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"ok": false, "error": msg})
}

// writeOK 统一成功响应 {"ok":true,"result":...}。
func writeOK(w http.ResponseWriter, result interface{}) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "result": result})
}

// conflictErr 判断是否乐观锁冲突。
func isConflict(err error) bool {
	_, ok := err.(*store.ConflictError)
	return ok
}

var _ = model.AppData{} // 占位

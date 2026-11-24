package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"apitool/server/auth"
	"apitool/server/store"
)

// ---- 鉴权 API ----

type registerReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResp struct {
	AccessToken  string            `json:"accessToken"`
	RefreshToken string            `json:"refreshToken"`
	User         userInfo          `json:"user"`
	Projects     []store.ProjectMeta `json:"projects"`
}

type userInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// handleRegister 注册（受 -allow-register 开关控制）。
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowRegister {
		writeErr(w, http.StatusForbidden, "该服务端未开放注册，请联系管理员")
		return
	}
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	req.Username = trimName(req.Username)
	if len(req.Username) < 2 || len(req.Username) > 32 {
		writeErr(w, http.StatusBadRequest, "用户名长度需在 2-32 之间")
		return
	}
	if len(req.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "密码长度至少 6 位")
		return
	}
	if _, err := s.db.UserByName(req.Username); err == nil {
		writeErr(w, http.StatusConflict, "用户名已存在")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "查询用户失败")
		return
	}
	salt, hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	uid := store.NewID("u")
	if err := s.db.CreateUser(store.UserRow{ID: uid, Username: req.Username, Salt: salt, PwdHash: hash}); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	// 自动创建个人团队
	if _, err := s.db.CreateTeam(store.TeamRow{Name: req.Username + " 的团队", OwnerID: uid}); err != nil {
		_ = err // 非致命
	}
	s.issueSession(w, uid, req.Username)
}

// handleLogin 登录。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	u, err := s.db.UserByName(trimName(req.Username))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询用户失败")
		return
	}
	if !auth.VerifyPassword(req.Password, u.Salt, u.PwdHash) {
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	s.issueSession(w, u.ID, u.Username)
}

// issueSession 签发 access+refresh 并写 Cookie。
func (s *Server) issueSession(w http.ResponseWriter, userID, username string) {
	access, err := s.jwt.Issue(userID, username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "签发令牌失败")
		return
	}
	refresh := auth.RandomToken(48)
	if err := s.db.SaveRefreshToken(userID, refresh, time.Now().Add(auth.RefreshTTL)); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存刷新令牌失败")
		return
	}
	// HttpOnly Cookie：SSE 与页面刷新兜底
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    access,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(auth.AccessTTL.Seconds()),
		SameSite: http.SameSiteLaxMode,
	})
	ps, _ := s.db.ProjectsOfUser(userID)
	writeJSON(w, http.StatusOK, loginResp{
		AccessToken:  access,
		RefreshToken: refresh,
		User:         userInfo{ID: userID, Username: username},
		Projects:     ps,
	})
}

// handleRefresh 刷新 accessToken（refresh token 轮换）。
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct{ RefreshToken string `json:"refreshToken"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeErr(w, http.StatusBadRequest, "缺少 refreshToken")
		return
	}
	uid, err := s.db.LookupRefreshToken(req.RefreshToken)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "刷新令牌无效或已过期")
		return
	}
	u, err := s.db.UserByID(uid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "用户不存在")
		return
	}
	s.issueSession(w, u.ID, u.Username)
}

// handleMe 当前用户信息（前端启动时探测）。
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	u, err := s.db.UserByID(uid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "用户不存在")
		return
	}
	ps, _ := s.db.ProjectsOfUser(uid)
	writeOK(w, map[string]interface{}{
		"user":     userInfo{ID: u.ID, Username: u.Username},
		"projects": ps,
		"server": map[string]interface{}{
			"version":       s.version,
			"allowRegister": s.cfg.AllowRegister,
			"enablePlugins": s.cfg.EnablePlugins,
			"enableAgent":   s.cfg.EnableAgent,
		},
	})
}

// ---- 项目 REST API（乐观锁）----

// handleProjectsList 项目列表（元数据，不含 data）。
func (s *Server) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	ps, err := s.db.ProjectsOfUser(userIDOf(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取项目列表失败")
		return
	}
	writeOK(w, ps)
}

// handleProjectCreate 新建项目。
func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	uid := userIDOf(r.Context())
	if err := s.db.CreateProject(store.ProjectRow{
		ID: req.ID, OwnerID: uid, Name: req.Name, Data: string(req.Data), Version: 1,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建项目失败: "+err.Error())
		return
	}
	writeOK(w, map[string]interface{}{"id": req.ID, "version": 1})
}

// handleProjectGet 读取项目整包。
func (s *Server) handleProjectGet(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	id := r.PathValue("id")
	role, err := s.db.ProjectRole(id, uid)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取项目失败")
		return
	}
	if role == "" {
		writeErr(w, http.StatusForbidden, "无权访问该项目")
		return
	}
	row, err := s.db.Project(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取项目失败")
		return
	}
	writeOK(w, map[string]interface{}{
		"id": row.ID, "name": row.Name, "data": json.RawMessage(row.Data),
		"version": row.Version, "role": role, "updatedAt": row.UpdatedAt,
	})
}

// handleProjectPut 带乐观锁保存项目。
func (s *Server) handleProjectPut(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	id := r.PathValue("id")
	role, err := s.db.ProjectRole(id, uid)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "校验权限失败")
		return
	}
	if role != store.RoleAdmin && role != store.RoleEditor {
		writeErr(w, http.StatusForbidden, "该账号对此项目为只读权限")
		return
	}
	var req struct {
		Name    string          `json:"name"`
		Data    json.RawMessage `json:"data"`
		Version int64           `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if err := s.db.UpdateProject(id, req.Version, req.Name, string(req.Data)); err != nil {
		if isConflict(err) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	row, _ := s.db.Project(id)
	writeOK(w, map[string]interface{}{"version": row.Version, "updatedAt": row.UpdatedAt})
}

// handleProjectDelete 删除项目（仅 owner/admin）。
func (s *Server) handleProjectDelete(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	id := r.PathValue("id")
	role, err := s.db.ProjectRole(id, uid)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "校验权限失败")
		return
	}
	if role != store.RoleAdmin {
		writeErr(w, http.StatusForbidden, "仅项目管理员可删除")
		return
	}
	if err := s.db.DeleteProject(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeOK(w, nil)
}

// ---- 团队 API ----

// handleTeamsList 用户所在团队。
func (s *Server) handleTeamsList(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	ts, err := s.db.TeamsOfUser(uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取团队失败")
		return
	}
	writeOK(w, ts)
}

// handleTeamCreate 新建团队。
func (s *Server) handleTeamCreate(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || trimName(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "团队名不能为空")
		return
	}
	uid := userIDOf(r.Context())
	t := store.TeamRow{Name: trimName(req.Name), OwnerID: uid}
	id, err := s.db.CreateTeam(t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建团队失败")
		return
	}
	t.ID = id
	writeOK(w, t)
}

// handleTeamMembers 团队成员列表。
func (s *Server) handleTeamMembers(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	teamID := r.PathValue("id")
	role, _ := s.db.TeamRole(teamID, uid)
	if role == "" {
		writeErr(w, http.StatusForbidden, "不是该团队成员")
		return
	}
	ms, err := s.db.TeamMembers(teamID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取成员失败")
		return
	}
	writeOK(w, ms)
}

// handleTeamAddMember 添加成员（仅 admin）。
func (s *Server) handleTeamAddMember(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	teamID := r.PathValue("id")
	role, _ := s.db.TeamRole(teamID, uid)
	if role != store.RoleAdmin {
		writeErr(w, http.StatusForbidden, "仅团队管理员可添加成员")
		return
	}
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || trimName(req.Username) == "" {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Role != store.RoleAdmin && req.Role != store.RoleEditor && req.Role != store.RoleViewer {
		req.Role = store.RoleViewer
	}
	u, err := s.db.UserByName(trimName(req.Username))
	if err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err := s.db.AddTeamMember(teamID, u.ID, req.Role); err != nil {
		writeErr(w, http.StatusInternalServerError, "添加成员失败")
		return
	}
	writeOK(w, nil)
}

// handleTeamRemoveMember 移除成员（仅 admin；不能移除 owner）。
func (s *Server) handleTeamRemoveMember(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	teamID := r.PathValue("id")
	target := r.PathValue("uid")
	role, _ := s.db.TeamRole(teamID, uid)
	if role != store.RoleAdmin {
		writeErr(w, http.StatusForbidden, "仅团队管理员可移除成员")
		return
	}
	// 不能移除 owner
	teams, terr := s.db.TeamsOfUser(uid)
	if terr == nil {
		for _, t := range teams {
			if t.ID == teamID && t.OwnerID == target {
				writeErr(w, http.StatusForbidden, "不能移除团队创建者")
				return
			}
		}
	}
	if err := s.db.RemoveTeamMember(teamID, target); err != nil {
		writeErr(w, http.StatusInternalServerError, "移除失败")
		return
	}
	writeOK(w, nil)
}

// ---- 项目共享 API ----

// handleProjectShare 授权团队访问项目（仅 owner/admin）。
func (s *Server) handleProjectShare(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	id := r.PathValue("id")
	role, _ := s.db.ProjectRole(id, uid)
	if role != store.RoleAdmin {
		writeErr(w, http.StatusForbidden, "仅项目管理员可授权")
		return
	}
	var req struct {
		TeamID string `json:"teamId"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == "" {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Role != store.RoleEditor && req.Role != store.RoleViewer {
		req.Role = store.RoleViewer
	}
	if err := s.db.GrantProject(id, req.TeamID, req.Role); err != nil {
		writeErr(w, http.StatusInternalServerError, "授权失败")
		return
	}
	writeOK(w, nil)
}

// handleProjectUnshare 撤销授权。
func (s *Server) handleProjectUnshare(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	id := r.PathValue("id")
	teamID := r.PathValue("teamID")
	role, _ := s.db.ProjectRole(id, uid)
	if role != store.RoleAdmin {
		writeErr(w, http.StatusForbidden, "仅项目管理员可撤销授权")
		return
	}
	if err := s.db.RevokeProject(id, teamID); err != nil {
		writeErr(w, http.StatusInternalServerError, "撤销失败")
		return
	}
	writeOK(w, nil)
}

func trimName(s string) string {
	// 去首尾空白（含全角空格）
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}

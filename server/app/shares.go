package app

// 网页端分享：文档分享链接的创建 / 列表 / 停止 / 公开访问。
//
// 桌面端的分享服务跑在客户端进程内（internal/share，内存态，退出即失）；
// 服务端模式必须落到数据库，才能跨设备、跨重启可用。分享表沿用既有
// shares 结构（token/project_id/title/html/password/expire_at/created_at）。

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"apitool/internal/appcore"
	"apitool/internal/util"
	"apitool/server/store"
)

// handleShareCreate 创建分享链接（需登录，且对当前项目有编辑以上权限）。
// POST /api/share，body: {dirId, apiId, password, expireMinutes} 或
// {reportJSON, password, expireMinutes}（分享测试报告）。
func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	var req struct {
		DirID         string `json:"dirId"`
		APIID         string `json:"apiId"`
		ReportJSON    string `json:"reportJSON"`
		Password      string `json:"password"`
		ExpireMinutes int    `json:"expireMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	// 分享内容取决于当前项目：取用户会话里的数据渲染
	var title, html string
	var projectID string
	_, err := s.sessions.withLock(uid, usernameOf(r.Context()), func(core *appcore.App) (interface{}, error) {
		var e error
		if strings.TrimSpace(req.ReportJSON) != "" {
			title, html, e = core.RenderTestReportHTML(req.ReportJSON)
		} else {
			title, html, e = core.ShareDocContent(req.DirID, req.APIID)
		}
		if e != nil {
			return nil, e
		}
		projectID = core.CurrentProjectID()
		return nil, nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if projectID == "" {
		writeErr(w, http.StatusBadRequest, "没有可分享的项目")
		return
	}
	role, rerr := s.db.ProjectRole(projectID, uid)
	if rerr != nil || (role != store.RoleAdmin && role != store.RoleEditor) {
		writeErr(w, http.StatusForbidden, "仅项目管理员/编辑者可创建分享")
		return
	}
	token := util.GenID()
	row := store.ShareRow{
		Token:     token,
		ProjectID: projectID,
		Title:     title,
		HTML:      html,
		Password:  strings.TrimSpace(req.Password),
		CreatedAt: time.Now(),
	}
	if req.ExpireMinutes > 0 {
		row.ExpireAt = time.Now().Add(time.Duration(req.ExpireMinutes) * time.Minute)
	}
	if err := s.db.SaveShare(row); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存分享失败")
		return
	}
	writeOK(w, map[string]interface{}{"token": token, "title": title, "link": "/s/" + token})
}

// handleShareList 当前用户的分享列表（仅本人项目）。
func (s *Server) handleShareList(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	metas, err := s.db.ProjectsOfUser(uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取项目失败")
		return
	}
	type item struct {
		Token       string    `json:"token"`
		Title       string    `json:"title"`
		ProjectID   string    `json:"projectId"`
		HasPassword bool      `json:"hasPassword"`
		ExpireAt    int64     `json:"expireAt"`
		CreatedAt   time.Time `json:"createdAt"`
	}
	out := []item{}
	for _, m := range metas {
		rows, err := s.db.SharesOfProject(m.ID)
		if err != nil {
			continue
		}
		for _, sh := range rows {
			out = append(out, item{
				Token: sh.Token, Title: sh.Title, ProjectID: sh.ProjectID,
				HasPassword: sh.Password != "", ExpireAt: sh.ExpireAt.Unix(),
				CreatedAt: sh.CreatedAt,
			})
		}
	}
	writeOK(w, out)
}

// handleShareDelete 停止分享（仅项目管理员）。
func (s *Server) handleShareDelete(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	token := r.PathValue("token")
	sh, err := s.db.Share(token)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "分享不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取分享失败")
		return
	}
	role, _ := s.db.ProjectRole(sh.ProjectID, uid)
	if role != store.RoleAdmin && role != store.RoleEditor {
		writeErr(w, http.StatusForbidden, "仅项目管理员/编辑者可停止分享")
		return
	}
	if err := s.db.DeleteShare(token); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除分享失败")
		return
	}
	writeOK(w, nil)
}

// handleShareView 公开访问分享（无需登录）。路径 /s/{token}，可选 ?pwd= 密码。
func (s *Server) handleShareView(w http.ResponseWriter, r *http.Request) {
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/s/"), "/")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	sh, err := s.db.Share(token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "分享不存在或已被停止", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "读取分享失败", http.StatusInternalServerError)
		return
	}
	if !sh.ExpireAt.IsZero() && time.Now().After(sh.ExpireAt) {
		http.Error(w, "该分享已过期", http.StatusGone)
		return
	}
	if sh.Password != "" {
		got := r.URL.Query().Get("pwd")
		if subtle.ConstantTimeCompare([]byte(got), []byte(sh.Password)) != 1 {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(passwordPage()))
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(sh.HTML))
}

// passwordPage 密码输入页（GET ?pwd= 提交）。
func passwordPage() string {
	return `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>需要密码</title>
<style>body{font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f6f8}
form{background:#fff;padding:28px 32px;border-radius:12px;box-shadow:0 4px 20px rgba(0,0,0,.08)}
input{padding:8px 12px;border:1px solid #dcdfe6;border-radius:6px;font-size:14px;width:220px}
button{padding:8px 18px;border:0;border-radius:6px;background:#165dff;color:#fff;cursor:pointer;margin-left:8px}
h3{margin:0 0 16px;font-size:16px;color:#1f2329}</style></head>
<body><form method="get"><h3>该分享需要访问密码</h3>
<input name="pwd" type="password" placeholder="请输入密码" autofocus/><button type="submit">查看</button></form></body></html>`
}

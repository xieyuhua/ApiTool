package agent

// 会话导出：HTML 预览（内联返回）与文件下载（附件）。
// 桌面端这两个能力走系统保存对话框，独立服务改为浏览器下载。

import (
	"net/http"
	"net/url"
	"strings"
)

// handleSessionHTML 返回会话 HTML 源码（供前端预览）。
func (s *Server) handleSessionHTML(w http.ResponseWriter, r *http.Request) {
	html, err := s.io.BuildSessionHTML(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, html)
}

// handleSessionExport 导出会话为附件（format=html|pdf）。
func (s *Server) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "html"
	}
	name, data, err := s.io.ExportSession(r.PathValue("id"), format)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ct := "text/html; charset=utf-8"
	if strings.HasSuffix(name, ".pdf") {
		ct = "application/pdf"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"apitool/internal/appcore"
)

// handleExport 网页/App 端的统一导出下载通道。
//
// 桌面端导出走系统文件对话框；网页端没有该能力，改为：服务端渲染后以附件返回，
// 浏览器自动下载。kind 决定渲染方式，args 为 JSON 编码的参数。
//
//	GET /export/doc?dirId=&apiId=&format=markdown|html|word|pdf|openapi
//	GET /export/test-report?format=...&report=<urlencoded JSON>
//	GET /export/stress-report?format=...&report=<urlencoded JSON>
//	GET /export/captured-openapi?ids=a,b,c&title=...
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	uid := userIDOf(r.Context())
	q := r.URL.Query()

	var name string
	var data []byte
	var err error

	_, err = s.sessions.withLock(uid, usernameOf(r.Context()), func(core *appcore.App) (interface{}, error) {
		switch r.URL.Path {
		case "/export/doc":
			name, data, err = core.RenderExportContent(q.Get("dirId"), q.Get("apiId"), q.Get("format"))
		case "/export/test-report":
			name, data, err = core.RenderTestReportContent(q.Get("report"), q.Get("format"))
		case "/export/stress-report":
			name, data, err = core.RenderStressReportContent(q.Get("report"), q.Get("format"))
		case "/export/captured-openapi":
			var ids []string
			if raw := strings.TrimSpace(q.Get("ids")); raw != "" {
				ids = strings.Split(raw, ",")
			}
			var text string
			text, err = core.RenderCapturedOpenAPI(ids, q.Get("title"))
			if err == nil {
				title := strings.TrimSpace(q.Get("title"))
				if title == "" {
					title = "接口"
				}
				name, data = title+".openapi.json", []byte(text)
			}
		default:
			err = fmt.Errorf("未知导出类型")
		}
		return nil, err
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "导出内容为空")
		return
	}

	// 文件名按 RFC 5987 编码（含中文）
	if name == "" {
		name = "export"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// exportKindOf 辅助：把前端传来的 kind 映射到路径（保留给未来扩展）。
var _ = json.Marshal

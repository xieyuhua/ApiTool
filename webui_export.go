package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"apitool/internal/agent"
)

// webExportDownload 供局域网网页端下载 Agent 导出的表格文件。
//
// GET /export/download?path=<绝对路径>
//
// 安全约束（必须有，Agent 具备文件读写与命令执行能力）：
//  1. 整个 webui 已被 webAuth 校验访问令牌；
//  2. path 必须位于应用数据目录的 exports 子目录内（ExportFileInDir），
//     否则网页端可借此下载数据库、配置等任意文件。
func (a *App) webExportDownload(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "缺少 path 参数", http.StatusBadRequest)
		return
	}
	if !a.ExportFileInDir(path) {
		http.Error(w, "仅允许下载导出目录（exports）内的文件", http.StatusForbidden)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "文件不存在或无法读取: "+err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "不是可下载的文件", http.StatusBadRequest)
		return
	}
	// 交给浏览器另存；中文文件名用 RFC5987 编码
	name := filepath.Base(path)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	h.Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, f); err != nil {
		fmt.Println("导出文件下载中断:", err)
	}
}

var _ = agent.ExportDirName

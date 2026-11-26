package agent

// Agent 运行日志接口。

import (
	"net/http"
	"strconv"

	agentcore "apitool/core/agent"
)

// handleQueryLogs 按条件查询日志（列表默认不含详情，见 QueryAgentLogsArgs.WithDetail）。
func (s *Server) handleQueryLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	args := agentcore.QueryAgentLogsArgs{
		Keyword:    q.Get("keyword"),
		Level:      q.Get("level"),
		Category:   q.Get("category"),
		Limit:      limit,
		WithDetail: q.Get("withDetail") == "1" || q.Get("withDetail") == "true",
	}
	writeOK(w, s.core.QueryAgentLogs(args))
}

// handleLogFacets 返回日志筛选项（级别/分类的可选值）。
func (s *Server) handleLogFacets(w http.ResponseWriter, r *http.Request) {
	writeOK(w, s.core.GetAgentLogFacets())
}

// handleGetLog 返回单条日志详情。
func (s *Server) handleGetLog(w http.ResponseWriter, r *http.Request) {
	l, err := s.core.GetAgentLog(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeOK(w, l)
}

// handleClearLogs 清空日志。
func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if err := s.core.ClearAgentLogs(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

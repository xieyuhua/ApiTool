package agent

// 会话管理接口。

import (
	"net/http"
)

// handleCreateSession 新建会话，返回新会话 ID。
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	_ = decodeJSON(r, &req)
	writeOK(w, s.core.CreateAgentSession(req.Title))
}

// handleSwitchSession 切换当前会话。
func (s *Server) handleSwitchSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.core.SwitchAgentSession(id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleRenameSession 重命名会话。
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	if err := s.core.RenameAgentSession(r.PathValue("id"), req.Title); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleDeleteSession 删除会话。
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.core.DeleteAgentSession(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleClearMessages 清空当前会话消息。
func (s *Server) handleClearMessages(w http.ResponseWriter, r *http.Request) {
	if err := s.core.ClearAgentMessages(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

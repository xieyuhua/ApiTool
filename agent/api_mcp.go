package agent

// 工具与 MCP 接口。

import (
	"net/http"

	agentcore "apitool/core/agent"
)

// handleBuiltinTools 返回内置工具的静态元信息。
func (s *Server) handleBuiltinTools(w http.ResponseWriter, r *http.Request) {
	writeOK(w, s.core.GetBuiltinTools())
}

// handleMCPTools 列出全部 MCP 工具（连接所有已启用的服务器）。
func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	tools, err := s.core.ListAllMCPTools()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w, tools)
}

// handleMCPTest 测试单个 MCP 服务器连通性并返回其工具列表。
func (s *Server) handleMCPTest(w http.ResponseWriter, r *http.Request) {
	var srv agentcore.MCPServer
	if err := decodeJSON(r, &srv); err != nil {
		writeErr(w, http.StatusBadRequest, "MCP 配置解析失败: "+err.Error())
		return
	}
	tools, err := s.core.TestMCPServer(srv)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w, tools)
}

package agent

// 数据与配置相关接口：Agent 数据加载、配置/技能/MCP 服务器/用户保存、启动引导。

import (
	"net/http"

	agentcore "apitool/core/agent"
)

// handleLoadData 返回 Agent 全量数据（会话、配置、技能、MCP、日志）。
func (s *Server) handleLoadData(w http.ResponseWriter, r *http.Request) {
	writeOK(w, s.core.LoadAgentData())
}

// handleBootstrap 返回「Agent 网页」启动所需的最小数据集。
// 与桌面端 GetAgentBootstrap 一致：AI 接口配置 + 数据库连接列表。
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	d := s.rt.ReadData()
	writeOK(w, map[string]interface{}{
		"settings": map[string]interface{}{
			"aiBaseUrl":  d.Settings.AIBaseURL,
			"aiKey":      d.Settings.AIKey,
			"aiModel":    d.Settings.AIModel,
			"timeoutSec": d.Settings.TimeoutSec,
			"theme":      d.Settings.Theme,
			"accent":     d.Settings.Accent,
		},
		"connections": d.Plugins.Connections,
	})
}

// handleSaveConfig 保存 Agent 运行配置。
func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var cfg agentcore.AgentConfig
	if err := decodeJSON(r, &cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "配置解析失败: "+err.Error())
		return
	}
	if err := s.core.SaveAgentConfig(cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleSaveSkills 保存技能列表。
func (s *Server) handleSaveSkills(w http.ResponseWriter, r *http.Request) {
	var skills []agentcore.AgentSkill
	if err := decodeJSON(r, &skills); err != nil {
		writeErr(w, http.StatusBadRequest, "技能解析失败: "+err.Error())
		return
	}
	if err := s.core.SaveAgentSkills(skills); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleSaveMCPServers 保存 MCP 服务器配置。
func (s *Server) handleSaveMCPServers(w http.ResponseWriter, r *http.Request) {
	var servers []agentcore.MCPServer
	if err := decodeJSON(r, &servers); err != nil {
		writeErr(w, http.StatusBadRequest, "MCP 配置解析失败: "+err.Error())
		return
	}
	if err := s.core.SaveMCPServers(servers); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleSaveUsers 保存 Agent 用户列表。
func (s *Server) handleSaveUsers(w http.ResponseWriter, r *http.Request) {
	var users []agentcore.AgentUser
	if err := decodeJSON(r, &users); err != nil {
		writeErr(w, http.StatusBadRequest, "用户列表解析失败: "+err.Error())
		return
	}
	if err := s.core.SaveAgentUsers(users); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

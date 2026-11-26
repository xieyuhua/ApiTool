package agent

import "time"

// 基础层：配置保存（运行配置 / 技能 / MCP 服务器 / 用户）。
// 只做校验与落库，不含运行逻辑。

// SaveAgentConfig 保存运行配置。
func (m *Manager) SaveAgentConfig(cfg AgentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.ReadAgentData()
	// 合并而非整体覆盖：前端「同步表结构」等场景只回传部分字段（dbSchemas 等），
	// 直接 d.Config = cfg 会把内存中已有的 Tools / SystemPrompt / Mode 等配置清零。
	// 仅当传入字段「非默认零值」时才覆盖，避免配置被局部保存吞掉。
	if cfg.ActiveDBConn != "" || cfg.DBLastDB != nil {
		d.Config.ActiveDBConn = cfg.ActiveDBConn
	}
	if cfg.DBSchemas != nil {
		d.Config.DBSchemas = cfg.DBSchemas
	}
	if cfg.DBSemantics != nil {
		d.Config.DBSemantics = cfg.DBSemantics
	}
	if cfg.DBLastDB != nil {
		d.Config.DBLastDB = cfg.DBLastDB
	}
	if cfg.SystemPrompt != "" {
		d.Config.SystemPrompt = cfg.SystemPrompt
	}
	if cfg.Mode != "" {
		d.Config.Mode = cfg.Mode
	}
	if cfg.MaxLoops > 0 {
		d.Config.MaxLoops = cfg.MaxLoops
	}
	if cfg.ContextLimit > 0 {
		d.Config.ContextLimit = cfg.ContextLimit
	}
	if cfg.Tools.Enabled != nil || cfg.Tools.Desc != nil {
		d.Config.Tools = cfg.Tools
	}
	if cfg.EnableDBAnalysis {
		d.Config.EnableDBAnalysis = cfg.EnableDBAnalysis
	}
	if cfg.Temperature != 0 {
		d.Config.Temperature = cfg.Temperature
	}
	if cfg.MaxTokens > 0 {
		d.Config.MaxTokens = cfg.MaxTokens
	}
	if cfg.MaxToolOutput > 0 {
		d.Config.MaxToolOutput = cfg.MaxToolOutput
	}
	if cfg.MaxFileRead > 0 {
		d.Config.MaxFileRead = cfg.MaxFileRead
	}
	if cfg.ShowThinking {
		d.Config.ShowThinking = cfg.ShowThinking
	}
	if cfg.ShowUsage {
		d.Config.ShowUsage = cfg.ShowUsage
	}
	if cfg.EnablePolish {
		d.Config.EnablePolish = cfg.EnablePolish
	}
	if cfg.EnableChart {
		d.Config.EnableChart = cfg.EnableChart
	}
	return m.WriteAgentData(d)
}

// SaveAgentSkills 覆盖保存技能列表（独立表存储，热加载：保存后立即生效）。
// SaveAgentSkills 覆盖保存技能列表（独立表存储，热加载：保存后立即生效）。
func (m *Manager) SaveAgentSkills(skills []AgentSkill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().Format(time.RFC3339)
	for i := range skills {
		if skills[i].ID == "" {
			skills[i].ID = agentID("skill")
		}
		skills[i].UpdatedAt = now
	}
	if err := m.host.Store().SaveSkills(skills); err != nil {
		return err
	}
	// 同步内存中的 AgentData，保证后续全量备份(meta.agent)包含最新技能。
	d := m.ReadAgentData()
	d.Skills = skills
	return m.WriteAgentData(d)
}

// SaveMCPServers 覆盖保存 MCP 服务器列表。
// SaveMCPServers 覆盖保存 MCP 服务器列表。
func (m *Manager) SaveMCPServers(servers []MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.ReadAgentData()
	now := time.Now().Format(time.RFC3339)
	for i := range servers {
		if servers[i].ID == "" {
			servers[i].ID = agentID("mcp")
		}
		if servers[i].Transport == "" {
			servers[i].Transport = "stdio"
		}
		servers[i].UpdatedAt = now
	}
	d.Servers = servers
	return m.WriteAgentData(d)
}

// SaveAgentUsers 覆盖保存用户列表。
// SaveAgentUsers 覆盖保存用户列表。
func (m *Manager) SaveAgentUsers(users []AgentUser) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.ReadAgentData()
	for i := range users {
		if users[i].ID == "" {
			users[i].ID = agentID("user")
		}
	}
	d.Users = users
	return m.WriteAgentData(d)
}

// ClearAgentMessages 清空当前会话历史。

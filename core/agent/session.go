package agent

import (
	"fmt"
	"time"
)

// 基础层：会话 CRUD 与变更通知。
// 这些方法对应 HTTP API 的会话接口，只做校验 + 读写。

// LoadAgentData 返回 Agent 全部数据（配置、技能、服务器、用户、消息）。日志单独查询。
func (m *Manager) LoadAgentData() AgentData {
	d := m.ReadAgentData()
	// 消息与日志较大，前端首屏只需要少量消息，这里全部返回由前端裁剪
	return d
}

// ClearAgentMessages 清空当前会话历史。
func (m *Manager) ClearAgentMessages() error {
	m.mu.Lock()
	d := m.ReadAgentData()
	if s := d.ActiveSessionPtr(); s != nil {
		s.Messages = []AgentMsg{}
	}
	err := m.WriteAgentData(d)
	m.mu.Unlock()
	m.NotifySessionsChanged("clear", nil)
	return err
}

// CreateAgentSession 新建会话，返回新会话 ID。
// CreateAgentSession 新建会话，返回新会话 ID。
func (m *Manager) CreateAgentSession(title string) string {
	m.mu.Lock()
	d := m.ReadAgentData()
	id := agentID("sess")
	now := time.Now().Format(time.RFC3339)
	if title == "" {
		title = "新会话"
	}
	d.Sessions = append(d.Sessions, AgentSession{ID: id, Title: title, CreatedAt: now, UpdatedAt: now, Messages: []AgentMsg{}})
	d.ActiveSession = id
	_ = m.WriteAgentData(d)
	m.mu.Unlock()
	m.NotifySessionsChanged("create", map[string]interface{}{"sessionId": id, "title": title})
	return id
}

// SwitchAgentSession 切换当前激活会话。
// SwitchAgentSession 切换当前激活会话。
func (m *Manager) SwitchAgentSession(id string) error {
	m.mu.Lock()
	d := m.ReadAgentData()
	if !sessionExists(d, id) {
		m.mu.Unlock()
		return fmt.Errorf("会话不存在")
	}
	d.ActiveSession = id
	_ = m.WriteAgentData(d)
	m.mu.Unlock()
	m.NotifySessionsChanged("switch", map[string]interface{}{"sessionId": id})
	return nil
}

// DeleteAgentSession 删除会话（至少保留一个）。
// DeleteAgentSession 删除会话（至少保留一个）。
func (m *Manager) DeleteAgentSession(id string) error {
	m.mu.Lock()
	d := m.ReadAgentData()
	if len(d.Sessions) <= 1 {
		m.mu.Unlock()
		return fmt.Errorf("至少保留一个会话")
	}
	idx := -1
	for i, s := range d.Sessions {
		if s.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.mu.Unlock()
		return fmt.Errorf("会话不存在")
	}
	d.Sessions = append(d.Sessions[:idx], d.Sessions[idx+1:]...)
	if d.ActiveSession == id {
		d.ActiveSession = d.Sessions[0].ID
	}
	_ = m.WriteAgentData(d)
	m.mu.Unlock()
	m.NotifySessionsChanged("delete", map[string]interface{}{"sessionId": id})
	return nil
}

// RenameAgentSession 重命名会话。
// RenameAgentSession 重命名会话。
func (m *Manager) RenameAgentSession(id, title string) error {
	m.mu.Lock()
	d := m.ReadAgentData()
	for i := range d.Sessions {
		if d.Sessions[i].ID == id {
			d.Sessions[i].Title = title
			d.Sessions[i].UpdatedAt = time.Now().Format(time.RFC3339)
		}
	}
	_ = m.WriteAgentData(d)
	m.mu.Unlock()
	m.NotifySessionsChanged("rename", map[string]interface{}{"sessionId": id, "title": title})
	return nil
}

// ============================ 会话变更通知 ============================

// notifySessionsChanged 广播「会话数据已变更」，所有端（桌面窗口 + 局域网网页端）收到后
// 重新拉取会话列表与当前会话消息。
//
// 为什么必须有它：会话数据是多端共享的同一份（局域网网页与桌面端读写同一个 store），
// 而前端不做轮询。若只靠发起端自己刷新，另一端（例如网页端提问、桌面端旁观）
// 界面会一直停留在旧数据，必须手动刷新页面才能看到。桌面端尤其如此 ——
// 它只在本地 send() 结束后才刷新，后端不推送就完全感知不到远端的变化。
//
// reason 用于区分来源：run（一次对话结束）/ create / delete / rename / switch / clear。
// extra 可携带 clientId、sessionId 等，前端据此判断是否是自己引发的（避免重复刷新）。
func (m *Manager) NotifySessionsChanged(reason string, extra map[string]interface{}) {
	p := map[string]interface{}{"reason": reason}
	for k, v := range extra {
		p[k] = v
	}
	m.emitEvent("agent:sessions-changed", p)
}

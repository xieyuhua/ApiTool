package agent

// 基础层测试：数据读写、会话 CRUD、日志。
//
// 工具层（tools）与引擎层（engine）各有独立测试，分别用 stub 注入，
// 不需要真实存储 —— 这正是分层带来的好处。

import (
	"testing"

	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/storeapi"
)

// stubHost 测试用的最小 Host 实现。
type stubHost struct {
	st *store.Store
}

func (h *stubHost) Store() storeapi.Store   { return h.st }
func (h *stubHost) ReadData() model.AppData { return h.st.GetData() }

// noopEmitter 测试用事件接收器。
type noopEmitter struct{ events []string }

func (e *noopEmitter) Emit(event string, _ ...interface{}) { e.events = append(e.events, event) }

// newTestManager 构造带临时存储的 Manager。
func newTestManager(t *testing.T) (*Manager, *noopEmitter) {
	t.Helper()
	st := store.New(t.TempDir(), "1.0.0", "")
	t.Cleanup(func() { _ = st.Close() })
	em := &noopEmitter{}
	return NewManager(&stubHost{st: st}, em), em
}

func TestSessionLifecycle(t *testing.T) {
	m, em := newTestManager(t)

	d := m.LoadAgentData()
	if len(d.Sessions) == 0 {
		t.Fatal("默认应有至少一个会话")
	}

	id := m.CreateAgentSession("新会话")
	if id == "" {
		t.Fatal("CreateAgentSession 应返回 ID")
	}
	if err := m.RenameAgentSession(id, "改名了"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	if err := m.SwitchAgentSession(id); err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	d = m.LoadAgentData()
	if d.ActiveSession != id {
		t.Fatalf("激活会话应为 %s，实际 %s", id, d.ActiveSession)
	}
	found := false
	for _, s := range d.Sessions {
		if s.ID == id && s.Title == "改名了" {
			found = true
		}
	}
	if !found {
		t.Fatal("重命名未生效")
	}

	if err := m.DeleteAgentSession(id); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	d = m.LoadAgentData()
	if len(d.Sessions) == 0 {
		t.Fatal("删除后应至少保留一个会话")
	}
	// 会话变更应推送事件
	if len(em.events) == 0 {
		t.Fatal("会话变更应广播事件")
	}
}

func TestClearMessagesKeepsSession(t *testing.T) {
	m, _ := newTestManager(t)
	id := m.CreateAgentSession("待清空")
	_ = m.UpdateData(func(d *AgentData) {
		s := FindSession(*d, id)
		s.Messages = append(s.Messages, AgentMsg{ID: "m1", Role: "user", Content: "hi"})
	})
	if err := m.ClearAgentMessages(); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	d := m.LoadAgentData()
	s := FindSession(d, id)
	if s == nil {
		t.Fatal("清空消息不应删除会话")
	}
	if len(s.Messages) != 0 {
		t.Fatalf("消息应被清空，实际 %d 条", len(s.Messages))
	}
}

func TestSaveConfigAndSkills(t *testing.T) {
	m, _ := newTestManager(t)

	cfg := m.AgentConfig()
	cfg.Mode = "plan"
	cfg.MaxLoops = 9
	if err := m.SaveAgentConfig(cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	got := m.AgentConfig()
	if got.Mode != "plan" || got.MaxLoops != 9 {
		t.Fatalf("配置未生效: %+v", got)
	}

	custom := AgentSkill{ID: "sk1", Name: "自定义技能", Prompt: "p", Enabled: true}
	if err := m.SaveAgentSkills([]AgentSkill{custom}); err != nil {
		t.Fatalf("保存技能失败: %v", err)
	}
	d := m.LoadAgentData()
	found := false
	for _, s := range d.Skills {
		if s.ID == "sk1" {
			found = true
		}
	}
	if !found {
		t.Fatal("技能未保存")
	}
}

func TestAppendAndQueryLogs(t *testing.T) {
	m, em := newTestManager(t)

	m.AppendLog(AgentLog{Level: "info", Category: "agent", Title: "测试日志", Detail: "详情内容"})
	if len(em.events) == 0 || em.events[len(em.events)-1] != "agent:log" {
		t.Fatal("写日志应推送 agent:log 事件")
	}

	list := m.QueryAgentLogs(QueryAgentLogsArgs{Limit: 10})
	if len(list) != 1 || list[0].Title != "测试日志" {
		t.Fatalf("查询日志异常: %+v", list)
	}
	// 列表模式默认不带详情（大文本不应在列表里传）
	if list[0].Detail != "" {
		t.Fatal("列表模式不应返回 Detail")
	}

	full, err := m.GetAgentLog(list[0].ID)
	if err != nil {
		t.Fatalf("读日志详情失败: %v", err)
	}
	if full.Detail != "详情内容" {
		t.Fatalf("详情不符: %q", full.Detail)
	}

	if err := m.ClearAgentLogs(); err != nil {
		t.Fatalf("清空日志失败: %v", err)
	}
	if n := len(m.QueryAgentLogs(QueryAgentLogsArgs{Limit: 10})); n != 0 {
		t.Fatalf("清空后应为 0 条，实际 %d", n)
	}
}

func TestUpdateDataIsAtomic(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.UpdateData(func(d *AgentData) { d.Usage.TotalTokens = 42 }); err != nil {
		t.Fatalf("UpdateData 失败: %v", err)
	}
	if got := m.ReadAgentData().Usage.TotalTokens; got != 42 {
		t.Fatalf("写回未生效: %d", got)
	}
}

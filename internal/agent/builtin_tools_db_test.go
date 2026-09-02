package agent

import (
	"strings"
	"testing"

	"apitool/internal/model"
	"apitool/internal/store"
)

// stubHost 是测试用的最小 Host 实现，仅提供存储能力（其余方法返回零值）。
type stubHost struct {
	st *store.Store
}

func (h *stubHost) Store() *store.Store              { return h.st }
func (h *stubHost) ReadData() model.AppData          { return h.st.GetData() }
func (h *stubHost) SaveData(d model.AppData) error    { return h.st.SaveData(d) }
func (h *stubHost) SendRequest(model.RequestSpec) model.ResponseData { return model.ResponseData{} }
func (h *stubHost) AppVersion() string                { return "test" }

// newTestManager 构造带临时存储的 Manager，避免 nil host 导致 panic。
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	st := store.New(dir, "1.0.0", "")
	t.Cleanup(func() { _ = st.Close() })
	return NewManager(&stubHost{st: st}, nil)
}

func TestBuiltinDBSchemaNoConn(t *testing.T) {
	m := newTestManager(t)
	_, err := builtinDBSchema(m, map[string]interface{}{"connId": "nope", "database": "db1"})
	if err == nil {
		t.Fatal("期望连接不可用错误")
	}
}

func TestBuiltinDBQueryNoConn(t *testing.T) {
	m := newTestManager(t)
	_, err := builtinDBQuery(m, map[string]interface{}{"connId": "nope", "database": "db1", "sql": "SELECT 1"})
	if err == nil {
		t.Fatal("期望连接不可用错误")
	}
}

func TestBuiltinDBQueryRejectsWrite(t *testing.T) {
	m := newTestManager(t)
	// 使用不存在的连接，但仍应先被 SQL 前缀拦截（先校验更安全）
	_, err := builtinDBQuery(m, map[string]interface{}{"connId": "x", "database": "d", "sql": "DROP TABLE t"})
	if err == nil {
		t.Fatal("期望被拒绝的写操作错误")
	}
	if strings.Contains(err.Error(), "未找到数据库连接") {
		t.Fatalf("写操作应被 SQL 校验拦截，而非连接查找: %v", err)
	}
}

// TestReadAgentDataSkillsFromIndependentTable 验证独立表存储后技能能正确加载（含预置技能补回）。
func TestReadAgentDataSkillsFromIndependentTable(t *testing.T) {
	m := newTestManager(t)
	// 场景1：全新库，独立表为空，应自动补回预置技能。
	d := m.readAgentData()
	if len(d.Skills) == 0 {
		t.Fatal("独立表为空时，readAgentData 应补回预置技能，但 Skills 为空")
	}
	// 预置技能应已写入独立表并能在后续读取中命中（不再依赖 mergeDefaultSkills）。
	sk := m.host.Store().LoadSkills()
	if len(sk) == 0 {
		t.Fatal("全新库首次读取应把预置技能持久化到独立表，但独立表仍为空")
	}
	// 场景2：再次读取应从独立表命中，且预置技能仍在。
	d2 := m.readAgentData()
	foundBuiltin := false
	for _, s := range d2.Skills {
		if s.Builtin {
			foundBuiltin = true
		}
	}
	if !foundBuiltin {
		t.Fatal("从独立表读取后预置技能应存在，但未找到 Builtin 技能")
	}
}

// TestSaveAndLoadSkills 验证 SaveAgentSkills 写入独立表后，LoadAgentData 能正确读回（含自定义技能）。
func TestSaveAndLoadSkills(t *testing.T) {
	m := newTestManager(t)
	custom := []AgentSkill{
		{ID: "skill-custom", Name: "自定义技能", Description: "测试", Prompt: "请按XX分析", Enabled: true},
	}
	if err := m.SaveAgentSkills(custom); err != nil {
		t.Fatalf("SaveAgentSkills 失败: %v", err)
	}
	d := m.LoadAgentData()
	found := false
	for _, s := range d.Skills {
		if s.ID == "skill-custom" {
			found = true
			if s.Prompt != "请按XX分析" {
				t.Fatalf("技能 prompt 未正确持久化: %q", s.Prompt)
			}
		}
	}
	if !found {
		t.Fatal("保存的自定义技能未在 LoadAgentData 中读回")
	}
	// 独立表应确实包含该技能（验证走的是独立表而非 meta.agent）。
	if sk := m.host.Store().LoadSkills(); len(sk) == 0 {
		t.Fatal("独立表应为非空")
	}
}

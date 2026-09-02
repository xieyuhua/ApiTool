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

// TestExtractBareToolJSON 验证带嵌套 arguments 的裸 JSON 工具调用（如 ｜｜DSML｜｜ 包裹）能被正确解析。
func TestExtractBareToolJSON(t *testing.T) {
	raw := "｜｜DSML｜｜\n{\"action\":\"tool\",\"server\":\"builtin\",\"tool\":\"db_query\",\"arguments\":{\"connId\":\"db_1788254096769\",\"database\":\"diygw\",\"sql\":\"SELECT * FROM diygw_days_erp\"}}\n｜｜DSML｜｜"
	act, ok := parseToolAction(raw)
	if !ok {
		t.Fatal("嵌套 arguments 的裸 JSON 工具调用未被解析")
	}
	if act.Tool != "db_query" {
		t.Fatalf("解析出的工具名错误: %q", act.Tool)
	}
	if act.Server != "builtin" {
		t.Fatalf("解析出的 server 错误: %q", act.Server)
	}
	if act.Arguments["connId"] != "db_1788254096769" {
		t.Fatalf("解析出的 arguments.connId 错误: %v", act.Arguments["connId"])
	}
	// 同时验证 stripTags 能移除裸 JSON 工具调用，避免最终正文残留。
	clean := stripTags(raw)
	if strings.Contains(clean, "\"action\"") {
		t.Fatalf("stripTags 未移除裸 JSON 工具调用，残留: %q", clean)
	}
}

// TestDBSchemasIndependentTableRoundtrip 验证同步的表结构经独立表存储后能正确读回，
// 且 db_query 的未同步表校验能匹配到已同步的表（排查「明明同步了却提示未同步」）。
func TestDBSchemasIndependentTableRoundtrip(t *testing.T) {
	m := newTestManager(t)
	connID := "db_1788254096769"
	database := "diygw"
	table := "diygw_days_erp"
	cfg := m.LoadAgentData().Config
	cfg.DBSchemas = map[string]DBSyncedTable{
		dbSchemaKey(connID, database, table): {
			ConnID:   connID,
			Database: database,
			Table:    table,
			Rows:     100,
			Columns:  []DBSyncedColumn{{Name: "add_time", Type: "int"}},
		},
	}
	if err := m.SaveAgentConfig(cfg); err != nil {
		t.Fatalf("SaveAgentConfig 失败: %v", err)
	}
	// 重新读回（模拟重启后从独立表加载）
	got := m.LoadAgentData().Config
	if len(got.DBSchemas) == 0 {
		t.Fatal("独立表存储后 DBSchemas 为空，表结构未持久化")
	}
	st := syncedTableSet(got, connID, database)
	if !st[strings.ToLower(table)] {
		t.Fatalf("已同步的表 %s 未出现在 syncedTableSet 中，实际 DBSchemas 键: %v", table, got.DBSchemas)
	}
	// 校验一条引用该表的 SQL 应放行
	if err := validateSyncedTables("SELECT * FROM diygw_days_erp", st, connID, database, got); err != nil {
		t.Fatalf("已同步表却被误判为未同步: %v", err)
	}
}

// TestDBSchemasPartialConfigSave 模拟前端 saveConfig 只传部分字段（dbSchemas 等），
// 验证残缺 cfg 覆盖 Config 后仍保留已同步表结构（排查「同步了却提示未同步」）。
func TestDBSchemasPartialConfigSave(t *testing.T) {
	m := newTestManager(t)
	connID := "db_1788254096769"
	database := "diygw"
	table := "diygw_days_erp"
	// 前端只传这 4 个字段，其余为默认零值
	partial := AgentConfig{
		ActiveDBConn: connID,
		DBSchemas: map[string]DBSyncedTable{
			dbSchemaKey(connID, database, table): {
				ConnID: connID, Database: database, Table: table, Rows: 100,
				Columns: []DBSyncedColumn{{Name: "add_time", Type: "int"}},
			},
		},
		DBSemantics: map[string]string{},
		DBLastDB:    map[string]string{connID: database},
	}
	if err := m.SaveAgentConfig(partial); err != nil {
		t.Fatalf("SaveAgentConfig 失败: %v", err)
	}
	got := m.LoadAgentData().Config
	st := syncedTableSet(got, connID, database)
	if !st[strings.ToLower(table)] {
		t.Fatalf("残缺 cfg 保存后已同步表 %s 丢失，DBSchemas: %v", table, got.DBSchemas)
	}
}

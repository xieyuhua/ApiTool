package db

import (
	"path/filepath"
	"testing"

	"apitool/internal/model"
)

func sampleData() model.AppData {
	return model.AppData{
		CurrentProjectID: "p1",
		Settings: model.Settings{
			AIBaseURL:  "https://api.openai.com/v1",
			AIKey:      "sk-test",
			AIModel:    "gpt-4o-mini",
			TimeoutSec: 30,
			Version:    "1.0.0",
		},
		Projects: []model.Project{
			{
				ID:          "p1",
				Name:        "默认项目",
				ActiveEnvID: "e1",
				Common:      model.CommonParams{Headers: []model.KV{{Key: "X-Test", Value: "1", Enabled: true}}},
				Dirs: []model.Directory{
					{ID: "d1", ParentID: "", Name: "用户模块", Sort: 1},
				},
				Apis: []model.ApiInfo{
					{
						ID: "a1", DirID: "d1", Name: "登录", Method: "POST", URL: "http://x/login",
						Headers:   []model.KV{{Key: "Content-Type", Value: "application/json"}},
						ReqFields: []*model.Field{{Name: "user", Type: "string", Children: []*model.Field{{Name: "name", Type: "string"}}}},
						LastResponse: &model.ResponseData{Status: 200, Body: `{"ok":true}`},
					},
				},
				Environments: []model.Environment{
					{ID: "e1", Name: "测试环境", Vars: []model.EnvVar{{Key: "host", Value: "1.2.3.4", Enabled: true}}},
				},
				TestCases: []model.TestCase{
					{
						ID: "c1", ApiID: "a1", Name: "正常登录", Method: "POST", URL: "http://x/login",
						Headers:    []model.KV{{Key: "Content-Type", Value: "application/json"}},
						Assertions: []model.Assertion{{Type: "status", Operator: "eq", Expected: "200", Enabled: true}},
						Enabled:    true,
					},
				},
				TestPlans: []model.TestPlan{
					{ID: "pl1", Name: "冒烟", CaseIDs: []string{"c1"}, EnvID: "e1", Concurrency: 2},
				},
				TestReports: []model.TestReport{
					{ID: "r1", PlanName: "冒烟", Total: 1, Passed: 1, Results: []model.TestResult{{CaseID: "c1", Passed: true}}},
				},
			},
		},
		Plugins:   model.PluginsData{Connections: []model.PluginConn{{ID: "pc1", Category: "db", Name: "本地库", Host: "127.0.0.1", Port: 3306, DbType: "mysql"}}},
		Clipboard: model.ClipData{History: []model.ClipItem{{ID: "cl1", Type: "text", Text: "hello", Timestamp: 123}}},
	}
}

func TestSQLiteRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	impl, err := OpenSQLite(dbPath, "1.0.0", "1.0.0")
	if err != nil {
		t.Fatalf("OpenSQLite 失败: %v", err)
	}
	defer impl.Close()

	in := sampleData()
	if err := impl.Write(in); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	out, err := impl.Read()
	if err != nil {
		t.Fatalf("Read 失败: %v", err)
	}

	if len(out.Projects) != 1 {
		t.Fatalf("期望 1 个项目，实际 %d", len(out.Projects))
	}
	p := out.Projects[0]
	if p.ID != "p1" || p.Name != "默认项目" || p.ActiveEnvID != "e1" {
		t.Fatalf("项目字段异常: %+v", p)
	}
	if len(p.Dirs) != 1 || p.Dirs[0].Name != "用户模块" {
		t.Fatalf("目录异常: %+v", p.Dirs)
	}
	if len(p.Apis) != 1 {
		t.Fatalf("接口数量异常: %d", len(p.Apis))
	}
	a := p.Apis[0]
	if a.Name != "登录" || a.Method != "POST" {
		t.Fatalf("接口字段异常: %+v", a)
	}
	if len(a.ReqFields) != 1 || len(a.ReqFields[0].Children) != 1 {
		t.Fatalf("嵌套字段未正确还原: %+v", a.ReqFields)
	}
	if a.LastResponse == nil || a.LastResponse.Status != 200 {
		t.Fatalf("LastResponse 未还原: %+v", a.LastResponse)
	}
	if len(p.Environments) != 1 || p.Environments[0].Vars[0].Key != "host" {
		t.Fatalf("环境未还原: %+v", p.Environments)
	}
	if len(p.TestCases) != 1 || !p.TestCases[0].Enabled || len(p.TestCases[0].Assertions) != 1 {
		t.Fatalf("用例未还原: %+v", p.TestCases)
	}
	if len(p.TestPlans) != 1 || len(p.TestPlans[0].CaseIDs) != 1 {
		t.Fatalf("计划未还原: %+v", p.TestPlans)
	}
	if len(p.TestReports) != 1 || p.TestReports[0].Passed != 1 {
		t.Fatalf("报告未还原: %+v", p.TestReports)
	}
	if out.Settings.AIKey != "sk-test" || out.Settings.TimeoutSec != 30 {
		t.Fatalf("设置未还原: %+v", out.Settings)
	}
	if len(out.Plugins.Connections) != 1 || out.Plugins.Connections[0].DbType != "mysql" {
		t.Fatalf("插件未还原: %+v", out.Plugins)
	}
	if len(out.Clipboard.History) != 1 || out.Clipboard.History[0].Text != "hello" {
		t.Fatalf("剪贴板未还原: %+v", out.Clipboard)
	}
}

func TestSQLiteEmptyThenWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	impl, err := OpenSQLite(dbPath, "1.0.0", "1.0.0")
	if err != nil {
		t.Fatalf("OpenSQLite 失败: %v", err)
	}
	defer impl.Close()

	// 空库读取应返回含默认设置的合法结构
	out, err := impl.Read()
	if err != nil {
		t.Fatalf("空库 Read 失败: %v", err)
	}
	if out.Settings.TimeoutSec != 30 {
		t.Fatalf("空库默认设置异常: %+v", out.Settings)
	}
}

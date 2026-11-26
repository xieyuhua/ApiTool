package tools

// 工具层测试：数据库工具的参数校验与 SQL 只读约束。
//
// Toolbox 只依赖 agent.Context，这里注入一个最小 stub，
// 不需要真实存储或数据库连接。

import (
	"strings"
	"testing"

	"apitool/core/agent"
	"apitool/internal/model"
)

// stubCtx 测试用宿主上下文。
type stubCtx struct {
	data model.AppData
	cfg  agent.AgentConfig
	dir  string
}

func (c *stubCtx) StoreDir() string               { return c.dir }
func (c *stubCtx) ReadData() model.AppData        { return c.data }
func (c *stubCtx) AgentConfig() agent.AgentConfig { return c.cfg }

func newTestToolbox() *Toolbox {
	return New(&stubCtx{dir: "."})
}

// TestDBSchemaUnknownConn 未配置的连接应给出明确错误，而不是空结果。
func TestDBSchemaUnknownConn(t *testing.T) {
	tb := newTestToolbox()
	_, err := tb.Exec("db_schema", map[string]interface{}{"connId": "nope", "database": "db1"}, 0)
	if err == nil {
		t.Fatal("未知连接应报错")
	}
	if !strings.Contains(err.Error(), "连接") {
		t.Fatalf("错误信息应说明连接问题: %v", err)
	}
}

// TestDBQueryRejectsWrite db_query 只允许 SELECT/WITH，写操作必须被拒绝。
func TestDBQueryRejectsWrite(t *testing.T) {
	tb := newTestToolbox()
	for _, sql := range []string{
		"DROP TABLE t",
		"DELETE FROM t",
		"UPDATE t SET a=1",
		"INSERT INTO t VALUES (1)",
		"CREATE TABLE x (a int)",
	} {
		if _, err := tb.Exec("db_query", map[string]interface{}{
			"connId": "x", "database": "d", "sql": sql,
		}, 0); err == nil {
			t.Fatalf("写操作应被拒绝: %s", sql)
		} else if !strings.Contains(err.Error(), "SELECT") {
			t.Fatalf("错误应提示只支持 SELECT（%s）: %v", sql, err)
		}
	}
}

// TestCalc 四则运算工具。
func TestCalc(t *testing.T) {
	tb := newTestToolbox()
	out, err := tb.Exec("calc", map[string]interface{}{"expr": "1+2*3"}, 0)
	if err != nil {
		t.Fatalf("计算失败: %v", err)
	}
	if !strings.Contains(out, "7") {
		t.Fatalf("结果应为 7: %q", out)
	}
	// 非法表达式应报错而不是返回错误结果
	if _, err := tb.Exec("calc", map[string]interface{}{"expr": "1+"}, 0); err == nil {
		t.Fatal("非法表达式应报错")
	}
}

// TestUnknownTool 未知工具应返回可读错误。
func TestUnknownTool(t *testing.T) {
	tb := newTestToolbox()
	if _, err := tb.Exec("no_such_tool", nil, 0); err == nil {
		t.Fatal("未知工具应报错")
	}
}

// TestCollectFiltersDisabled 关闭的工具不应出现在清单里。
func TestCollectFiltersDisabled(t *testing.T) {
	enabled := map[string]bool{"calc": true, "read_file": false}
	list := Collect(enabled, nil)
	var names []string
	for _, item := range list {
		names = append(names, item.Name)
	}
	if !contains(names, "calc") {
		t.Fatal("启用的 calc 应出现")
	}
	if contains(names, "read_file") {
		t.Fatal("关闭的 read_file 不应出现")
	}
}

// TestCollectUsesCustomDesc 自定义描述应覆盖默认描述。
func TestCollectUsesCustomDesc(t *testing.T) {
	list := Collect(map[string]bool{"calc": true}, map[string]string{"calc": "我的计算器"})
	for _, item := range list {
		if item.Name == "calc" {
			if !strings.Contains(item.Description, "我的计算器") {
				t.Fatalf("自定义描述未生效: %q", item.Description)
			}
			return
		}
	}
	t.Fatal("未找到 calc")
}

// TestMetaMatchesCollect 静态元信息与工具清单必须一一对应（防止加工具忘记登记）。
func TestMetaMatchesCollect(t *testing.T) {
	meta := Meta()
	all := map[string]bool{}
	for _, m := range meta {
		all[m.Name] = true
	}
	for _, name := range []string{
		"read_file", "write_file", "list_dir", "make_dir", "remove_dir",
		"remove_file", "rename_path", "web_search", "system_info", "get_time",
		"calc", "run_command", "db_schema", "db_query", "export_table",
	} {
		if !all[name] {
			t.Fatalf("元信息缺少工具 %s", name)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

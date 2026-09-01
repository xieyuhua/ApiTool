package agent

import (
	"strings"
	"testing"
)

func TestBuiltinDBSchemaNoConn(t *testing.T) {
	m := &Manager{}
	_, err := builtinDBSchema(m, map[string]interface{}{"connId": "nope", "database": "db1"})
	if err == nil {
		t.Fatal("期望连接不可用错误")
	}
}

func TestBuiltinDBQueryNoConn(t *testing.T) {
	m := &Manager{}
	_, err := builtinDBQuery(m, map[string]interface{}{"connId": "nope", "database": "db1", "sql": "SELECT 1"})
	if err == nil {
		t.Fatal("期望连接不可用错误")
	}
}

func TestBuiltinDBQueryRejectsWrite(t *testing.T) {
	m := &Manager{}
	// 使用不存在的连接，但仍应先被 SQL 前缀拦截（先校验更安全）
	_, err := builtinDBQuery(m, map[string]interface{}{"connId": "x", "database": "d", "sql": "DROP TABLE t"})
	if err == nil {
		t.Fatal("期望被拒绝的写操作错误")
	}
	if strings.Contains(err.Error(), "未找到数据库连接") {
		t.Fatalf("写操作应被 SQL 校验拦截，而非连接查找: %v", err)
	}
}

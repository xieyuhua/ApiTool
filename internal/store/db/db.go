// Package db 提供应用数据的数据库存储抽象，支持 SQLite（默认、本地文件）
// 与 MySQL（远程/共享）两种后端，通过 database/sql 统一接口屏蔽差异。
//
// 设计要点：
//   - 顶层实体（项目/目录/接口/环境/用例/计划/报告/设置等）用真实表存储；
//   - 嵌套切片（KV、字段、断言、环境变量、执行结果等）序列化为 JSON 文本列，
//     避免随结构演进频繁变更 schema，同时兼容 SQLite 与 MySQL。
//   - Write 采用"事务内先清空相关表再全量插入"的策略：桌面应用数据量小，
//     全量覆盖最简单且跨数据库通用（无需处理 upsert 方言差异）。
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"apitool/internal/model"
)

// DB 是存储后端的统一接口。Read 返回完整应用数据，Write 全量覆盖保存。
type DB interface {
	Read() (model.AppData, error)
	Write(model.AppData) error
	Close() error
	// Driver 返回后端类型，便于上层展示与诊断。
	Driver() string
}

// jsonCol 将任意值序列化为 JSON 文本（用于嵌套结构列）。
func jsonCol(v interface{}) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// scanJSON 从文本列解析回目标结构。
func scanJSON(col string, out interface{}) error {
	if col == "" || col == "[]" || col == "{}" {
		return nil
	}
	return json.Unmarshal([]byte(col), out)
}

// execTx 在事务中执行一组语句，任一失败则回滚。
func execTx(tx *sql.Tx, stmts ...string) error {
	for _, s := range stmts {
		if s == "" {
			continue
		}
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("执行 SQL 失败: %w\nSQL: %s", err, s)
		}
	}
	return nil
}

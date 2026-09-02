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
	// UpdateAgent 仅更新 meta.agent 单列，避免全量重写整个库（agent 数据频繁变动）。
	UpdateAgent(raw string) error
	// ReadAgent 读取 meta.agent 单列。
	ReadAgent() (string, error)
	// ReadSkills 读取全部技能（独立表 agent_skills）。
	ReadSkills() ([]AgentSkill, error)
	// SaveSkills 覆盖保存技能列表（独立表，行级 upsert）。
	SaveSkills([]AgentSkill) error
	// ReadDBAnalysis 读取数据库连接分析数据（独立表 db_schemas/db_semantics/db_last_db）。
	ReadDBAnalysis() (*DBAnalysisSnapshot, error)
	// SaveDBAnalysis 覆盖保存数据库连接分析数据（独立表）。
	SaveDBAnalysis(*DBAnalysisSnapshot) error
}

// jsonCol 将任意值序列化为 JSON 文本（用于嵌套结构列）。
func jsonCol(v any) string {
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
func scanJSON(col string, out any) error {
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

// AgentSkill 技能（可热加载）：一段带描述的系统能力/提示词片段，运行时按需注入。
// 独立存储于 agent_skills 表，支持行级增删改查，不再随 meta.agent 大 JSON 读写。
type AgentSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"` // 何时使用该技能（供模型判断）
	Prompt      string `json:"prompt"`      // 技能被激活时注入的提示词
	Enabled     bool   `json:"enabled"`     // 是否启用（热加载开关）
	Builtin     bool   `json:"builtin"`     // 是否为预置技能（首次启动写入，升级时不被覆盖）
	UpdatedAt   string `json:"updatedAt"`
}

// Package tools 是 Agent 的工具层：内置工具实现、数据库只读查询、表格导出。
//
// 不参与对话编排（那在 engine 层），也不知道 Manager 的其余能力：
// 只通过 agent.Context（数据目录 / 应用数据 / Agent 配置）读取它需要的信息。
package tools

import (
	"apitool/core/agent"
)

// Toolbox 内置工具执行器。
type Toolbox struct {
	ctx agent.Context
}

// New 创建工具执行器。
func New(ctx agent.Context) *Toolbox {
	return &Toolbox{ctx: ctx}
}

// Meta 返回全部内置工具的静态元信息（图标 / 默认描述 / 分组）。
func Meta() []agent.BuiltinToolDef { return agent.BuiltinToolMeta() }

// Collect 按开关与自定义描述构建工具清单（供提示词与前端展示）。
func Collect(enabled map[string]bool, desc map[string]string) []agent.MCPTool {
	return collectBuiltinTools(enabled, desc)
}

// SyncedDBs 返回某连接下已同步表结构的库列表（提示词中提示可用库）。
func (t *Toolbox) SyncedDBs(connID string) []string { return t.syncedDBsOfConn(connID) }

// Exec 执行一个内置工具。
func (t *Toolbox) Exec(name string, args map[string]interface{}, fileLimit int) (string, error) {
	return t.execBuiltinTool(name, args, fileLimit)
}

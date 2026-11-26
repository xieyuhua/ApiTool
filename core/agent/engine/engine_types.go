package engine

// 引擎层：Engine 定义与构造。
//
// Engine 只持有两项外部依赖：
//   - rt：数据与事件（agent.Runtime 接口，由基础层 Manager 实现）
//   - tb：内置工具执行器（tools.Toolbox）
// 因此可以传入 stub 单独测试对话循环。

import (
	"sync"

	"apitool/core/agent"
	"apitool/core/agent/tools"
)

// Engine 执行 Agent 对话。
type Engine struct {
	rt    agent.Runtime
	tb    *tools.Toolbox
	runMu sync.Mutex // 同一时刻只允许一次对话，避免两端交叉写入
}

// New 创建引擎。
func New(rt agent.Runtime, tb *tools.Toolbox) *Engine {
	return &Engine{rt: rt, tb: tb}
}

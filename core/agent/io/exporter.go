// Package io 是 Agent 的导出层：把会话渲染为可分享的 HTML / PDF。
//
// 只读数据、不写业务状态，因此只依赖 agent.DataReader（读快照 + 默认模型名）。
package io

import (
	"apitool/core/agent"
)

// Exporter 会话导出器。
type Exporter struct {
	r agent.DataReader
}

// New 创建导出器。
func New(r agent.DataReader) *Exporter { return &Exporter{r: r} }

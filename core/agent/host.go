package agent

// 基础层：宿主依赖与 Manager 定义。
//
// Manager 是本层唯一的有状态对象：它持有存储（Host）与事件出口（Emitter），
// 并实现全部「数据 + 配置 + 日志 + MCP」能力。
// engine / tools / io 三个子包都通过窄接口（见 update.go 等）拿到它们需要的那部分。

import (
	"context"
	"encoding/json"
	"sync"

	"apitool/internal/model"
	"apitool/internal/storeapi"
	"apitool/internal/util"
)

// Host 宿主能力接口：agent 模块通过它访问应用数据存储与配置，
// 从而与具体客户端形态（桌面 App / 服务端会话 / 独立服务）解耦。
type Host interface {
	Store() storeapi.Store
	ReadData() model.AppData
}

// Emitter 事件推送抽象：独立服务实现为 SSE 广播，桌面端实现为 Wails 事件。
// 只保留 Emit —— Agent 不需要窗口/剪贴板/文件对话框。
type Emitter interface {
	Emit(event string, data ...interface{})
}

// Manager 承载 agent 数据、配置、日志与 MCP 客户端。
type Manager struct {
	host Host
	b    Emitter
	ctx  context.Context
	mu   sync.Mutex // 保护 AgentData 的读改写（数据最终落主库 meta.agent 列）
}

// NewManager 创建 agent 管理器。host 提供数据与配置能力，b 用于事件推送。
// agent 数据统一通过 host.Store() 持久化到应用主库（SQLite/MySQL）。
func NewManager(host Host, b Emitter) *Manager {
	return &Manager{host: host, b: b, ctx: context.Background()}
}

// ---- 供子包使用的窄访问器 ----

// StoreDir 返回数据目录（工具写文件时的基准目录）。
func (m *Manager) StoreDir() string {
	if m == nil || m.host == nil || m.host.Store() == nil {
		return ""
	}
	return m.host.Store().Dir()
}

// ReadData 返回完整应用数据（agent.Context 要求，工具读取连接配置）。
func (m *Manager) ReadData() model.AppData {
	if m == nil || m.host == nil {
		return model.AppData{}
	}
	return m.host.ReadData()
}

// Connections 返回用户配置的全部连接（数据库/SSH 等）。
func (m *Manager) Connections() []model.PluginConn {
	if m == nil || m.host == nil {
		return nil
	}
	return m.host.ReadData().Plugins.Connections
}

// AgentConfig 返回当前 Agent 配置。
func (m *Manager) AgentConfig() AgentConfig {
	return m.ReadAgentData().Config
}

// AIModel 返回用户配置的默认模型名（导出文档头部展示用）。
func (m *Manager) AIModel() string {
	if m == nil || m.host == nil {
		return ""
	}
	return m.host.ReadData().Settings.AIModel
}

// emitEvent 向前端推送 agent 运行事件（实时展示步骤/思考）。
func (m *Manager) emitEvent(name string, payload interface{}) {
	if m.ctx != nil {
		m.b.Emit(name, payload)
	}
}

// EmitEvent 导出给子包使用（engine 的流式事件推送）。
func (m *Manager) EmitEvent(name string, payload interface{}) {
	m.emitEvent(name, payload)
}

// ---- 工具函数 ----

// agentID 生成带前缀的唯一会话/服务 ID（如 "sess_xxx"、"srv_xxx"）。
// 基于 UUID，避免原 time.Now().UnixNano() 在高并发同前缀下的碰撞风险。
func agentID(prefix string) string {
	return prefix + "_" + util.GenID()
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

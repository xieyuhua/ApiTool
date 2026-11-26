package agent

// 基础层：子包依赖的窄接口。
//
// 分层原则：engine / tools / io 不知道 Manager 的全部能力，只声明它们真正需要的。
// 这样既能避免「子包 import 门面包的具体类型」造成耦合，也让子包可用 stub 单独测试。

import "apitool/internal/model"

// Runtime 引擎层（engine）所需的数据与事件能力。
type Runtime interface {
	// ReadAgentData 返回一份完整 Agent 数据快照。
	ReadAgentData() AgentData
	// UpdateData 加锁执行一次「读-改-写」，用于保存会话消息与 token 统计。
	UpdateData(fn func(*AgentData)) error
	// AppendLog 追加一条运行日志（同时实时推送给前端）。
	AppendLog(AgentLog)
	// EmitEvent 推送流式事件（agent:delta 等）。
	EmitEvent(name string, payload interface{})
	// CreateAgentSession 新建会话并返回其 ID。
	CreateAgentSession(title string) string
	// CallMCPTool 调用外部 MCP 服务器的工具。
	CallMCPTool(srv MCPServer, tool string, arguments map[string]interface{}, userCtx map[string]interface{}) (string, error)
	// Connections 返回用户配置的全部连接（数据库/SSH 等）。
	Connections() []model.PluginConn
	// ListMCPTools 列举某个 MCP 服务器提供的工具。
	ListMCPTools(srv MCPServer, userCtx map[string]interface{}) ([]MCPTool, error)
	// MCPUserContext 构造传给 MCP 服务器的用户身份上下文。
	MCPUserContext(cfg AgentConfig, users []AgentUser) map[string]interface{}
	// NotifySessionsChanged 广播「会话数据已变更」。
	NotifySessionsChanged(reason string, extra map[string]interface{})
}

// UpdateData 加锁执行一次「读-改-写」，返回写回时的错误。
// fn 内可以自由修改 *AgentData；返回后统一持久化。
func (m *Manager) UpdateData(fn func(*AgentData)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.ReadAgentData()
	fn(&d)
	return m.WriteAgentData(d)
}

// Context 工具层（tools）所需的宿主上下文。
type Context interface {
	// StoreDir 返回数据目录（相对路径的基准）。
	StoreDir() string
	// ReadData 返回完整应用数据（读取连接配置）。
	ReadData() model.AppData
	// Config 返回当前 Agent 配置（读取表结构快照与字段语义）。
	AgentConfig() AgentConfig
}

// DataReader 导出层（io）所需的数据能力。
type DataReader interface {
	ReadAgentData() AgentData
	AIModel() string
}

// ---- 子包共用的纯函数别名 ----

// ToJSON 把任意值序列化为 JSON 字符串（引擎日志与步骤展示用）。
func ToJSON(v interface{}) string { return toJSON(v) }

// NewID 生成带前缀的唯一 ID（会话 / 工具调用）。
func NewID(prefix string) string { return agentID(prefix) }

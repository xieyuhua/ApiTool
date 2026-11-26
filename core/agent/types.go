package agent

// 基础层：数据模型。
//
// 本包是所有子包（engine / tools / io）的依赖终点：
// 这里**只放类型与数据能力**，不得 import 任何子包，否则会形成 import 环。

import (
	"apitool/internal/store/db"
)

// AgentSkill 技能（可热加载）：一段带描述的系统能力/提示词片段，运行时按需注入。
// 类型定义在 store/db 包（db.AgentSkill），独立存储于 agent_skills 表。
type AgentSkill = db.AgentSkill

// MCPServer MCP 服务器配置。支持 stdio（本地命令）与 http/sse（远程）两种传输。
type MCPServer struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // stdio | http
	Command   string            `json:"command"`   // stdio: 可执行命令
	Args      []string          `json:"args"`      // stdio: 参数
	Env       map[string]string `json:"env"`       // stdio: 环境变量
	URL       string            `json:"url"`       // http: 服务地址
	Headers   map[string]string `json:"headers"`   // http: 附加请求头
	Enabled   bool              `json:"enabled"`
	UpdatedAt string            `json:"updatedAt"`
}

// AgentUser 登录用户（用于传入 MCP 服务器以区分权限）。
type AgentUser struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Token string   `json:"token"`
	Roles []string `json:"roles"`
}

// ToolFlags 内置工具开关（无需 MCP 服务器，Agent 本地执行）。
// Enabled 为各工具独立开关（key=工具名），Desc 为各工具的自定义描述（缺省用默认）。
type ToolFlags struct {
	Enabled map[string]bool   `json:"enabled"` // 各工具独立开关
	Desc    map[string]string `json:"desc"`    // 各工具的自定义描述（可编辑）
}

// DBSyncedColumn 同步的字段信息。
//
// 字段语义有两层：Comment 来自数据库自身的列注释（同步时原样取回），
// 用户维护的中文说明则存在 AgentConfig.DBSemantics 里（key 含列名）。
// 两层在提示词里会合并展示，数据库没写注释时用户补的说明就是唯一线索。
type DBSyncedColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // 完整类型，如 varchar(64) / decimal(10,2)
	Nullable string `json:"nullable"` // YES / NO
	Default  string `json:"default"`  // 默认值表达式（无则为空）
	Comment  string `json:"comment"`  // 数据库自带注释
}

// DBSyncedTable 数据连接管理中同步的表结构快照。
//
// 键为 connId|database|table（全小写），存在 AgentConfig.DBSchemas；
// 只保存结构不保存数据，用于离线查看、补注释与生成 SQL 时注入上下文。
type DBSyncedTable struct {
	ConnID   string           `json:"connId"`
	Database string           `json:"database"`
	Table    string           `json:"table"`
	Rows     int              `json:"rows"` // 行数（估算，来自 information_schema）
	Columns  []DBSyncedColumn `json:"columns"`
}

// AgentConfig Agent 运行配置。
type AgentConfig struct {
	SystemPrompt string `json:"systemPrompt"` // 自定义系统提示词
	Mode         string `json:"mode"`         // react | plan
	MaxLoops     int    `json:"maxLoops"`     // agent loop 最大轮数
	ContextLimit int    `json:"contextLimit"` // 加载最近多少条上下文记录
	ShowThinking bool   `json:"showThinking"` // 输出思考过程
	ShowUsage    bool   `json:"showUsage"`    // 在每条回复上显示该轮消耗的 token
	EnablePolish bool   `json:"enablePolish"` // AI 润色
	EnableChart  bool   `json:"enableChart"`  // 图表输出（mermaid）

	// ---- 数据连接（表结构同步与语义注释）----
	// 这组字段是 db_schema / db_query 两个内置工具的数据来源：
	// 用户在「数据连接管理」里把线上库的表结构快照同步到本地，
	// 再把表/字段的中文语义补上，模型据此生成 SQL，避免臆测表名字段名。

	// EnableDBAnalysis 是否启用数据库连接分析（同步表结构 / 字段语义维护）。
	EnableDBAnalysis bool `json:"enableDBAnalysis"`
	// ActiveDBConn 当前激活用于数据库连接分析的连接 ID（同一时间仅允许一个）。
	// 内置工具在调用方未指定 connId 时会回退到它；多个连接时必须是显式指定。
	ActiveDBConn string `json:"activeDBConn"`
	// DBSemantics 用户维护的表/字段语义：key = connId|database|table|column（全小写），
	// value 为中文说明或业务口径。column 为空表示表级语义。
	// 生成 SQL 时与数据库自带注释一起注入提示词，二者都缺省时模型只能靠字段名猜。
	DBSemantics map[string]string `json:"dbSemantics"`
	// DBSchemas 已同步的表结构快照：key = connId|database|table（全小写）。
	// 保存完整列信息（名称/类型/可空/默认值/注释），使 db_schema 工具离线可用，
	// 也让用户在「数据连接管理」里不连库就能查看与补注释。
	DBSchemas map[string]DBSyncedTable `json:"dbSchemas"`
	// DBLastDB 每个连接（connId）上次选中的 database，下次打开「数据连接管理」时自动恢复，
	// 避免每次都要重选库。键为 connId，值为库名。
	DBLastDB map[string]string `json:"dbLastDB"`

	Temperature   float64   `json:"temperature"`
	CurrentUserID string    `json:"currentUserId"` // 当前登录用户
	Tools         ToolFlags `json:"tools"`         // 内置工具开关
	MaxToolOutput int       `json:"maxToolOutput"` // 工具结果回灌模型前的最大字符数（0=默认4000）
	MaxFileRead   int       `json:"maxFileRead"`   // 文件读取最大字符数（0=默认200000）
	MaxTokens     int       `json:"maxTokens"`     // 模型回复长度上限（0=使用模型服务端默认值）
}

// AgentMsg 会话消息。
type AgentMsg struct {
	ID       string      `json:"id"`
	Role     string      `json:"role"` // user | assistant | system
	Content  string      `json:"content"`
	Thinking string      `json:"thinking,omitempty"` // 思考过程
	Steps    []AgentStep `json:"steps,omitempty"`    // 使用的 skill/tool
	Time     string      `json:"time"`
	// Usage 本条 assistant 消息对应的那一轮消耗的 token（历史消息可能没有）。
	Usage *TokenUsage `json:"usage,omitempty"`
}

// TokenUsage 累计 token 消耗。
type TokenUsage struct {
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
}

// AgentSession 一次完整对话会话（含独立的消息历史与 token 消耗）。
type AgentSession struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"` // 会话标题（取首条用户输入）
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`
	Messages  []AgentMsg `json:"messages"`
	Usage     TokenUsage `json:"usage"`
}

// AgentStep Agent 单步执行记录（供前端展示"使用了哪些 skill/tool"）。
type AgentStep struct {
	Type   string `json:"type"`             // skill | tool | thought | plan
	Name   string `json:"name"`             // skill 名 / tool 名
	Server string `json:"server,omitempty"` // MCP server 名
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	// CallID 本次「调用实例」标识。同一次调用的「开始」与「结束」两次事件共用一个
	// CallID，前端据此把后者合并进前者（只显示一张卡片，且带上最终结果）；
	// 而模型多次调用同一个工具时每次 CallID 不同，因此会**逐条展示**，
	// 不会出现「调了 5 次只看到 1 条」的情况。
	CallID string `json:"callId,omitempty"`
}

// AgentLog 请求与调度日志（可查看、搜索）。
type AgentLog struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`    // info | request | response | tool | error | plan
	Category  string `json:"category"` // llm | mcp | agent | skill
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	// Summary 列表模式下的内容摘要（详情被清空时用于展示，也让关键词命中可见）
	Summary    string `json:"summary,omitempty"`
	DurationMs int64  `json:"durationMs"`
	UserID     string `json:"userId"`
	// Usage 本条 LLM 调用实际消耗的 token（仅 llm 分类的响应日志有值，历史日志为空）。
	// 日志面板据此在每条请求旁展示输入/输出/合计，并汇总当前筛选结果的用量。
	Usage *TokenUsage `json:"usage,omitempty"`
}

// logDetailHardLimit 单条日志详情的硬上限（字符）。正常请求远小于此；
// 超过说明工具结果异常庞大，截断时会在正文末尾标注原始长度。
const LogDetailHardLimit = 200000

// AgentData Agent 模块全部持久化数据（独立于主 data.json）。
type AgentData struct {
	Config        AgentConfig    `json:"config"`
	Skills        []AgentSkill   `json:"skills"`
	Servers       []MCPServer    `json:"servers"`
	Users         []AgentUser    `json:"users"`
	Sessions      []AgentSession `json:"sessions"`      // 多会话
	ActiveSession string         `json:"activeSession"` // 当前激活会话 ID
	Usage         TokenUsage     `json:"usage"`         // 全局累计 token
	Messages      []AgentMsg     `json:"messages"`      // 兼容旧数据（迁移后为空）
	Logs          []AgentLog     `json:"logs"`
}

// RunAgentArgs 前端发起一次 Agent 对话的入参（engine 与 HTTP 层共用）。
type RunAgentArgs struct {
	Input     string `json:"input"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	Model     string `json:"model"`
	Timeout   int    `json:"timeoutSec"`
	MaxTokens int    `json:"maxTokens"` // 模型回复长度上限（0=模型默认）
	// ClientID 发起端标识（桌面端或局域网网页端各自随机生成并保持不变）。
	// 后端会把它回传到 agent:done / agent:sessions-changed 等事件里，
	// 前端据此判断「这次对话是不是我自己发起的」——
	// 自己发起的不重复刷新，别人（如网页端）发起的才刷新会话并提示，
	// 否则两端会互相覆盖对方正在展示的流式内容。
	ClientID string `json:"clientId,omitempty"`
	// SessionID 本次对话所属会话。桌面端与局域网网页端各自浏览不同会话时，
	// 若仍依赖后端那个全局共享的 ActiveSession 游标，读到的历史与写入的结果
	// 就会落到不同会话上（表现为「刚发的内容不见了」/「串到别的会话」）。
	// 前端传自己正在浏览的会话 ID，为空时才回退到全局游标。
	SessionID string `json:"sessionId,omitempty"`
}

// RunAgentResult 一次对话的最终结果。
type RunAgentResult struct {
	Content  string      `json:"content"`
	Thinking string      `json:"thinking"`
	Steps    []AgentStep `json:"steps"`
	Plan     string      `json:"plan,omitempty"`
	Error    string      `json:"error,omitempty"`
	Usage    TokenUsage  `json:"usage"`
}

// messagesLogText 把发往模型的请求消息序列化为可读文本，写入 request 级日志，
// 用于核对「到底把什么发给了模型」：系统提示词、历史上下文、本轮输入、
// 工具结果回灌的内容全部原样保留，不做静默截断；仅在整体超过 logDetailHardLimit
// （极端的超长工具结果）时才截断，并在末尾明确标注原始长度，避免"看着完整其实被截了"。

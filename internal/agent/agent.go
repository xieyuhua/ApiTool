package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"apitool/internal/ai"
	"apitool/internal/bus"
	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/store/db"
	"apitool/internal/util"
)

// Host 宿主能力接口：agent 模块通过它访问应用数据存储、事件推送与配置，
// 从而与 main 包的 App 解耦（App 实现该接口并注入）。
type Host interface {
	Store() *store.Store
	ReadData() model.AppData
	SaveData(model.AppData) error
	SendRequest(model.RequestSpec) model.ResponseData
	AppVersion() string
}

// Manager 承载 agent 运行、配置与 MCP 客户端等全部逻辑，不再依赖 main 包的 App。
type Manager struct {
	host Host
	b    bus.Bus
	ctx  context.Context
	mu   sync.Mutex // 保护 AgentData 读写（数据最终落主库 meta.agent 列）
}

// NewManager 创建 agent 管理器。host 提供数据与配置能力，b 用于事件推送。
// agent 数据统一通过 host.Store() 持久化到应用主库（SQLite/MySQL）。
func NewManager(host Host, b bus.Bus) *Manager {
	return &Manager{host: host, b: b, ctx: context.Background()}
}

// ============================ 数据模型 ============================

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

// BuiltinToolDef 单个内置工具的静态定义（名称、图标、默认描述、分组）。
type BuiltinToolDef struct {
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Group   string `json:"group"`
	Default string `json:"default"` // 默认描述
}

// BuiltinToolMeta 返回全部内置工具的静态元信息（图标、默认描述、分组）。
// 顺序即设置页展示顺序。
func BuiltinToolMeta() []BuiltinToolDef {
	return []BuiltinToolDef{
		{Name: "read_file", Icon: "📄", Group: "文件操作", Default: "当用户需要查看某个文本/代码文件的内容时使用。传入文件绝对路径，可指定 limit 只读取前若干行。返回文件文本内容。"},
		{Name: "write_file", Icon: "✏️", Group: "文件操作", Default: "当用户需要创建新文件或覆盖写入内容时使用。传入文件绝对路径与要写入的完整 content（会覆盖原文件）。返回写入结果。"},
		{Name: "list_dir", Icon: "📂", Group: "文件操作", Default: "当用户想了解某个目录里有哪些文件或子目录时使用。传入目录路径，省略则默认当前工作目录。返回该目录下的文件/文件夹列表。"},
		{Name: "make_dir", Icon: "📁", Group: "文件操作", Default: "当用户需要新建目录时使用，支持一次创建多级目录（默认 all=true 递归创建）。传入目标目录路径。返回创建结果。"},
		{Name: "remove_dir", Icon: "🗑️", Group: "文件操作", Default: "当用户要删除目录时使用。默认仅删除空目录（安全）；若目录非空且确认可删，请传入 all=true 递归删除全部内容。传入目录路径。"},
		{Name: "remove_file", Icon: "❌", Group: "文件操作", Default: "当用户要删除某个具体的文件时使用（注意：不会删除目录）。传入文件绝对路径。返回删除结果。"},
		{Name: "rename_path", Icon: "🔀", Group: "文件操作", Default: "当用户要重命名文件/目录，或把文件/目录移动到新位置时使用。传入 src（原路径）与 dst（新路径）。返回移动/重命名结果。"},
		{Name: "web_search", Icon: "🔍", Group: "网页搜索", Default: "当用户的问题需要最新信息、实时数据、或你不确定答案时才使用。传入 query 关键词，可指定 limit 返回条数。返回相关网页的标题、摘要与链接。"},
		{Name: "system_info", Icon: "💻", Group: "系统信息", Default: "当用户想了解运行本程序的这台机器信息时使用：操作系统、CPU、内存、主机名、当前时间、当前工作目录等。无需参数。"},
		{Name: "get_time", Icon: "⏰", Group: "常用工具", Default: "当用户需要知道当前日期/时间，或指定时区（如 Asia/Shanghai）的时间时使用。返回格式化的本地时间。"},
		{Name: "calc", Icon: "🧮", Group: "常用工具", Default: "当用户需要计算一个算术表达式时使用，仅支持数字与 + - * / 和括号 ()。传入 expr，例如 \"1+2*3\"。返回计算结果。"},
		{Name: "run_command", Icon: "⌨️", Group: "常用工具", Default: "当用户需要执行一条本机 shell 命令（如 git、npm、系统命令）并获取其输出时使用。传入 command 字符串。⚠️ 会真实执行命令，请仅在确定安全时使用。"},
		{Name: "db_schema", Icon: "🗄️", Group: "数据库连接分析", Default: "返回用户在「插件/数据库连接」中已同步配置的表结构（库/表/字段名/类型/可空/默认值/注释），把数据库结构交给大模型用于生成 SQL 与数据分析。注意：本工具只返回你已同步的表，不会实时读取数据库全部表；大模型应严格只基于返回的这些表与字段编写 SQL，不得臆测或使用未列出的表/字段。返回的字段注释会叠加用户维护的表级与字段级语义。connId 与 database 为必填；tables 留空则返回该库下所有已同步的表。connId 可省略，省略时自动使用当前激活的分析连接。"},
		{Name: "db_query", Icon: "🔎", Group: "数据库连接分析", Default: "在已配置的连接上执行只读 SELECT 查询并返回结果（限制行数），用于采样数据、核对数值或验证假设。支持 MySQL、PostgreSQL、Oracle。必须以 SELECT 或 WITH 开头，禁止 INSERT/UPDATE/DELETE/DDL 等任何写操作；connId 可省略（省略时用当前激活连接），但 database 与 sql 必填。结果过大时请补充 WHERE/LIMIT 条件。"},
	}
}

// GetBuiltinTools 返回全部内置工具的静态元信息（供前端设置页动态罗列，保证前后端一致）。
func (m *Manager) GetBuiltinTools() []BuiltinToolDef {
	return BuiltinToolMeta()
}

// ToolFlags 内置工具开关（无需 MCP 服务器，Agent 本地执行）。
// Enabled 为各工具独立开关（key=工具名），Desc 为各工具的自定义描述（缺省用默认）。
type ToolFlags struct {
	Enabled map[string]bool    `json:"enabled"` // 各工具独立开关
	Desc    map[string]string  `json:"desc"`    // 各工具的自定义描述（可编辑）
}

// DBSyncedColumn 同步的字段信息。
type DBSyncedColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
	Default  string `json:"default"`
	Comment  string `json:"comment"` // 数据库自带注释
}

// DBSyncedTable 数据连接管理中同步的表结构快照。
type DBSyncedTable struct {
	ConnID   string           `json:"connId"`
	Database string           `json:"database"`
	Table    string           `json:"table"`
	Rows     int              `json:"rows"` // 行数（估算）
	Columns  []DBSyncedColumn `json:"columns"`
}

// AgentConfig Agent 运行配置。
type AgentConfig struct {
	SystemPrompt   string `json:"systemPrompt"`   // 自定义系统提示词
	Mode           string `json:"mode"`           // react | plan
	MaxLoops       int    `json:"maxLoops"`       // agent loop 最大轮数
	ContextLimit   int    `json:"contextLimit"`   // 加载最近多少条上下文记录
	ShowThinking   bool   `json:"showThinking"`   // 输出思考过程
	EnablePolish   bool   `json:"enablePolish"`   // AI 润色
	EnableChart    bool   `json:"enableChart"`    // 图表输出（mermaid）
	EnableDBAnalysis bool `json:"enableDBAnalysis"` // 数据库连接分析（同步表结构 / 字段语义维护）
	ActiveDBConn     string `json:"activeDBConn"`   // 当前激活用于数据库连接分析的连接 ID（同一时间仅允许一个）
	// 数据连接管理中维护的字段语义：key = connId|database|table|column（全小写），value = 中文/维护语义
	DBSemantics map[string]string `json:"dbSemantics"`
	// 数据连接管理中同步的表结构快照：key = connId|database|table（全小写），便于离线查看与再编辑语义
	DBSchemas map[string]DBSyncedTable `json:"dbSchemas"`
	// 每个数据库连接（connId）上次在「插件 / 数据库连接」中选中的 database，再次打开时自动恢复选中
	DBLastDB map[string]string `json:"dbLastDB"`
	Temperature    float64 `json:"temperature"`
	CurrentUserID  string `json:"currentUserId"`  // 当前登录用户
	Tools          ToolFlags `json:"tools"`       // 内置工具开关
	MaxToolOutput  int      `json:"maxToolOutput"` // 工具结果回灌模型前的最大字符数（0=默认4000）
	MaxFileRead    int      `json:"maxFileRead"`   // 文件读取最大字符数（0=默认200000）
	MaxTokens      int      `json:"maxTokens"`     // 模型回复长度上限（0=使用模型服务端默认值）
}

// AgentMsg 会话消息。
type AgentMsg struct {
	ID       string `json:"id"`
	Role     string `json:"role"` // user | assistant | system
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"` // 思考过程
	Steps    []AgentStep `json:"steps,omitempty"` // 使用的 skill/tool
	Time     string `json:"time"`
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
	Title     string     `json:"title"`     // 会话标题（取首条用户输入）
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`
	Messages  []AgentMsg `json:"messages"`
	Usage     TokenUsage `json:"usage"`
}

// AgentStep Agent 单步执行记录（供前端展示"使用了哪些 skill/tool"）。
type AgentStep struct {
	Type   string `json:"type"`   // skill | tool | thought | plan
	Name   string `json:"name"`   // skill 名 / tool 名
	Server string `json:"server,omitempty"` // MCP server 名
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// AgentLog 请求与调度日志（可查看、搜索）。
type AgentLog struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`   // info | request | response | tool | error | plan
	Category  string `json:"category"` // llm | mcp | agent | skill
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	DurationMs int64 `json:"durationMs"`
	UserID    string `json:"userId"`
}

// AgentData Agent 模块全部持久化数据（独立于主 data.json）。
type AgentData struct {
	Config         AgentConfig   `json:"config"`
	Skills         []AgentSkill  `json:"skills"`
	Servers        []MCPServer   `json:"servers"`
	Users          []AgentUser   `json:"users"`
	Sessions       []AgentSession `json:"sessions"`      // 多会话
	ActiveSession  string        `json:"activeSession"`  // 当前激活会话 ID
	Usage          TokenUsage    `json:"usage"`          // 全局累计 token
	Messages       []AgentMsg    `json:"messages"`       // 兼容旧数据（迁移后为空）
	Logs           []AgentLog    `json:"logs"`
}

// ============================ 持久化 ============================

func defaultAgentData() AgentData {
	return AgentData{
		Config: AgentConfig{
			SystemPrompt: "你是一个通用智能助手，会严格按照用户给定的角色与任务要求行事（例如数据分析师、运维工程师、客服、开发等），若用户未明确指定角色，则以「贴心、专业、严谨」的通用助手身份回答。\n\n通用准则：\n1. 优先用工具获取真实信息，不要臆测；信息不足时向用户确认，而非编造。\n2. 涉及数据库时：需要结构用 db_schema，需要采样数据用 db_query（仅只读 SELECT，禁止写操作）；当返回结果包含用户维护的「语义」时，优先采用这些语义理解业务含义。\n3. 未显式指定数据库连接时，使用当前已激活的连接；连接未配置或报错时，明确提示用户去「插件 / 数据库连接」配置。\n4. 回答使用简体中文，结论简洁、可验证；执行任何操作或给出 SQL/命令前先说明用途与风险。\n\n【数据分析请求强制流程】当用户要求「分析 / 统计 / 查询 / 看板 / 报表」任何与具体数据相关的任务时，必须且只能基于工具返回的真实数据作答，严禁凭空编造数字或结论。标准流程：\n第 1 步：先用 db_schema 了解相关表的结构、字段含义与可用时间字段；\n第 2 步：用 db_query 编写并执行只读 SQL 取得所需数据（如「最近一周销售」需按时间范围过滤、按维度聚合）；\n第 3 步：基于返回数据做统计与解读，必要时给出趋势/对比/异常说明；\n若已加载了相关的技能（Skill），优先按该技能定义的分析模板执行。每一步都应让用户看到你正在调用哪个工具。",
			Mode:         "react",
			MaxLoops:     6,
			ContextLimit: 20,
			ShowThinking: true,
			EnablePolish: false,
			EnableChart:  true,
			Temperature:  0.3,
			Tools:        defaultToolFlags(),
			MaxToolOutput: 4000,
			MaxFileRead:   200000,
			MaxTokens:     8000,
		},
		Skills:  defaultSkills(),
		Servers: []MCPServer{},
		Users:   []AgentUser{},
		Sessions: []AgentSession{
			{ID: "default", Title: "默认会话", CreatedAt: time.Now().Format(time.RFC3339), UpdatedAt: time.Now().Format(time.RFC3339)},
		},
		ActiveSession: "default",
		Messages:      []AgentMsg{},
		Logs:          []AgentLog{},
	}
}

// readAgentData 从应用主库（meta.agent 列）读取并补全 Agent 数据。
// 主库不可用时由 Store 回退到旧 agent.json 文件（兼容迁移）。调用方需持有 m.mu。
func (m *Manager) readAgentData() AgentData {
	data := defaultAgentData()
	raw := m.host.Store().LoadAgentRaw()
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &data)
	}
	if data.Config.MaxLoops <= 0 {
		data.Config.MaxLoops = 6
	}
	if data.Config.ContextLimit <= 0 {
		data.Config.ContextLimit = 20
	}
	if data.Config.Mode == "" {
		data.Config.Mode = "react"
	}
	// 技能独立存储于 agent_skills 表：优先从独立表读取。
	// 先保留 meta.agent 中解析出的旧 skills（用于首次迁移判断），不可提前清空。
	metaSkills := data.Skills
	if metaSkills == nil {
		metaSkills = []AgentSkill{}
	}
	dbSkills := m.host.Store().LoadSkills()
	if len(dbSkills) == 0 && len(metaSkills) > 0 {
		// 独立表为空但旧 meta.agent JSON 中含有 skills：首次迁移，写入独立表。
		dbSkills = metaSkills
		_ = m.host.Store().SaveSkills(dbSkills)
	}
	// 合并预置技能（不覆盖用户已有的同 ID 技能），并持久化新增的预置技能。
	merged := mergeDefaultSkills(dbSkills)
	if len(merged) != len(dbSkills) {
		_ = m.host.Store().SaveSkills(merged)
	}
	data.Skills = merged
	if data.Skills == nil {
		data.Skills = []AgentSkill{}
	}
	// 数据库连接分析数据（表结构同步 / 字段语义维护）独立存储于
	// db_schemas / db_semantics / db_last_db 表，优先从独立表读取。
	snap := m.host.Store().LoadDBAnalysis()
	oldSchemas := data.Config.DBSchemas
	oldSem := data.Config.DBSemantics
	oldLast := data.Config.DBLastDB
	if len(snap.Schemas) == 0 && len(oldSchemas) > 0 {
		// 独立表为空但旧 meta.agent JSON 中含有：首次迁移，写入独立表。
		snap.Schemas = schemasToJSONMap(oldSchemas)
		snap.Semantics = oldSem
		snap.LastDB = oldLast
		_ = m.host.Store().SaveDBAnalysis(snap)
	}
	// 仅当独立表确有数据时才覆盖，避免「独立表空 / 回退模式」时用空值清掉 meta.agent 中已同步的数据。
	if len(snap.Schemas) > 0 {
		data.Config.DBSchemas = schemasFromJSONMap(snap.Schemas)
	}
	if len(snap.Semantics) > 0 {
		data.Config.DBSemantics = snap.Semantics
	}
	if len(snap.LastDB) > 0 {
		data.Config.DBLastDB = snap.LastDB
	}
	if data.Config.DBSchemas == nil {
		data.Config.DBSchemas = map[string]DBSyncedTable{}
	}
	if data.Config.DBSemantics == nil {
		data.Config.DBSemantics = map[string]string{}
	}
	if data.Config.DBLastDB == nil {
		data.Config.DBLastDB = map[string]string{}
	}
	if data.Servers == nil {
		data.Servers = []MCPServer{}
	}
	if data.Users == nil {
		data.Users = []AgentUser{}
	}
	if data.Logs == nil {
		data.Logs = []AgentLog{}
	}
	if data.Sessions == nil {
		// 兼容旧版本：把顶层 Messages 迁移为默认会话
		data.Sessions = []AgentSession{
			{ID: "default", Title: "默认会话", CreatedAt: time.Now().Format(time.RFC3339), UpdatedAt: time.Now().Format(time.RFC3339), Messages: data.Messages},
		}
		data.ActiveSession = "default"
	}
	if data.ActiveSession == "" && len(data.Sessions) > 0 {
		data.ActiveSession = data.Sessions[0].ID
	}
	// 确保当前会话存在
	if !sessionExists(data, data.ActiveSession) && len(data.Sessions) > 0 {
		data.ActiveSession = data.Sessions[0].ID
	}
	// 内置工具开关迁移与兜底
	data.Config.Tools = migrateToolFlags(data.Config.Tools)
	for i := range data.Sessions {
		if data.Sessions[i].Messages == nil {
			data.Sessions[i].Messages = []AgentMsg{}
		}
	}
	return data
}

func sessionExists(d AgentData, id string) bool {
	for _, s := range d.Sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}

// defaultToolFlags 返回内置工具默认开关与描述（全部开启，描述为默认值）。
func defaultToolFlags() ToolFlags {
	enabled := map[string]bool{}
	desc := map[string]string{}
	for _, t := range BuiltinToolMeta() {
		enabled[t.Name] = true
		desc[t.Name] = t.Default
	}
	return ToolFlags{Enabled: enabled, Desc: desc}
}

// defaultSkills 预置的数据分析模板技能。用户可在前端「技能」中编辑、新增或关闭，
// 启动时通过 mergeDefaultSkills 合并，不会覆盖用户已有配置。
func defaultSkills() []AgentSkill {
	now := time.Now().Format(time.RFC3339)
	return []AgentSkill{
		{
			ID:          "skill_sales_analysis",
			Name:        "销售数据分析",
			Description: "当用户要求分析销售额、销量、订单、营收、客单价、最近一周/月/季度销售等与销售业绩相关的话题时使用。",
			Prompt:      "你是销售数据分析专家。分析销售相关问题时遵循：\n1. 先用 db_schema 找到订单/销售/商品相关表，确认金额、数量、时间、状态等字段。\n2. 用 db_query 执行只读 SQL，按时间(最近一周/月/季度)、品类、地区、渠道等维度聚合，计算销售额、销量、订单数、客单价、环比/同比。\n3. 识别Top商品、异常波动、退货/退款影响。\n4. 给出结论与可执行建议，并用表格或趋势描述呈现关键指标。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
		{
			ID:          "skill_user_retention",
			Name:        "用户留存分析",
			Description: "当用户要求分析新增用户、活跃用户、留存率、流失率、复购、用户生命周期等与用户行为相关的话题时使用。",
			Prompt:      "你是用户增长分析专家。分析用户留存/活跃相关问题时遵循：\n1. 用 db_schema 定位用户表、注册表、登录/行为表、订单表。\n2. 用 db_query 计算：日/周/月新增、活跃(DAU/WAU/MAU)、次日/7日/30日留存、复购率、流失率。\n3. 按渠道、注册月份做同期群(Cohort)对比。\n4. 输出留存曲线结论与提升建议。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
		{
			ID:          "skill_traffic_conversion",
			Name:        "流量转化分析",
			Description: "当用户要求分析访问量、转化率、漏斗、加购、下单转化、渠道效果等与流量和转化相关的话题时使用。",
			Prompt:      "你是流量与转化分析专家。分析转化相关问题时遵循：\n1. 用 db_schema 定位曝光/点击/访问、加购、下单等各漏斗环节表。\n2. 用 db_query 计算各环节量级与转化率，构建漏斗(曝光→点击→加购→下单→支付)。\n3. 拆解各渠道/落地页的转化差异，定位流失最大环节。\n4. 输出漏斗图结论与优化建议。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
		{
			ID:          "skill_anomaly_diagnosis",
			Name:        "指标异常诊断",
			Description: "当用户要求排查某指标突增/突降、数据异常、波动原因、对比上期差异时使用。",
			Prompt:      "你是指标异常诊断专家。分析异常波动时遵循：\n1. 用 db_schema 确认指标所属表与维度字段。\n2. 用 db_query 拉取异常期与对照期(环比/同比)的分维度数据。\n3. 通过下钻(地区/渠道/品类/新老用户)定位主要贡献因子。\n4. 给出根因结论与验证 SQL。",
			Enabled:     true,
			Builtin:     true,
			UpdatedAt:   now,
		},
	}
}

// mergeDefaultSkills 将预置技能与用户已有技能合并：用户已存在同 ID 的技能以用户的为准，
// 用户未配置过的预置技能追加进列表，保证升级后模板技能自动出现且不覆盖用户修改。
func mergeDefaultSkills(user []AgentSkill) []AgentSkill {
	have := map[string]bool{}
	for _, s := range user {
		if s.ID != "" {
			have[s.ID] = true
		}
	}
	merged := make([]AgentSkill, 0, len(user)+len(defaultSkills()))
	merged = append(merged, user...)
	for _, s := range defaultSkills() {
		if !have[s.ID] {
			merged = append(merged, s)
		}
	}
	return merged
}

// migrateToolFlags 将旧版分组开关（fileOp/webSearch/sysInfo/common）迁移为各工具独立开关。
func migrateToolFlags(old ToolFlags) ToolFlags {
	if old.Enabled != nil && len(old.Enabled) > 0 {
		return old
	}
	enabled := map[string]bool{}
	desc := map[string]string{}
	for _, t := range BuiltinToolMeta() {
		enabled[t.Name] = true
		desc[t.Name] = t.Default
	}
	return ToolFlags{Enabled: enabled, Desc: desc}
}

// schemasToJSONMap 将表结构快照 map 转为 value 为 JSON 的通用 map，便于跨包存独立表。
func schemasToJSONMap(schemas map[string]DBSyncedTable) map[string]string {
	out := map[string]string{}
	if schemas == nil {
		return out
	}
	for k, v := range schemas {
		if b, err := json.Marshal(v); err == nil {
			out[k] = string(b)
		}
	}
	return out
}

// schemasFromJSONMap 将独立表中 value 为 JSON 的 map 转回表结构快照 map。
func schemasFromJSONMap(m map[string]string) map[string]DBSyncedTable {
	out := map[string]DBSyncedTable{}
	if m == nil {
		return out
	}
	for k, v := range m {
		var t DBSyncedTable
		if err := json.Unmarshal([]byte(v), &t); err == nil {
			out[k] = t
		}
	}
	return out
}

// activeSession 返回当前激活会话指针（不存在时返回 nil）。
func (d *AgentData) activeSession() *AgentSession {
	for i := range d.Sessions {
		if d.Sessions[i].ID == d.ActiveSession {
			return &d.Sessions[i]
		}
	}
	return nil
}

// writeAgentData 将 Agent 数据序列化并持久化到应用主库（meta.agent 列）。
// 调用方需持有 m.mu；内部会读取当前 AppData 并覆盖 Agent 字段后整体保存。
func (m *Manager) writeAgentData(data AgentData) error {
	// 限制日志与消息数量，避免无限增长
	if len(data.Logs) > 2000 {
		data.Logs = data.Logs[len(data.Logs)-2000:]
	}
	if len(data.Messages) > 500 {
		data.Messages = data.Messages[len(data.Messages)-500:]
	}
	// 限制每个会话消息数量，避免无限增长
	for i := range data.Sessions {
		if len(data.Sessions[i].Messages) > 500 {
			data.Sessions[i].Messages = data.Sessions[i].Messages[len(data.Sessions[i].Messages)-500:]
		}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := m.host.Store().SaveAgentRaw(string(b)); err != nil {
		return err
	}
	// 同步数据库连接分析数据到独立表（与 meta.agent 冗余，但支持行级读写与查询）。
	snap := &db.DBAnalysisSnapshot{
		Schemas:   schemasToJSONMap(data.Config.DBSchemas),
		Semantics: data.Config.DBSemantics,
		LastDB:    data.Config.DBLastDB,
	}
	return m.host.Store().SaveDBAnalysis(snap)
}

// ============================ 前端可调用：配置 / CRUD ============================

// LoadAgentData 返回 Agent 全部数据（配置、技能、服务器、用户、消息）。日志单独查询。
func (m *Manager) LoadAgentData() AgentData {
	d := m.readAgentData()
	// 消息与日志较大，前端首屏只需要少量消息，这里全部返回由前端裁剪
	return d
}

// SaveAgentConfig 保存运行配置。
func (m *Manager) SaveAgentConfig(cfg AgentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	// 合并而非整体覆盖：前端「同步表结构」等场景只回传部分字段（dbSchemas 等），
	// 直接 d.Config = cfg 会把内存中已有的 Tools / SystemPrompt / Mode 等配置清零。
	// 仅当传入字段「非默认零值」时才覆盖，避免配置被局部保存吞掉。
	if cfg.ActiveDBConn != "" || cfg.DBLastDB != nil {
		d.Config.ActiveDBConn = cfg.ActiveDBConn
	}
	if cfg.DBSchemas != nil {
		d.Config.DBSchemas = cfg.DBSchemas
	}
	if cfg.DBSemantics != nil {
		d.Config.DBSemantics = cfg.DBSemantics
	}
	if cfg.DBLastDB != nil {
		d.Config.DBLastDB = cfg.DBLastDB
	}
	if cfg.SystemPrompt != "" {
		d.Config.SystemPrompt = cfg.SystemPrompt
	}
	if cfg.Mode != "" {
		d.Config.Mode = cfg.Mode
	}
	if cfg.MaxLoops > 0 {
		d.Config.MaxLoops = cfg.MaxLoops
	}
	if cfg.ContextLimit > 0 {
		d.Config.ContextLimit = cfg.ContextLimit
	}
	if cfg.Tools.Enabled != nil || cfg.Tools.Desc != nil {
		d.Config.Tools = cfg.Tools
	}
	if cfg.EnableDBAnalysis {
		d.Config.EnableDBAnalysis = cfg.EnableDBAnalysis
	}
	if cfg.Temperature != 0 {
		d.Config.Temperature = cfg.Temperature
	}
	if cfg.MaxTokens > 0 {
		d.Config.MaxTokens = cfg.MaxTokens
	}
	if cfg.MaxToolOutput > 0 {
		d.Config.MaxToolOutput = cfg.MaxToolOutput
	}
	if cfg.MaxFileRead > 0 {
		d.Config.MaxFileRead = cfg.MaxFileRead
	}
	if cfg.ShowThinking {
		d.Config.ShowThinking = cfg.ShowThinking
	}
	if cfg.EnablePolish {
		d.Config.EnablePolish = cfg.EnablePolish
	}
	if cfg.EnableChart {
		d.Config.EnableChart = cfg.EnableChart
	}
	return m.writeAgentData(d)
}

// SaveAgentSkills 覆盖保存技能列表（独立表存储，热加载：保存后立即生效）。
func (m *Manager) SaveAgentSkills(skills []AgentSkill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().Format(time.RFC3339)
	for i := range skills {
		if skills[i].ID == "" {
			skills[i].ID = agentID("skill")
		}
		skills[i].UpdatedAt = now
	}
	if err := m.host.Store().SaveSkills(skills); err != nil {
		return err
	}
	// 同步内存中的 AgentData，保证后续全量备份(meta.agent)包含最新技能。
	d := m.readAgentData()
	d.Skills = skills
	return m.writeAgentData(d)
}

// SaveMCPServers 覆盖保存 MCP 服务器列表。
func (m *Manager) SaveMCPServers(servers []MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	now := time.Now().Format(time.RFC3339)
	for i := range servers {
		if servers[i].ID == "" {
			servers[i].ID = agentID("mcp")
		}
		if servers[i].Transport == "" {
			servers[i].Transport = "stdio"
		}
		servers[i].UpdatedAt = now
	}
	d.Servers = servers
	return m.writeAgentData(d)
}

// SaveAgentUsers 覆盖保存用户列表。
func (m *Manager) SaveAgentUsers(users []AgentUser) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	for i := range users {
		if users[i].ID == "" {
			users[i].ID = agentID("user")
		}
	}
	d.Users = users
	return m.writeAgentData(d)
}

// ClearAgentMessages 清空当前会话历史。
func (m *Manager) ClearAgentMessages() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	if s := d.activeSession(); s != nil {
		s.Messages = []AgentMsg{}
	}
	return m.writeAgentData(d)
}

// CreateAgentSession 新建会话，返回新会话 ID。
func (m *Manager) CreateAgentSession(title string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	id := agentID("sess")
	now := time.Now().Format(time.RFC3339)
	if title == "" {
		title = "新会话"
	}
	d.Sessions = append(d.Sessions, AgentSession{ID: id, Title: title, CreatedAt: now, UpdatedAt: now, Messages: []AgentMsg{}})
	d.ActiveSession = id
	_ = m.writeAgentData(d)
	m.b.Emit("agent:session-created", map[string]interface{}{"id": id, "title": title})
	return id
}

// SwitchAgentSession 切换当前激活会话。
func (m *Manager) SwitchAgentSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	if !sessionExists(d, id) {
		return fmt.Errorf("会话不存在")
	}
	d.ActiveSession = id
	_ = m.writeAgentData(d)
	return nil
}

// DeleteAgentSession 删除会话（至少保留一个）。
func (m *Manager) DeleteAgentSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	if len(d.Sessions) <= 1 {
		return fmt.Errorf("至少保留一个会话")
	}
	idx := -1
	for i, s := range d.Sessions {
		if s.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("会话不存在")
	}
	d.Sessions = append(d.Sessions[:idx], d.Sessions[idx+1:]...)
	if d.ActiveSession == id {
		d.ActiveSession = d.Sessions[0].ID
	}
	_ = m.writeAgentData(d)
	return nil
}

// RenameAgentSession 重命名会话。
func (m *Manager) RenameAgentSession(id, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	for i := range d.Sessions {
		if d.Sessions[i].ID == id {
			d.Sessions[i].Title = title
			d.Sessions[i].UpdatedAt = time.Now().Format(time.RFC3339)
		}
	}
	_ = m.writeAgentData(d)
	return nil
}

// ============================ 日志 ============================

func (m *Manager) appendLog(l AgentLog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	if l.ID == "" {
		l.ID = agentID("log")
	}
	if l.Timestamp == 0 {
		l.Timestamp = time.Now().UnixMilli()
	}
	if l.Time == "" {
		l.Time = time.Now().Format("2006-01-02 15:04:05.000")
	}
	d.Logs = append(d.Logs, l)
	_ = m.writeAgentData(d)
	// 实时推送给前端日志面板
	if m.ctx != nil {
		m.b.Emit( "agent:log", l)
	}
}

// QueryAgentLogsArgs 日志查询参数。
type QueryAgentLogsArgs struct {
	Keyword  string `json:"keyword"`
	Level    string `json:"level"`
	Category string `json:"category"`
	Limit    int    `json:"limit"`
}

// QueryAgentLogs 按关键词/级别/分类搜索日志，返回最新在前。
func (m *Manager) QueryAgentLogs(args QueryAgentLogsArgs) []AgentLog {
	d := m.readAgentData()
	kw := strings.ToLower(strings.TrimSpace(args.Keyword))
	out := make([]AgentLog, 0, len(d.Logs))
	for _, l := range d.Logs {
		if args.Level != "" && l.Level != args.Level {
			continue
		}
		if args.Category != "" && l.Category != args.Category {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(l.Title + " " + l.Detail)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	limit := args.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ClearAgentLogs 清空日志。
func (m *Manager) ClearAgentLogs() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.readAgentData()
	d.Logs = []AgentLog{}
	return m.writeAgentData(d)
}

// ============================ MCP 客户端 ============================

// MCPTool MCP 工具定义。
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Server      string          `json:"server"` // 归属服务器 ID（前端用）
	ServerName  string          `json:"serverName"`
}

// jsonRPCReq / jsonRPCResp JSON-RPC 2.0 报文。
type jsonRPCReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}
type jsonRPCResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mcpUserContext 传给 MCP 的登录用户上下文（用于权限区分）。
func (m *Manager) mcpUserContext(cfg AgentConfig, users []AgentUser) map[string]interface{} {
	for _, u := range users {
		if u.ID == cfg.CurrentUserID {
			return map[string]interface{}{
				"userId": u.ID,
				"userName": u.Name,
				"token": u.Token,
				"roles": u.Roles,
			}
		}
	}
	return nil
}

// callMCPStdio 通过 stdio 传输调用一次 MCP（initialize -> 指定 method）。
// 为简化实现，每次调用启动一次进程，完成后退出。
func (m *Manager) callMCPStdio(srv MCPServer, method string, params interface{}) (json.RawMessage, error) {
	if srv.Command == "" {
		return nil, fmt.Errorf("MCP[%s] 未配置命令", srv.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP 进程失败: %w", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	reader := bufio.NewReader(stdout)
	writeReq := func(req jsonRPCReq) error {
		b, _ := json.Marshal(req)
		b = append(b, '\n')
		_, err := stdin.Write(b)
		return err
	}
	readResp := func(wantID int) (json.RawMessage, error) {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var resp jsonRPCResp
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue // 忽略非 JSON-RPC 行（部分 server 会打印日志）
			}
			if resp.ID != wantID {
				continue
			}
			if resp.Error != nil {
				return nil, fmt.Errorf("MCP 错误 %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

	// initialize
	if err := writeReq(jsonRPCReq{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "apitool-agent", "version": ai.AppVersion},
	}}); err != nil {
		return nil, err
	}
	if _, err := readResp(1); err != nil {
		return nil, err
	}
	// initialized 通知
	_ = writeReq(jsonRPCReq{JSONRPC: "2.0", ID: 0, Method: "notifications/initialized"})

	// 真正的方法调用
	if err := writeReq(jsonRPCReq{JSONRPC: "2.0", ID: 2, Method: method, Params: params}); err != nil {
		return nil, err
	}
	return readResp(2)
}

// callMCPHTTP 通过 HTTP（JSON-RPC over POST）调用远程 MCP。
func (m *Manager) callMCPHTTP(srv MCPServer, method string, params interface{}, userCtx map[string]interface{}) (json.RawMessage, error) {
	if srv.URL == "" {
		return nil, fmt.Errorf("MCP[%s] 未配置 URL", srv.Name)
	}
	reqBody, _ := json.Marshal(jsonRPCReq{JSONRPC: "2.0", ID: 2, Method: method, Params: params})
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range srv.Headers {
		req.Header.Set(k, v)
	}
	// 传入登录用户标识，供服务端区分权限
	if userCtx != nil {
		if uid, ok := userCtx["userId"].(string); ok && uid != "" {
			req.Header.Set("X-User-Id", uid)
		}
		if tok, ok := userCtx["token"].(string); ok && tok != "" {
			req.Header.Set("X-User-Token", tok)
		}
		if b, err := json.Marshal(userCtx); err == nil {
			req.Header.Set("X-User-Context", string(b))
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(body))
	}
	var rpc jsonRPCResp
	if err := json.Unmarshal(body, &rpc); err != nil {
		return nil, fmt.Errorf("解析 MCP 响应失败: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("MCP 错误 %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	return rpc.Result, nil
}

// listMCPTools 列出单个服务器的工具。
func (m *Manager) listMCPTools(srv MCPServer, userCtx map[string]interface{}) ([]MCPTool, error) {
	var raw json.RawMessage
	var err error
	if srv.Transport == "http" {
		raw, err = m.callMCPHTTP(srv, "tools/list", map[string]interface{}{}, userCtx)
	} else {
		raw, err = m.callMCPStdio(srv, "tools/list", map[string]interface{}{})
	}
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	for i := range parsed.Tools {
		parsed.Tools[i].Server = srv.ID
		parsed.Tools[i].ServerName = srv.Name
	}
	return parsed.Tools, nil
}

// ListAllMCPTools 列出所有启用服务器的工具（前端预览/调试用）。
func (m *Manager) ListAllMCPTools() ([]MCPTool, error) {
	d := m.readAgentData()
	userCtx := m.mcpUserContext(d.Config, d.Users)
	var all []MCPTool
	for _, srv := range d.Servers {
		if !srv.Enabled {
			continue
		}
		tools, err := m.listMCPTools(srv, userCtx)
		if err != nil {
			m.appendLog(AgentLog{Level: "error", Category: "mcp", Title: "列出工具失败: " + srv.Name, Detail: err.Error()})
			continue
		}
		all = append(all, tools...)
	}
	return all, nil
}

// TestMCPServer 测试单个 MCP 服务器连通性，返回工具列表。
func (m *Manager) TestMCPServer(srv MCPServer) ([]MCPTool, error) {
	d := m.readAgentData()
	userCtx := m.mcpUserContext(d.Config, d.Users)
	return m.listMCPTools(srv, userCtx)
}

// callMCPTool 调用某工具，注入登录用户上下文（供权限区分）。
func (m *Manager) callMCPTool(srv MCPServer, tool string, arguments map[string]interface{}, userCtx map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      tool,
		"arguments": arguments,
	}
	// 同时通过 _meta 注入用户上下文（MCP 规范允许携带 _meta）
	if userCtx != nil {
		params["_meta"] = map[string]interface{}{"user": userCtx}
	}
	start := time.Now()
	var raw json.RawMessage
	var err error
	if srv.Transport == "http" {
		raw, err = m.callMCPHTTP(srv, "tools/call", params, userCtx)
	} else {
		raw, err = m.callMCPStdio(srv, "tools/call", params)
	}
	dur := time.Since(start).Milliseconds()
	if err != nil {
		m.appendLog(AgentLog{Level: "error", Category: "mcp", Title: fmt.Sprintf("调用工具失败: %s@%s", tool, srv.Name), Detail: err.Error(), DurationMs: dur})
		return "", err
	}
	// 解析 content 文本
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(raw, &result)
	var sb strings.Builder
	for _, c := range result.Content {
		if c.Text != "" {
			sb.WriteString(c.Text)
			sb.WriteString("\n")
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		out = string(raw)
	}
	m.appendLog(AgentLog{Level: "tool", Category: "mcp", Title: fmt.Sprintf("调用工具: %s@%s", tool, srv.Name), Detail: "参数: " + toJSON(arguments) + "\n结果: " + util.Truncate(out, 4000), DurationMs: dur})
	return out, nil
}

// ============================ 工具函数 ============================

// agentID 生成带前缀的唯一会话/服务 ID（如 "sess_xxx"、"srv_xxx"）。
// 基于 UUID，避免原 time.Now().UnixNano() 在高并发同前缀下的碰撞风险。
func agentID(prefix string) string {
	return prefix + "_" + util.GenID()
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

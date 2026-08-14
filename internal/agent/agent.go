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
	host          Host
	b             bus.Bus
	ctx           context.Context
	mu            sync.Mutex // 对应原 agentMu，保护 agent.json 读写
	agentDataPath string
}

// NewManager 创建 agent 管理器。host 提供数据与配置能力，b 用于事件推送，
// agentDataPath 为 agent.json 的绝对路径。
func NewManager(host Host, b bus.Bus, agentDataPath string) *Manager {
	return &Manager{host: host, b: b, ctx: context.Background(), agentDataPath: agentDataPath}
}

// ============================ 数据模型 ============================

// AgentSkill 技能（可热加载）：一段带描述的系统能力/提示词片段，运行时按需注入。
type AgentSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"` // 何时使用该技能（供模型判断）
	Prompt      string `json:"prompt"`      // 技能被激活时注入的提示词
	Enabled     bool   `json:"enabled"`     // 是否启用（热加载开关）
	UpdatedAt   string `json:"updatedAt"`
}

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

// AgentConfig Agent 运行配置。
type AgentConfig struct {
	SystemPrompt   string `json:"systemPrompt"`   // 自定义系统提示词
	Mode           string `json:"mode"`           // react | plan
	MaxLoops       int    `json:"maxLoops"`       // agent loop 最大轮数
	ContextLimit   int    `json:"contextLimit"`   // 加载最近多少条上下文记录
	ShowThinking   bool   `json:"showThinking"`   // 输出思考过程
	EnablePolish   bool   `json:"enablePolish"`   // AI 润色
	EnableChart    bool   `json:"enableChart"`    // 图表输出（mermaid）
	Temperature    float64 `json:"temperature"`
	CurrentUserID  string `json:"currentUserId"`  // 当前登录用户
	Tools          ToolFlags `json:"tools"`       // 内置工具开关
	MaxToolOutput  int      `json:"maxToolOutput"` // 工具结果回灌模型前的最大字符数（0=默认4000）
	MaxFileRead    int      `json:"maxFileRead"`   // 文件读取最大字符数（0=默认200000）
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

func (m *Manager) agentFilePath() string {
	return m.agentDataPath
}

func defaultAgentData() AgentData {
	return AgentData{
		Config: AgentConfig{
			SystemPrompt: "你是一个专业的 API 测试与开发智能助手，可以调用工具帮助用户完成任务。回答使用简体中文。",
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
		},
		Skills:  []AgentSkill{},
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

func (m *Manager) readAgentData() AgentData {
	m.mu.Lock()
	defer m.mu.Unlock()
	data := defaultAgentData()
	b, err := os.ReadFile(m.agentDataPath)
	if err != nil {
		return data
	}
	_ = json.Unmarshal(b, &data)
	if data.Config.MaxLoops <= 0 {
		data.Config.MaxLoops = 6
	}
	if data.Config.ContextLimit <= 0 {
		data.Config.ContextLimit = 20
	}
	if data.Config.Mode == "" {
		data.Config.Mode = "react"
	}
	if data.Skills == nil {
		data.Skills = []AgentSkill{}
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

// activeSession 返回当前激活会话指针（不存在时返回 nil）。
func (d *AgentData) activeSession() *AgentSession {
	for i := range d.Sessions {
		if d.Sessions[i].ID == d.ActiveSession {
			return &d.Sessions[i]
		}
	}
	return nil
}

func (m *Manager) writeAgentData(data AgentData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 限制日志与消息数量，避免文件无限增长
	if len(data.Logs) > 2000 {
		data.Logs = data.Logs[len(data.Logs)-2000:]
	}
	if len(data.Messages) > 500 {
		data.Messages = data.Messages[len(data.Messages)-500:]
	}
	// 限制每个会话消息数量，避免文件无限增长
	for i := range data.Sessions {
		if len(data.Sessions[i].Messages) > 500 {
			data.Sessions[i].Messages = data.Sessions[i].Messages[len(data.Sessions[i].Messages)-500:]
		}
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.agentDataPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.agentDataPath)
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
	d := m.readAgentData()
	if cfg.MaxLoops <= 0 {
		cfg.MaxLoops = 6
	}
	if cfg.ContextLimit <= 0 {
		cfg.ContextLimit = 20
	}
	if cfg.Mode != "plan" {
		cfg.Mode = "react"
	}
	d.Config = cfg
	return m.writeAgentData(d)
}

// SaveAgentSkills 覆盖保存技能列表（热加载：保存后立即生效）。
func (m *Manager) SaveAgentSkills(skills []AgentSkill) error {
	d := m.readAgentData()
	now := time.Now().Format(time.RFC3339)
	for i := range skills {
		if skills[i].ID == "" {
			skills[i].ID = agentID("skill")
		}
		skills[i].UpdatedAt = now
	}
	d.Skills = skills
	return m.writeAgentData(d)
}

// SaveMCPServers 覆盖保存 MCP 服务器列表。
func (m *Manager) SaveMCPServers(servers []MCPServer) error {
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
	d := m.readAgentData()
	if s := d.activeSession(); s != nil {
		s.Messages = []AgentMsg{}
	}
	return m.writeAgentData(d)
}

// CreateAgentSession 新建会话，返回新会话 ID。
func (m *Manager) CreateAgentSession(title string) string {
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

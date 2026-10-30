package model

// KV 键值对（Header / Query / Form 等）
// Type 仅用于表单（formItems）："text" 普通文本（默认），"file" 文件上传（Value 为本地文件路径）
type KV struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"` // text | file（仅表单使用）
}

// 表单字段类型常量
const (
	FormTypeText = "text"
	FormTypeFile = "file"
)

// Field 参数字段（支持嵌套）
type Field struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Example     string   `json:"example"`
	Description string   `json:"description"`
	Children    []*Field `json:"children,omitempty"`
}

// CommonParams 项目级公共参数（自动附加到所有接口请求，接口自身同名参数优先）
type CommonParams struct {
	Headers []KV `json:"headers"`
	Query   []KV `json:"query"`
}

// ResponseData 请求响应结果
type ResponseData struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	DurationMs int64             `json:"durationMs"`
	Size       int64             `json:"size"`
	IsJSON     bool              `json:"isJson"`
	Error      string            `json:"error"`
}

// RequestSpec 请求定义
type RequestSpec struct {
	Method      string `json:"method"`
	URL         string `json:"url"`
	Headers     []KV   `json:"headers"`
	Query       []KV   `json:"query"`
	BodyType    string `json:"bodyType"` // none | json | form | text
	Body        string `json:"body"`
	FormItems   []KV   `json:"formItems"`
	TimeoutSec  int    `json:"timeoutSec"`
	Env         []KV   `json:"env"`         // 当前环境环境变量（用于 {{var}} 替换）
	ContentType string `json:"contentType"` // 自定义 Content-Type 覆盖（非空时优先）
}

// ApiInfo 接口信息
type ApiInfo struct {
	ID           string        `json:"id"`
	DirID        string        `json:"dirId"`
	Name         string        `json:"name"`
	Method       string        `json:"method"`
	URL          string        `json:"url"`
	Description  string        `json:"description"`
	ContentType  string        `json:"contentType"` // 自定义 Content-Type 覆盖，为空则按 BodyType 自动
	Headers      []KV          `json:"headers"`
	Query        []KV          `json:"query"`
	BodyType     string        `json:"bodyType"`
	Body         string        `json:"body"`
	FormItems    []KV          `json:"formItems"`
	ReqFields    []*Field      `json:"reqFields"`
	RespFields   []*Field      `json:"respFields"`
	PreScript    string        `json:"preScript"`  // 发送前脚本（前端执行）
	PostScript   string        `json:"postScript"` // 收到响应后脚本（前端执行）
	LastResponse *ResponseData `json:"lastResponse,omitempty"`
	UpdatedAt    string        `json:"updatedAt"`
}

// EnvVar 环境变量
type EnvVar struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// Environment 环境（变量集合）
type Environment struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Vars []EnvVar `json:"vars"`
}

// Directory 目录节点
type Directory struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Name     string `json:"name"`
	Sort     int    `json:"sort"`
}

// Project 项目（每个项目拥有独立的目录/接口/环境）
type Project struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Dirs         []Directory   `json:"dirs"`
	Apis         []ApiInfo     `json:"apis"`
	Environments []Environment `json:"environments"`
	ActiveEnvID  string        `json:"activeEnvId"`
	Common       CommonParams  `json:"common"`
	UpdatedAt    string        `json:"updatedAt"`
	// 接口测试
	TestCases   []TestCase   `json:"testCases,omitempty"`
	TestPlans   []TestPlan   `json:"testPlans,omitempty"`
	TestReports []TestReport `json:"testReports,omitempty"`
}

// Assertion 测试断言（期望）
type Assertion struct {
	Type     string `json:"type"`     // status | json | body | header | duration
	Target   string `json:"target"`   // json 时为 JSONPath；header 时为请求头名；其余为空
	Operator string `json:"operator"` // eq | ne | gt | gte | lt | lte | contains | exists | isTrue | isFalse
	Expected string `json:"expected"` // 期望值（duration 时单位为毫秒）
	Enabled  bool   `json:"enabled"`
}

// TestCase 测试用例（自带完整请求快照 + 断言，运行时不依赖原接口）
type TestCase struct {
	ID          string      `json:"id"`
	ApiID       string      `json:"apiId"`    // 关联接口 ID（可为空）
	ApiName     string      `json:"apiName"`  // 关联接口名称（展示用）
	Category    string      `json:"category"` // 正常流程 | 参数边界 | 异常场景 | 权限安全
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	Headers     []KV        `json:"headers"`
	Query       []KV        `json:"query"`
	BodyType    string      `json:"bodyType"`
	Body        string      `json:"body"`
	FormItems   []KV        `json:"formItems"`
	ContentType string      `json:"contentType"`
	Assertions  []Assertion `json:"assertions"`
	Enabled     bool        `json:"enabled"`
	CreatedAt   string      `json:"createdAt"`
	DirID       string      `json:"dirId,omitempty"`
	DirName     string      `json:"dirName,omitempty"`
	Source      string      `json:"source,omitempty"` // 用例来源：api=接口导入 capture=浏览器捕获导入 ai=AI生成
}

// TestPlan 测试执行计划（有序用例 + 运行环境）
type TestPlan struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	CaseIDs     []string `json:"caseIds"`     // 有序用例 ID 列表
	EnvID       string   `json:"envId"`       // 运行环境
	Concurrency int      `json:"concurrency"` // 并发数（预留，当前串行执行）
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// AssertionResult 单条断言执行结果
type AssertionResult struct {
	Description string `json:"description"` // 人类可读描述
	Passed      bool   `json:"passed"`
	Detail      string `json:"detail"` // 实际值 vs 期望值
}

// TestResult 单个用例执行结果
type TestResult struct {
	CaseID     string `json:"caseId"`
	CaseName   string `json:"caseName"`
	Category   string `json:"category"`
	Passed     bool   `json:"passed"`
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	DurationMs int64  `json:"durationMs"`
	Size       int64  `json:"size"`
	Error      string `json:"error"`
	// 实际请求快照（公共参数合并、环境变量替换前的定义）
	RequestMethod      string `json:"requestMethod"`
	RequestURL         string `json:"requestURL"`
	RequestHeaders     []KV   `json:"requestHeaders"`
	RequestQuery       []KV   `json:"requestQuery"`
	RequestFormItems   []KV   `json:"requestFormItems"`
	RequestBodyType    string `json:"requestBodyType"`
	RequestBody        string `json:"requestBody"`
	RequestContentType string `json:"requestContentType"`
	// 完整响应
	ResponseBody     string            `json:"responseBody"`
	ResponseHeaders  map[string]string `json:"responseHeaders"`
	AssertionResults []AssertionResult `json:"assertionResults"`
}

// TestReport 测试报告
type TestReport struct {
	ID         string       `json:"id"`
	PlanID     string       `json:"planId"`
	PlanName   string       `json:"planName"`
	EnvName    string       `json:"envName"` // 运行环境名称（执行上下文）
	CreatedAt  string       `json:"createdAt"`
	Total      int          `json:"total"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	DurationMs int64        `json:"durationMs"`
	Results    []TestResult `json:"results"`
	Summary    string       `json:"summary"` // AI 分析摘要
}

// Settings 全局设置
// ClipSettings 剪贴板监听相关配置
type ClipSettings struct {
	Monitor  bool `json:"monitor"`  // 是否启用系统剪贴板监听
	MaxItems int  `json:"maxItems"` // 历史保留最大条数
}

type Settings struct {
	AIBaseURL  string `json:"aiBaseUrl"`
	AIKey      string `json:"aiKey"`
	AIModel    string `json:"aiModel"`
	TimeoutSec int    `json:"timeoutSec"`
	// AI 生成测试用例提示词（用户可在「设置」中维护；为空则使用内置默认提示词）
	TestCasePrompt string `json:"testCasePrompt"`
	// 剪贴板
	Clipboard ClipSettings `json:"clipboard"`
	// 云同步
	CloudURL   string `json:"cloudURL"`
	CloudToken string `json:"cloudToken"`
	CloudUser  string `json:"cloudUser"`
	// 版本与升级
	Version   string `json:"version"`   // 客户端版本号（同时作为配置初始化标记）
	UpdateURL string `json:"updateURL"` // 升级服务地址，如 http://127.0.0.1:8080
	// 外观
	Theme    string `json:"theme"`    // light | dark | system
	Accent   string `json:"accent"`   // 主题强调色
	AutoSync bool   `json:"autoSync"` // 是否启用云同步
	// 详情代码块字体（作用于右侧详情所有代码块）
	CodeFont     string  `json:"codeFont"`     // 字体族
	CodeFontSize float64 `json:"codeFontSize"` // 字号(px)
	// 文档导出水印（作用于 Word / PDF 导出，可在「文档中心」配置）
	Watermark DocWatermark `json:"watermark"`
	// 企业级文档排版（封面 / 目录 / 前言 / 规范 / 附录 / 修订记录，作用于全格式导出）
	DocLayout DocLayout `json:"docLayout"`
	// 用户保存的自定义文档结构模板（一键套用）
	DocLayoutTemplates []DocLayoutTemplate `json:"docLayoutTemplates"`
	// 项目级文档结构覆盖：key 为项目 ID，未命中表示沿用全局「文档模板」配置
	ProjectDocLayouts map[string]DocLayout `json:"projectDocLayouts,omitempty"`
	// 分享文档是否套用「文档结构」与「导出水印」
	ShareDocLayout    bool `json:"shareDocLayout"`
	ShareDocWatermark bool `json:"shareDocWatermark"`
}

// DocLayoutTemplate 自定义文档结构模板（团队可保存多套，如「对外版」「评审版」）。
type DocLayoutTemplate struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Layout DocLayout `json:"layout"`
}

// DocLayout 企业级接口文档的排版配置：决定导出文档由哪些章节组成、顺序如何。
// 零值即「只导出接口正文 + 目录」，用户可在「文档中心 → 文档结构」中逐项开启。
type DocLayout struct {
	Toc         bool          `json:"toc"`         // 是否生成目录（Word 为可更新域，PDF/HTML 为静态目录）
	Numbering   bool          `json:"numbering"`   // 章节与接口树是否自动编号（1 / 1.1 / 1.1.1）
	Cover       DocCover      `json:"cover"`       // 封面（作者/单位/版本/日期）
	Prefaces    []DocChapter  `json:"prefaces"`    // 文档公共描述（可多条章节）
	Convention  string        `json:"convention"`  // 接口统一定义规范（Markdown 文本，空则不输出）
	StatusCodes bool          `json:"statusCodes"` // 附录：HTTP 状态码说明
	Revisions   []DocRevision `json:"revisions"`   // 文档修订记录
	// Environment 输出「接入地址（环境）」章节，地址取自项目环境变量
	Environment bool `json:"environment"`
	// CommonHeaders 输出「公共请求头与公共参数」章节，取自项目公共参数
	CommonHeaders bool `json:"commonHeaders"`
	// Sign 加密与验签说明（Markdown 文本）
	Sign string `json:"sign"`
	// SignOn 是否输出「加密与验签」章节；nil 视为开启（兼容旧数据）
	SignOn *bool `json:"signOn,omitempty"`
	// ConventionOn 是否输出「接口统一定义规范」章节；nil 视为开启（兼容旧数据）
	ConventionOn *bool `json:"conventionOn,omitempty"`
	// BusinessCodes 附录：业务错误码（空则不输出该章节）
	BusinessCodes []DocCode `json:"businessCodes"`
	// QuickStart 输出「接入指引（快速开始）」章节
	QuickStart bool `json:"quickStart"`
	// Glossary 术语表（空则不输出该章节）
	Glossary []DocTerm `json:"glossary"`
	// Enums 附录：数据字典（枚举值，空则不输出该章节）
	Enums []DocEnum `json:"enums"`
	// Support 技术支持与联系（Enabled 为 true 且至少填一项时输出）
	Support DocSupport `json:"support"`
	// Name 当前套用的模板名称（仅用于界面展示与选择，不参与排版）
	Name string `json:"name,omitempty"`
}

// DocTerm 术语表条目。
type DocTerm struct {
	Term string `json:"term"` // 术语
	Desc string `json:"desc"` // 说明
}

// DocEnum 数据字典条目（枚举值说明）。
type DocEnum struct {
	Field   string `json:"field"`   // 字段名
	Code    string `json:"code"`    // 枚举值
	Meaning string `json:"meaning"` // 含义
}

// DocSupport 技术支持与联系信息。
type DocSupport struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`   // 对接人 / 支持团队
	Email   string `json:"email"`  // 邮箱
	Phone   string `json:"phone"`  // 电话
	Group   string `json:"group"`  // 支持群 / 工单入口
	Ticket  string `json:"ticket"` // 工单 / 帮助中心链接
	SLA     string `json:"sla"`    // 响应时效说明
}

// DocCode 业务错误码条目（附录表格的一行）。
type DocCode struct {
	Code    string `json:"code"`    // 业务码，如 10001
	Message string `json:"message"` // 含义
	Desc    string `json:"desc"`    // 说明 / 排查建议
}

// DocCover 封面信息。
type DocCover struct {
	Enabled  bool   `json:"enabled"`
	Title    string `json:"title"`    // 为空时取文档/项目标题
	Subtitle string `json:"subtitle"` // 如「内部技术文档 · 禁止外传」
	Author   string `json:"author"`
	Org      string `json:"org"`     // 公司 / 部门
	Version  string `json:"version"` // 如 V1.2.0
	Date     string `json:"date"`    // 为空时取导出日期
}

// DocChapter 文档章节（用于「文档公共描述」等自定义正文）。
type DocChapter struct {
	Title   string `json:"title"`
	Content string `json:"content"` // 简化 Markdown（支持标题/列表/表格/代码块/段落）
}

// DocRevision 文档修订记录的一行。
type DocRevision struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Author  string `json:"author"`
	Note    string `json:"note"`
}

// DocWatermark 文档导出水印配置。
// 零值视为「未配置」，渲染前经 doc.NormalizeWatermark 补全为内置默认值。
type DocWatermark struct {
	Enabled bool    `json:"enabled"` // 是否启用
	Text    string  `json:"text"`    // 水印文字，默认「内部资料」
	Color   string  `json:"color"`   // 颜色 #RRGGBB，默认 #C0C4CC
	Size    int     `json:"size"`    // 字号(pt)，默认 42
	Opacity float64 `json:"opacity"` // 不透明度 0-1，默认 0.14
	Angle   int     `json:"angle"`   // 旋转角度(度，顺时针为正)，默认 -30
	Repeat  bool    `json:"repeat"`  // 是否平铺；false 为页面居中单个水印
}

// AppData 应用全部数据
type AppData struct {
	Projects         []Project   `json:"projects"`
	CurrentProjectID string      `json:"currentProjectId"`
	Settings         Settings    `json:"settings"`
	Plugins          PluginsData `json:"plugins"`
	Clipboard        ClipData    `json:"clipboard"`
	// Agent 为 agent 模块全部数据（配置/技能/MCP/用户/会话/日志，含表结构语义）的 JSON 序列化，
	// 随应用主库（SQLite/MySQL）持久化；json:"-" 表示不写入顶层 data.json 回退文件，
	// 回退模式下由 store 单独读取旧 agent.json 兼容。
	Agent string `json:"-"`
}

// PluginManager 连接配置（按分类管理：数据库 / Redis / ES / XShell(SSH) / FTP / SFTP）
type PluginConn struct {
	ID        string `json:"id"`
	Category  string `json:"category"` // db | redis | es | ssh | ftp | sftp
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Database  string `json:"database"` // 数据库名 / ES 索引（可选）
	DbType    string `json:"dbType"`   // mysql | postgres（仅 db 分类使用）
	DbIndex   int    `json:"dbIndex"`  // Redis 默认 DB 序号（仅 redis 分类使用）
	Encoding  string `json:"encoding"` // 终端编码：utf-8 | gbk | gb18030（仅 ssh 分类使用）
	UseTLS    bool   `json:"useTLS"`   // es/ftp(s) 等可选
	Remark    string `json:"remark"`
	UpdatedAt string `json:"updatedAt"`
}

type PluginsData struct {
	Connections []PluginConn `json:"connections"`
}

// 剪贴板条目类型
const (
	ClipTypeText  = "text"
	ClipTypeImage = "image"
)

// 剪贴板历史记录
type ClipItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // text | image
	Text      string `json:"text"`      // 文本类型内容
	ImagePath string `json:"imagePath"` // 图片类型：本地 PNG 相对路径
	Width     int    `json:"width"`     // 图片宽度
	Height    int    `json:"height"`    // 图片高度
	Time      string `json:"time"`
	Timestamp int64  `json:"timestamp"`
}

type ClipData struct {
	History []ClipItem `json:"history"`
}

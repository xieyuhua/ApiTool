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
	CaseID           string            `json:"caseId"`
	CaseName         string            `json:"caseName"`
	Category         string            `json:"category"`
	Passed           bool              `json:"passed"`
	Status           int               `json:"status"`
	DurationMs       int64             `json:"durationMs"`
	Error            string            `json:"error"`
	ResponseBody     string            `json:"responseBody"`
	AssertionResults []AssertionResult `json:"assertionResults"`
}

// TestReport 测试报告
type TestReport struct {
	ID         string       `json:"id"`
	PlanID     string       `json:"planId"`
	PlanName   string       `json:"planName"`
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
	Theme   string       `json:"theme"`   // light | dark | system
	Accent  string       `json:"accent"`  // 主题强调色
	AutoSync bool        `json:"autoSync"` // 是否启用云同步
}

// AppData 应用全部数据
type AppData struct {
	Projects         []Project   `json:"projects"`
	CurrentProjectID string      `json:"currentProjectId"`
	Settings         Settings    `json:"settings"`
	Plugins          PluginsData `json:"plugins"`
	Clipboard        ClipData    `json:"clipboard"`
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

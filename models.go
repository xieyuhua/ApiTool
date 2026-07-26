package main

// KV 键值对（Header / Query / Form 等）
type KV struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// Field 参数字段（支持嵌套）
type Field struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Example     string   `json:"example"`
	Description string   `json:"description"`
	Children    []*Field `json:"children,omitempty"`
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
	Method     string `json:"method"`
	URL        string `json:"url"`
	Headers    []KV   `json:"headers"`
	Query      []KV   `json:"query"`
	BodyType   string `json:"bodyType"` // none | json | form | text
	Body       string `json:"body"`
	FormItems  []KV   `json:"formItems"`
	TimeoutSec int    `json:"timeoutSec"`
	Env        []KV   `json:"env"` // 当前环境环境变量（用于 {{var}} 替换）
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
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Vars []EnvVar  `json:"vars"`
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
	UpdatedAt    string        `json:"updatedAt"`
}

// Settings 全局设置
type Settings struct {
	AIBaseURL  string `json:"aiBaseUrl"`
	AIKey      string `json:"aiKey"`
	AIModel    string `json:"aiModel"`
	TimeoutSec int    `json:"timeoutSec"`
	// 云同步
	CloudURL   string `json:"cloudURL"`
	CloudToken string `json:"cloudToken"`
	CloudUser  string `json:"cloudUser"`
}

// AppData 应用全部数据
type AppData struct {
	Projects        []Project `json:"projects"`
	CurrentProjectID string    `json:"currentProjectId"`
	Settings        Settings  `json:"settings"`
}

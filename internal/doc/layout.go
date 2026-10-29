package doc

// 企业级文档排版：把「封面 / 修订记录 / 目录 / 公共描述 / 统一定义规范 / 接口正文 / 附录」
// 组装成有序章节，供 Markdown / HTML(PDF) / DOCX 三种渲染器共用，避免各写一套顺序逻辑。

import (
	"fmt"
	"strings"
	"time"

	"apitool/internal/model"
)

// docSectionKind 章节类型。
type docSectionKind string

const (
	sectionCover      docSectionKind = "cover"      // 封面
	sectionRevisions  docSectionKind = "revisions"  // 文档修订记录
	sectionTOC        docSectionKind = "toc"        // 目录
	sectionPreface    docSectionKind = "preface"    // 文档公共描述（可多条）
	sectionConvention docSectionKind = "convention" // 接口统一定义规范
	sectionAPIs       docSectionKind = "apis"       // 接口正文
	sectionAppendix   docSectionKind = "appendix"   // 附录（HTTP 状态码）
)

// docSection 导出文档中的一章。Content 为简化 Markdown（前言 / 规范正文）。
type docSection struct {
	Kind      docSectionKind
	ID        string // 锚点 / Word 书签名（目录链接用）
	Title     string
	Content   string
	Chapter   model.DocChapter
	Cover     model.DocCover
	Revisions []model.DocRevision
}

// EffectiveDocLayout 返回实际生效的文档结构：项目级覆盖优先，其次用「文档模板」的全局配置。
func EffectiveDocLayout(data model.AppData, proj model.Project) model.DocLayout {
	if proj.ID != "" {
		if l, ok := data.Settings.ProjectDocLayouts[proj.ID]; ok {
			return l
		}
	}
	return data.Settings.DocLayout
}

// normalizeDocLayout 补齐排版配置的默认值：封面标题/日期为空时用文档标题与当天日期；
// 去掉标题为空的前言章节，避免出现空章节。
func normalizeDocLayout(layout model.DocLayout, docTitle string, now time.Time) model.DocLayout {
	out := layout
	if out.Cover.Enabled {
		if strings.TrimSpace(out.Cover.Title) == "" {
			out.Cover.Title = docTitle
		}
		if strings.TrimSpace(out.Cover.Date) == "" {
			out.Cover.Date = now.Format("2006-01-02")
		}
	}
	kept := make([]model.DocChapter, 0, len(out.Prefaces))
	for _, c := range out.Prefaces {
		if strings.TrimSpace(c.Title) == "" && strings.TrimSpace(c.Content) == "" {
			continue // 完全空白的章节直接丢掉
		}
		if strings.TrimSpace(c.Title) == "" {
			c.Title = fmt.Sprintf("说明 %d", len(kept)+1)
		}
		kept = append(kept, c)
	}
	out.Prefaces = kept
	out.Convention = strings.TrimSpace(out.Convention)
	return out
}

// buildDocSections 按企业文档惯例组装章节顺序：
// 封面 → 修订记录 → 目录 → 文档公共描述 → 接口统一定义规范 → 接口正文 → 附录（状态码）。
func buildDocSections(docTitle string, layout model.DocLayout, now time.Time) []docSection {
	layout = normalizeDocLayout(layout, docTitle, now)
	var out []docSection

	if layout.Cover.Enabled {
		out = append(out, docSection{Kind: sectionCover, ID: "cover", Cover: layout.Cover})
	}
	if len(layout.Revisions) > 0 {
		out = append(out, docSection{
			Kind: sectionRevisions, ID: "revisions", Title: "文档修订记录", Revisions: layout.Revisions,
		})
	}
	if layout.Toc {
		out = append(out, docSection{Kind: sectionTOC, ID: "toc", Title: "目录"})
	}
	for i, c := range layout.Prefaces {
		out = append(out, docSection{
			Kind: sectionPreface, ID: fmt.Sprintf("preface-%d", i+1), Title: c.Title, Content: c.Content, Chapter: c,
		})
	}
	if layout.Convention != "" {
		out = append(out, docSection{
			Kind: sectionConvention, ID: "convention", Title: "接口统一定义规范", Content: layout.Convention,
		})
	}
	out = append(out, docSection{Kind: sectionAPIs, ID: "apis", Title: docTitle})
	if layout.StatusCodes {
		out = append(out, docSection{Kind: sectionAppendix, ID: "appendix-status", Title: "附录：HTTP 状态码"})
	}
	return out
}

// DefaultConventionTemplate 内置的「接口统一定义规范」模板，用户可一键填入后按团队要求修改。
func DefaultConventionTemplate() string { return defaultConventionTemplate }

// ---------------- 章节编号 ----------------

// docNumbering 章节与接口树的编号表：章节 1、1.1、1.1.1…，按渲染顺序（先接口、后子目录）递增。
// 编号在渲染前统一算好，三种渲染器只需查表，保证 Word / PDF / Markdown 编号完全一致。
type docNumbering struct {
	enabled  bool
	sections map[string]string // 章节 ID → 编号
	dirs     map[string]string // 目录 ID → 编号
	apis     map[string]string // 接口 ID → 编号
}

// buildDocNumbering 计算编号表；关闭编号时返回空表（各取标签函数原样返回标题）。
func buildDocNumbering(sections []docSection, dirs []model.Directory, apis []model.ApiInfo, rootID string, enabled bool) docNumbering {
	n := docNumbering{
		enabled:  enabled,
		sections: map[string]string{},
		dirs:     map[string]string{},
		apis:     map[string]string{},
	}
	if !enabled {
		return n
	}
	ch := 0
	for _, s := range sections {
		// 封面与目录页本身不参与编号
		if s.Kind == sectionCover || s.Kind == sectionTOC {
			continue
		}
		ch++
		label := fmt.Sprint(ch)
		n.sections[s.ID] = label
		if s.Kind == sectionAPIs {
			if dirs == nil && len(apis) == 1 && rootID == "" {
				n.apis[apis[0].ID] = label + ".1"
			} else {
				n.numberTree(label, dirs, apis, rootID)
			}
		}
	}
	return n
}

// numberTree 递归为接口树编号：同一层内按渲染顺序（接口在前、子目录在后）依次递增。
func (n *docNumbering) numberTree(prefix string, dirs []model.Directory, apis []model.ApiInfo, parentID string) {
	seq := 0
	for _, api := range dirApis(apis, parentID) {
		seq++
		n.apis[api.ID] = fmt.Sprintf("%s.%d", prefix, seq)
	}
	for _, d := range childDirs(dirs, parentID) {
		seq++
		label := fmt.Sprintf("%s.%d", prefix, seq)
		n.dirs[d.ID] = label
		n.numberTree(label, dirs, apis, d.ID)
	}
}

// section 返回带编号的章节标题。
func (n docNumbering) section(id, title string) string {
	if label, ok := n.sections[id]; ok {
		return label + " " + title
	}
	return title
}

// dir 返回带编号的目录标题。
func (n docNumbering) dir(id, name string) string {
	if label, ok := n.dirs[id]; ok {
		return label + " " + name
	}
	return name
}

// api 返回带编号的接口标题。
func (n docNumbering) api(id, name string) string {
	if label, ok := n.apis[id]; ok {
		return label + " " + name
	}
	return name
}

// ---------------- 预置文档结构模板 ----------------

// DocPreset 一套预置文档结构（只含 Key/Label/Desc，具体配置由 DocPresetLayout 生成）。
type DocPreset struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Desc  string   `json:"desc"`
	Tag   string   `json:"tag"`   // 适用场景标签，便于前端分组展示
	Chips []string `json:"chips"` // 章节构成（封面 / 目录 / 规范…），前端以标签形式展示
}

// DocPresetList 预置模板清单（供前端「模板库」展示，一键套用）。
func DocPresetList() []DocPreset {
	return []DocPreset{
		{Key: "internal", Label: "内部版", Tag: "推荐",
			Desc:  "封面 + 修订记录 + 目录 + 文档说明 + 统一规范 + 附录状态码，适合内部研发协作",
			Chips: []string{"封面", "修订记录", "目录", "编号", "公共描述", "接入地址", "公共请求头", "加密验签", "统一规范", "附录"}},
		{Key: "external", Label: "对外版", Tag: "对外",
			Desc:  "封面 + 目录 + 统一规范 + 附录状态码（不含修订记录），适合交付客户 / 合作方",
			Chips: []string{"封面", "目录", "编号", "接入地址", "公共请求头", "加密验签", "统一规范", "附录"}},
		{Key: "rest", Label: "标准 RESTful 版", Tag: "通用",
			Desc:  "目录 + 统一规范 + 附录状态码，不带封面，适合开放平台 / 网关文档",
			Chips: []string{"目录", "编号", "接入地址", "公共请求头", "加密验签", "统一规范", "附录"}},
		{Key: "review", Label: "评审版", Tag: "评审",
			Desc:  "封面 + 修订记录 + 目录 + 评审说明 + 统一规范，供接口评审会使用",
			Chips: []string{"封面", "修订记录", "目录", "编号", "评审说明", "接入地址", "公共请求头", "加密验签", "统一规范"}},
		{Key: "micro", Label: "微服务版", Tag: "架构",
			Desc:  "封面 + 目录 + 服务与调用约定 + 统一规范 + 附录状态码，适合多服务协同",
			Chips: []string{"封面", "目录", "编号", "服务约定", "接入地址", "公共请求头", "加密验签", "统一规范", "附录"}},
		{Key: "minimal", Label: "精简版", Tag: "精简",
			Desc:  "仅目录 + 接口正文，最快出稿，适合临时交付或内部速查",
			Chips: []string{"目录", "编号"}},
	}
}

// DocPresetLayout 生成预置模板的排版配置；未知 key 回退为精简版。
func DocPresetLayout(key string) model.DocLayout {
	today := time.Now().Format("2006-01-02")
	base := model.DocLayout{Toc: true, Numbering: true}

	switch key {
	case "internal":
		base.Cover = model.DocCover{Enabled: true, Subtitle: "内部技术文档 · 禁止外传", Version: "V1.0.0"}
		base.Prefaces = []model.DocChapter{docChapterIntro()}
		base.Environment, base.CommonHeaders, base.Sign = true, true, DefaultSignTemplate()
		base.Convention = DefaultConventionTemplate()
		base.StatusCodes = true
		base.Revisions = []model.DocRevision{{Version: "V1.0.0", Date: today, Note: "首次发布"}}
	case "external":
		base.Cover = model.DocCover{Enabled: true, Subtitle: "对外接口文档", Version: "V1.0.0"}
		base.Environment, base.CommonHeaders, base.Sign = true, true, DefaultSignTemplate()
		base.Convention = DefaultConventionTemplate()
		base.StatusCodes = true
	case "rest":
		base.Environment, base.CommonHeaders, base.Sign = true, true, DefaultSignTemplate()
		base.Convention = DefaultConventionTemplate()
		base.StatusCodes = true
	case "review":
		base.Cover = model.DocCover{Enabled: true, Subtitle: "接口评审稿", Version: "V0.9.0"}
		base.Prefaces = []model.DocChapter{{
			Title: "评审说明",
			Content: "#### 评审范围\n- 本次新增 / 变更的接口清单\n- 参数与响应结构的合理性\n\n" +
				"#### 评审要点\n- 命名与路径是否符合统一规范\n- 是否存在破坏性变更、是否需要升版本\n" +
				"- 权限与鉴权是否明确、错误码是否覆盖异常分支\n\n" +
				"| 结论 | 记录 |\n|---|---|\n| 通过 / 待修改 | |\n",
		}}
		base.Environment, base.CommonHeaders, base.Sign = true, true, DefaultSignTemplate()
		base.Convention = DefaultConventionTemplate()
		base.Revisions = []model.DocRevision{{Version: "V0.9.0", Date: today, Note: "提交评审"}}
	case "micro":
		base.Cover = model.DocCover{Enabled: true, Subtitle: "微服务接口文档", Version: "V1.0.0"}
		base.Prefaces = []model.DocChapter{{
			Title: "服务与调用约定",
			Content: "#### 服务信息\n- 服务名：`order-service`（按实际填写）\n- 网关前缀：`/api/order`\n\n" +
				"#### 调用约定\n- 内部调用走服务发现，禁止硬编码 IP\n" +
				"- 超时：连接 1s / 读取 3s；仅幂等接口可重试，最多 2 次\n" +
				"- 链路追踪：透传 `X-Request-Id` 与 `X-Trace-Id`\n\n" +
				"#### 鉴权\n- 网关校验 `Authorization: Bearer <token>`，服务内二次校验租户 `X-Tenant-Id`\n",
		}}
		base.Environment, base.CommonHeaders, base.Sign = true, true, DefaultSignTemplate()
		base.Convention = DefaultConventionTemplate()
		base.StatusCodes = true
	default: // minimal：仅目录 + 接口正文
	}
	return base
}

// docChapterIntro 内部版默认的「文档说明」章节内容。
func docChapterIntro() model.DocChapter {
	return model.DocChapter{
		Title: "文档说明",
		Content: "#### 适用范围\n- 本文档覆盖当前项目全部对外接口\n\n" +
			"#### 阅读对象\n- 前端 / 后端 / 测试同学\n\n" +
			"#### 术语约定\n| 术语 | 说明 |\n|---|---|\n" +
			"| 业务状态码 | 响应体中的 `code` 字段 |\n" +
			"| 鉴权 | 请求头 `Authorization: Bearer <token>` |\n",
	}
}

// ---------------- 企业标准章节：接入地址 / 公共请求头 / 加密验签 ----------------

// EnrichDocLayout 依据项目环境与公共参数，补齐「接入地址 / 公共请求头 / 加密与验签」章节。
// 这些内容来自项目真实配置，放在渲染前动态生成，Markdown / HTML(PDF) / Word 共用同一份文本。
func EnrichDocLayout(layout model.DocLayout, envs []model.Environment, common model.CommonParams) model.DocLayout {
	extra := make([]model.DocChapter, 0, 3)
	if layout.Environment {
		if c := envChapter(envs); c != "" {
			extra = append(extra, model.DocChapter{Title: "接入地址（环境）", Content: c})
		}
	}
	if layout.CommonHeaders {
		if c := headersChapter(common); c != "" {
			extra = append(extra, model.DocChapter{Title: "公共请求头与公共参数", Content: c})
		}
	}
	if sign := strings.TrimSpace(layout.Sign); sign != "" {
		extra = append(extra, model.DocChapter{Title: "加密与验签", Content: sign})
	}
	if len(extra) == 0 {
		return layout
	}
	out := layout
	out.Prefaces = append(append([]model.DocChapter{}, layout.Prefaces...), extra...)
	return out
}

// envChapter 把项目的环境列表渲染成「接入地址」表格。
func envChapter(envs []model.Environment) string {
	if len(envs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("| 环境 | 接口地址 | 说明 |\n|---|---|---|\n")
	for _, e := range envs {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = "未命名环境"
		}
		addr := envBaseURL(e)
		if addr == "" {
			addr = "—"
		}
		sb.WriteString("| " + mdCell(name) + " | " + mdCell(addr) + " | " + mdCell(envDesc(name)) + " |\n")
	}
	sb.WriteString("\n> 以上地址取自项目环境变量（`baseUrl` / `host` / `domain`），切换环境时请同步替换。" +
		"`appId`、`appSecret` 等敏感变量请在各端配置中心维护，不要写入文档正文。\n")
	return sb.String()
}

// envBaseURL 从环境变量里挑出接口根地址（优先 baseUrl，其次包含 url/host/domain 的键）。
func envBaseURL(e model.Environment) string {
	best := ""
	for _, v := range e.Vars {
		key := strings.ToLower(strings.TrimSpace(v.Key))
		val := strings.TrimSpace(v.Value)
		if key == "" || val == "" || !v.Enabled {
			continue
		}
		switch {
		case key == "baseurl" || key == "base_url":
			return val
		case best == "" && (strings.Contains(key, "url") || strings.Contains(key, "host") || strings.Contains(key, "domain")):
			best = val
		}
	}
	return best
}

// envDesc 按环境名给出常见用途说明。
func envDesc(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "test") || strings.Contains(name, "测试") || strings.Contains(name, "联调"):
		return "联调测试环境，数据可随时重置"
	case strings.Contains(lower, "prod") || strings.Contains(name, "正式") || strings.Contains(name, "生产"):
		return "生产环境，谨慎操作，勿使用测试数据"
	case strings.Contains(lower, "pre") || strings.Contains(name, "预发") || strings.Contains(name, "灰度"):
		return "预发环境，上线前验收使用"
	case strings.Contains(lower, "dev") || strings.Contains(name, "开发"):
		return "开发环境，仅供研发自测"
	}
	return ""
}

// headersChapter 把项目的公共请求头 / 公共查询参数渲染成表格。
func headersChapter(common model.CommonParams) string {
	var sb strings.Builder
	if len(common.Headers) > 0 {
		sb.WriteString("#### 公共请求头\n\n| 请求头 | 示例值 | 说明 |\n|---|---|---|\n")
		for _, kv := range common.Headers {
			key := strings.TrimSpace(kv.Key)
			if key == "" {
				continue
			}
			sb.WriteString("| `" + mdCell(key) + "` | " + mdCell(kv.Value) + " | 公共请求头，所有接口都需携带 |\n")
		}
		sb.WriteString("\n")
	}
	if len(common.Query) > 0 {
		sb.WriteString("#### 公共查询参数\n\n| 参数 | 示例值 | 说明 |\n|---|---|---|\n")
		for _, kv := range common.Query {
			key := strings.TrimSpace(kv.Key)
			if key == "" {
				continue
			}
			sb.WriteString("| `" + mdCell(key) + "` | " + mdCell(kv.Value) + " | 公共查询参数，所有接口都需携带 |\n")
		}
		sb.WriteString("\n")
	}
	if sb.Len() == 0 {
		return ""
	}
	sb.WriteString("> 以上取自项目「公共参数」，可在接口调试页统一维护，导出时自动写入文档。\n")
	return sb.String()
}

// mdCell 转义表格单元格：去掉换行、避免 | 破坏表格结构。
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	return s
}

// DefaultSignTemplate 内置的「加密与验签」说明模板（HMAC-SHA256 签名 + 可选报文加密）。
func DefaultSignTemplate() string { return defaultSignTemplate }

const defaultSignTemplate = "#### 1. 请求签名\n" +
	"- 待签名字符串：`HTTPMethod` + `\\n` + `Path` + `\\n` + 按 key 升序拼接的 query + `\\n` + `X-Timestamp` + `\\n` + `X-Nonce`\n" +
	"- 算法：`HMAC-SHA256`，密钥为平台分配的 `appSecret`，输出**大写十六进制**\n" +
	"- 需携带的请求头：`X-App-Id`（应用标识）、`X-Timestamp`（毫秒时间戳）、`X-Nonce`（随机串）、`X-Sign`（签名值）\n" +
	"- 防重放：时间戳与服务端相差超过 5 分钟即拒绝；同一 `X-Nonce` 在 5 分钟内不可重复使用\n\n" +
	"```text\n" +
	"Method: POST\nPath: /user-center/users\nQuery: page=1&size=20\n" +
	"Timestamp: 1730000000000\nNonce: 8f2c1e\nSign: 9C1B7F...\n" +
	"```\n\n" +
	"#### 2. 报文加密（如需）\n" +
	"- 请求体整体加密：`AES-128-CBC`，`key` 取 `appSecret` 前 16 字节，`iv` 随机 16 字节并以 `X-Iv` 传递\n" +
	"- 加密后请求体形如 `{\"encrypted\": true, \"data\": \"<base64>\"}`，服务端解密后再解析\n" +
	"- 响应加密同上，客户端先解密再按业务结构解析\n\n" +
	"#### 3. 参数校验约定\n" +
	"- 所有金额单位为「分」；时间统一 `yyyy-MM-dd HH:mm:ss`（东八区）\n" +
	"- 签名失败统一返回 HTTP 200 + 业务码 `10003`；时间戳过期返回 `10004`\n"

const defaultConventionTemplate = `#### 1. 命名与路径
- 路径全部小写，单词间用短横线：` + "`/user-center/users/{id}`" + `
- 资源名用复数；动作由 HTTP 方法表达，不写进路径（避免 ` + "`/getUser`" + `）
- 路径参数统一用花括号：` + "`{id}`" + `；查询参数用驼峰或下划线均可，但同一项目需统一

#### 2. 请求约定
- 编码：UTF-8；请求体类型：` + "`application/json`" + `（文件上传用 ` + "`multipart/form-data`" + `）
- 分页参数：` + "`page`" + `（从 1 开始）、` + "`pageSize`" + `（默认 20，最大 200）
- 时间格式：` + "`yyyy-MM-dd HH:mm:ss`" + `（东八区），跨时区场景改用 ISO 8601 带时区
- 幂等：GET / PUT / DELETE 必须幂等，POST 非幂等

#### 3. 响应约定
- 统一结构：` + "`{ \"code\": 0, \"message\": \"ok\", \"data\": {} }`" + `
- ` + "`code`" + ` 为业务状态码（0 表示成功），HTTP 状态码仅表达传输层语义
- 列表响应：` + "`data: { list: [], total: 0, page: 1, pageSize: 20 }`" + `
- 金额统一用字符串（分）或整型（分），禁止使用浮点数

#### 4. 鉴权与安全
- 统一在请求头携带：` + "`Authorization: Bearer <token>`" + `
- 敏感字段（手机号、身份证、密码）响应需脱敏；禁止在日志中打印明文
- 写接口必须校验幂等令牌或业务唯一键，避免重复提交

#### 5. 错误码
- 4xx：调用方问题（参数、权限、资源不存在），需返回可读 ` + "`message`" + ` 与定位字段
- 5xx：服务端问题，禁止把堆栈返给调用方
- 业务错误码分段管理，例如 ` + "`1xxxx`" + ` 用户域、` + "`2xxxx`" + ` 订单域

#### 6. 版本与兼容
- 破坏性变更必须提升路径版本：` + "`/v2/...`" + `，并保留旧版本至少一个大版本周期
- 新增可选字段属于兼容变更；删除/改名/改类型属于破坏性变更`

// statusCodeRow 附录中的一行 HTTP 状态码说明。
type statusCodeRow struct {
	Code    int
	Name    string
	Meaning string
	Usage   string
}

// commonStatusCodes 企业接口文档附录常用的 HTTP 状态码。
var commonStatusCodes = []statusCodeRow{
	{200, "OK", "请求成功", "GET / PUT / PATCH 成功返回数据"},
	{201, "Created", "资源创建成功", "POST 新增成功后返回，建议同时返回资源 ID"},
	{204, "No Content", "成功但无响应体", "DELETE 成功、无需返回数据的写操作"},
	{301, "Moved Permanently", "资源永久重定向", "域名或路径永久迁移，需客户端更新地址"},
	{304, "Not Modified", "资源未变更", "配合 ETag / If-None-Match 做协商缓存"},
	{400, "Bad Request", "请求参数错误", "参数缺失、格式错误、校验不通过"},
	{401, "Unauthorized", "未认证或凭证失效", "token 缺失 / 过期 / 签名错误"},
	{403, "Forbidden", "无访问权限", "已认证但角色/数据权限不足"},
	{404, "Not Found", "资源不存在", "ID 不存在，或无权得知其存在"},
	{409, "Conflict", "资源冲突", "唯一键重复、并发更新冲突、状态机非法流转"},
	{413, "Payload Too Large", "请求体过大", "超出上传大小限制"},
	{415, "Unsupported Media Type", "媒体类型不支持", "Content-Type 与接口约定不符"},
	{422, "Unprocessable Entity", "语义校验失败", "结构正确但业务规则不满足"},
	{429, "Too Many Requests", "请求过于频繁", "触发限流，需返回 Retry-After"},
	{500, "Internal Server Error", "服务端内部错误", "未捕获异常，禁止返回堆栈信息"},
	{502, "Bad Gateway", "网关错误", "上游服务不可用或返回非法响应"},
	{503, "Service Unavailable", "服务暂不可用", "发布、过载、熔断降级期间"},
	{504, "Gateway Timeout", "网关超时", "上游处理超时，需配合超时与重试策略"},
}

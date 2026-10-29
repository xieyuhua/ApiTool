package doc

// Markdown 渲染：生成便于 Git / Wiki 阅读的接口文档。

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"apitool/internal/model"
)

func mdFieldRows(sb *strings.Builder, fields []*model.Field, depth int) {
	for _, f := range fields {
		indent := strings.Repeat("&nbsp;&nbsp;&nbsp;", depth)
		prefix := ""
		if depth > 0 {
			prefix = "└ "
		}
		sb.WriteString(fmt.Sprintf("| %s%s%s | %s | %s | %s | %s |\n",
			indent, prefix, MdEscape(f.Name), f.Type, boolCN(f.Required), MdEscape(f.Description), MdEscape(f.Example)))
		mdFieldRows(sb, f.Children, depth+1)
	}
}

func mdFieldTable(sb *strings.Builder, title string, fields []*model.Field) {
	if len(fields) == 0 {
		return
	}
	sb.WriteString("**" + title + "**\n\n")
	sb.WriteString("| 字段名 | 类型 | 必填 | 说明 | 示例 |\n|---|---|---|---|---|\n")
	mdFieldRows(sb, fields, 0)
	sb.WriteString("\n")
}

func mdKVTable(sb *strings.Builder, title string, kvs []model.KV) {
	kvs = EnabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	sb.WriteString("**" + title + "**\n\n")
	sb.WriteString("| 参数名 | 值/示例 | 说明 |\n|---|---|---|\n")
	for _, kv := range kvs {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", MdEscape(kv.Key), MdEscape(kv.Value), MdEscape(kv.Description)))
	}
	sb.WriteString("\n")
}

// mdFormTable 渲染表单参数（含文件类型）
func mdFormTable(sb *strings.Builder, title string, kvs []model.KV) {
	kvs = EnabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	sb.WriteString("**" + title + "**\n\n")
	sb.WriteString("| 字段名 | 类型 | 值/文件 | 说明 |\n|---|---|---|---|\n")
	for _, kv := range kvs {
		typ := "文本"
		val := kv.Value
		if kv.Type == model.FormTypeFile {
			typ = "文件"
			if val == "" {
				val = "（未选择文件）"
			} else {
				_, name := filepath.Split(val)
				val = "📎 " + name
			}
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", MdEscape(kv.Key), typ, MdEscape(val), MdEscape(kv.Description)))
	}
	sb.WriteString("\n")
}

func mdApi(sb *strings.Builder, api model.ApiInfo, level int, common model.CommonParams, num docNumbering) {
	h := strings.Repeat("#", minInt(level, 6))
	sb.WriteString(fmt.Sprintf("%s %s\n\n", h, num.api(api.ID, api.Name)))
	sb.WriteString(fmt.Sprintf("`%s` `%s`\n\n", strings.ToUpper(api.Method), api.URL))
	if api.Description != "" {
		sb.WriteString("> " + api.Description + "\n\n")
	}
	mdKVTable(sb, labelReqHeaders, api.Headers)
	mdKVTable(sb, labelQueryParams, api.Query)
	mdFormTable(sb, labelFormParams, api.FormItems)
	// 公共参数：项目级，自动附加到所有接口（接口同名覆盖公共）
	mdKVTable(sb, labelCommonHeaders, common.Headers)
	mdKVTable(sb, labelCommonQuery, common.Query)
	mdFieldTable(sb, labelReqFields, api.ReqFields)
	mdFieldTable(sb, labelRespFields, api.RespFields)
	if api.BodyType == "json" && strings.TrimSpace(api.Body) != "" {
		sb.WriteString("**请求示例**\n\n```json\n" + api.Body + "\n```\n\n")
	}
	if body := responseSampleBody(api); body != "" {
		sb.WriteString("**响应示例**\n\n```json\n" + body + "\n```\n\n")
	}
	sb.WriteString("---\n\n")
}

func mdDir(sb *strings.Builder, dirs []model.Directory, apis []model.ApiInfo, parentID string, level int, common model.CommonParams, num docNumbering) {
	for _, api := range dirApis(apis, parentID) {
		mdApi(sb, api, level, common, num)
	}
	for _, d := range childDirs(dirs, parentID) {
		h := strings.Repeat("#", minInt(level, 6))
		sb.WriteString(fmt.Sprintf("%s %s\n\n", h, num.dir(d.ID, d.Name)))
		mdDir(sb, dirs, apis, d.ID, level+1, common, num)
	}
}

// buildMarkdown 生成 Markdown 文档，按 layout 输出封面 / 修订记录 / 目录 / 公共描述 /
// 统一定义规范 / 接口正文 / 附录。
func buildMarkdown(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, layout model.DocLayout) string {
	var sb strings.Builder
	now := time.Now()
	sections := buildDocSections(title, layout, now)
	num := buildDocNumbering(sections, dirs, apis, rootID, layout.Numbering)
	covered := false
	for _, s := range sections {
		switch s.Kind {
		case sectionCover:
			covered = true
			mdCover(&sb, s.Cover)
		case sectionRevisions:
			mdRevisions(&sb, num.section(s.ID, s.Title), s.Revisions)
		case sectionTOC:
			mdTOC(&sb, sections, dirs, apis, rootID, num)
		case sectionPreface:
			sb.WriteString("## " + num.section(s.ID, s.Title) + "\n\n")
			sb.WriteString(strings.TrimSpace(miniMarkdownToText(s.Content)) + "\n\n")
		case sectionConvention:
			sb.WriteString("## " + num.section(s.ID, s.Title) + "\n\n")
			sb.WriteString(strings.TrimSpace(miniMarkdownToText(s.Content)) + "\n\n")
		case sectionAPIs:
			if !covered {
				sb.WriteString("# " + s.Title + "\n\n")
				sb.WriteString("> 导出时间：" + now.Format("2006-01-02 15:04:05") + "\n\n")
			} else if len(sections) > 1 {
				sb.WriteString("## " + num.section(s.ID, labelAPISection) + "\n\n")
			}
			if dirs == nil && len(apis) == 1 && rootID == "" {
				mdApi(&sb, apis[0], 2, common, num)
			} else {
				mdDir(&sb, dirs, apis, rootID, 2, common, num)
			}
		case sectionAppendix:
			mdStatusCodes(&sb, num.section(s.ID, s.Title))
		case sectionBizCodes:
			sb.WriteString("## " + num.section(s.ID, s.Title) + "\n\n" + s.Content + "\n")
		}
	}
	return sb.String()
}

// BuildMarkdown 生成 Markdown 文档文本（供分享/复制场景复用内部渲染逻辑）。
func BuildMarkdown(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams) string {
	return buildMarkdown(title, rootID, dirs, apis, common, model.DocLayout{})
}

// BuildMarkdownWithLayout 生成带企业文档排版（封面 / 目录 / 前言 / 规范 / 附录 / 修订记录）的 Markdown。
func BuildMarkdownWithLayout(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, layout model.DocLayout) string {
	return buildMarkdown(title, rootID, dirs, apis, common, layout)
}

// ---------------- 企业文档章节 ----------------

// mdCover 输出 Markdown 封面。
func mdCover(sb *strings.Builder, c model.DocCover) {
	sb.WriteString("# " + c.Title + "\n\n")
	if c.Subtitle != "" {
		sb.WriteString("**" + c.Subtitle + "**\n\n")
	}
	for _, line := range [][2]string{{"作者", c.Author}, {"单位", c.Org}, {"版本", c.Version}, {"日期", c.Date}} {
		if strings.TrimSpace(line[1]) != "" {
			sb.WriteString("- " + line[0] + "：" + line[1] + "\n")
		}
	}
	sb.WriteString("\n---\n\n")
}

// mdRevisions 输出修订记录表。
func mdRevisions(sb *strings.Builder, title string, rows []model.DocRevision) {
	sb.WriteString("## " + title + "\n\n")
	sb.WriteString("| 版本 | 日期 | 作者 | 修订说明 |\n|---|---|---|---|\n")
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			MdEscape(r.Version), MdEscape(r.Date), MdEscape(r.Author), MdEscape(r.Note)))
	}
	sb.WriteString("\n")
}

// mdTOC 输出目录（Markdown 用缩进列表，不做锚点链接以保证各平台兼容）。
func mdTOC(sb *strings.Builder, sections []docSection, dirs []model.Directory, apis []model.ApiInfo, rootID string, num docNumbering) {
	sb.WriteString("## 目录\n\n")
	mdTOCSections(sb, sections, num)
	sb.WriteString("\n")

	// 接口树：目录与接口名展开成两级列表
	var walk func(parentID string, depth int)
	walk = func(parentID string, depth int) {
		indent := strings.Repeat("  ", depth)
		for _, api := range dirApis(apis, parentID) {
			sb.WriteString(indent + "- " + num.api(api.ID, api.Name) + "（" + strings.ToUpper(api.Method) + " " + api.URL + "）\n")
		}
		for _, d := range childDirs(dirs, parentID) {
			sb.WriteString(indent + "- " + num.dir(d.ID, d.Name) + "\n")
			walk(d.ID, depth+1)
		}
	}
	if dirs != nil || rootID != "" {
		walk(rootID, 1)
	}
}

// mdTOCSections 输出章节级目录项（不含接口树的展开）。
func mdTOCSections(sb *strings.Builder, sections []docSection, num docNumbering) {
	for _, s := range sections {
		switch s.Kind {
		case sectionCover, sectionTOC:
			continue
		case sectionAPIs:
			sb.WriteString("- " + num.section(s.ID, labelAPISection) + "\n")
		default:
			sb.WriteString("- " + num.section(s.ID, s.Title) + "\n")
		}
	}
}

// mdStatusCodes 输出 HTTP 状态码附录。
func mdStatusCodes(sb *strings.Builder, title string) {
	sb.WriteString("## " + title + "\n\n")
	sb.WriteString("| 状态码 | 名称 | 含义 | 典型场景 |\n|---|---|---|---|\n")
	for _, sc := range commonStatusCodes {
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n", sc.Code, sc.Name, MdEscape(sc.Meaning), MdEscape(sc.Usage)))
	}
	sb.WriteString("\n")
}

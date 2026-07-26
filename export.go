package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"
)

func childDirs(dirs []Directory, parentID string) []Directory {
	var out []Directory
	for _, d := range dirs {
		if d.ParentID == parentID {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out
}

func dirApis(apis []ApiInfo, dirID string) []ApiInfo {
	var out []ApiInfo
	for _, a := range apis {
		if a.DirID == dirID {
			out = append(out, a)
		}
	}
	return out
}

func enabledKVs(kvs []KV) []KV {
	var out []KV
	for _, kv := range kvs {
		if kv.Enabled && kv.Key != "" {
			out = append(out, kv)
		}
	}
	return out
}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func boolCN(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// ---------------- Markdown ----------------

func mdFieldRows(sb *strings.Builder, fields []*Field, depth int) {
	for _, f := range fields {
		indent := strings.Repeat("&nbsp;&nbsp;&nbsp;", depth)
		prefix := ""
		if depth > 0 {
			prefix = "└ "
		}
		sb.WriteString(fmt.Sprintf("| %s%s%s | %s | %s | %s | %s |\n",
			indent, prefix, mdEscape(f.Name), f.Type, boolCN(f.Required), mdEscape(f.Description), mdEscape(f.Example)))
		mdFieldRows(sb, f.Children, depth+1)
	}
}

func mdFieldTable(sb *strings.Builder, title string, fields []*Field) {
	if len(fields) == 0 {
		return
	}
	sb.WriteString("**" + title + "**\n\n")
	sb.WriteString("| 字段名 | 类型 | 必填 | 说明 | 示例 |\n|---|---|---|---|---|\n")
	mdFieldRows(sb, fields, 0)
	sb.WriteString("\n")
}

func mdKVTable(sb *strings.Builder, title string, kvs []KV) {
	kvs = enabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	sb.WriteString("**" + title + "**\n\n")
	sb.WriteString("| 参数名 | 值/示例 | 说明 |\n|---|---|---|\n")
	for _, kv := range kvs {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", mdEscape(kv.Key), mdEscape(kv.Value), mdEscape(kv.Description)))
	}
	sb.WriteString("\n")
}

func mdApi(sb *strings.Builder, api ApiInfo, level int) {
	h := strings.Repeat("#", min(level, 6))
	sb.WriteString(fmt.Sprintf("%s %s\n\n", h, api.Name))
	sb.WriteString(fmt.Sprintf("`%s` `%s`\n\n", strings.ToUpper(api.Method), api.URL))
	if api.Description != "" {
		sb.WriteString("> " + api.Description + "\n\n")
	}
	mdKVTable(sb, "请求头", api.Headers)
	mdKVTable(sb, "Query 参数", api.Query)
	mdFieldTable(sb, "请求参数", api.ReqFields)
	mdFieldTable(sb, "响应参数", api.RespFields)
	if api.BodyType == "json" && strings.TrimSpace(api.Body) != "" {
		sb.WriteString("**请求示例**\n\n```json\n" + api.Body + "\n```\n\n")
	}
	if api.LastResponse != nil && api.LastResponse.IsJSON && api.LastResponse.Body != "" {
		body := api.LastResponse.Body
		if len(body) > 4000 {
			body = body[:4000] + "\n... (截断)"
		}
		sb.WriteString("**响应示例**\n\n```json\n" + body + "\n```\n\n")
	}
	sb.WriteString("---\n\n")
}

func mdDir(sb *strings.Builder, dirs []Directory, apis []ApiInfo, parentID string, level int) {
	for _, api := range dirApis(apis, parentID) {
		mdApi(sb, api, level)
	}
	for _, d := range childDirs(dirs, parentID) {
		h := strings.Repeat("#", min(level, 6))
		sb.WriteString(fmt.Sprintf("%s %s\n\n", h, d.Name))
		mdDir(sb, dirs, apis, d.ID, level+1)
	}
}

func buildMarkdown(title, rootID string, dirs []Directory, apis []ApiInfo) string {
	var sb strings.Builder
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("> 导出时间：" + time.Now().Format("2006-01-02 15:04:05") + "\n\n")
	if dirs == nil && len(apis) == 1 && rootID == "" {
		mdApi(&sb, apis[0], 2)
	} else {
		mdDir(&sb, dirs, apis, rootID, 2)
	}
	return sb.String()
}

// ---------------- HTML ----------------

func htmlFieldRows(sb *strings.Builder, fields []*Field, depth int) {
	for _, f := range fields {
		name := html.EscapeString(f.Name)
		pad := depth * 18
		sb.WriteString(fmt.Sprintf(
			`<tr><td style="padding-left:%dpx" class="fname">%s</td><td><span class="ftype">%s</span></td><td>%s</td><td>%s</td><td class="fex">%s</td></tr>`,
			pad+12, name, html.EscapeString(f.Type), boolCN(f.Required),
			html.EscapeString(f.Description), html.EscapeString(f.Example)))
		htmlFieldRows(sb, f.Children, depth+1)
	}
}

func htmlFieldTable(sb *strings.Builder, title string, fields []*Field) {
	if len(fields) == 0 {
		return
	}
	sb.WriteString(`<h4>` + title + `</h4><table><thead><tr><th>字段名</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr></thead><tbody>`)
	htmlFieldRows(sb, fields, 0)
	sb.WriteString(`</tbody></table>`)
}

func htmlKVTable(sb *strings.Builder, title string, kvs []KV) {
	kvs = enabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	sb.WriteString(`<h4>` + title + `</h4><table><thead><tr><th>参数名</th><th>值/示例</th><th>说明</th></tr></thead><tbody>`)
	for _, kv := range kvs {
		sb.WriteString(fmt.Sprintf(`<tr><td class="fname">%s</td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(kv.Key), html.EscapeString(kv.Value), html.EscapeString(kv.Description)))
	}
	sb.WriteString(`</tbody></table>`)
}

func htmlApi(sb *strings.Builder, api ApiInfo) {
	method := strings.ToUpper(api.Method)
	sb.WriteString(fmt.Sprintf(`<div class="api" id="api-%s"><h3>%s</h3>`, api.ID, html.EscapeString(api.Name)))
	sb.WriteString(fmt.Sprintf(`<div class="urlbar"><span class="method m-%s">%s</span><code>%s</code></div>`,
		strings.ToLower(method), method, html.EscapeString(api.URL)))
	if api.Description != "" {
		sb.WriteString(`<p class="desc">` + html.EscapeString(api.Description) + `</p>`)
	}
	htmlKVTable(sb, "请求头", api.Headers)
	htmlKVTable(sb, "Query 参数", api.Query)
	htmlFieldTable(sb, "请求参数", api.ReqFields)
	htmlFieldTable(sb, "响应参数", api.RespFields)
	if api.BodyType == "json" && strings.TrimSpace(api.Body) != "" {
		sb.WriteString(`<h4>请求示例</h4><pre>` + html.EscapeString(api.Body) + `</pre>`)
	}
	if api.LastResponse != nil && api.LastResponse.IsJSON && api.LastResponse.Body != "" {
		body := api.LastResponse.Body
		if len(body) > 4000 {
			body = body[:4000] + "\n... (截断)"
		}
		sb.WriteString(`<h4>响应示例</h4><pre>` + html.EscapeString(body) + `</pre>`)
	}
	sb.WriteString(`</div>`)
}

func htmlDir(sb *strings.Builder, dirs []Directory, apis []ApiInfo, parentID string, level int) {
	for _, api := range dirApis(apis, parentID) {
		htmlApi(sb, api)
	}
	for _, d := range childDirs(dirs, parentID) {
		sb.WriteString(fmt.Sprintf(`<h2 class="dir">%s</h2>`, html.EscapeString(d.Name)))
		htmlDir(sb, dirs, apis, d.ID, level+1)
	}
}

func htmlToc(sb *strings.Builder, dirs []Directory, apis []ApiInfo, parentID string, depth int) {
	for _, api := range dirApis(apis, parentID) {
		sb.WriteString(fmt.Sprintf(`<a class="toc-api" style="padding-left:%dpx" href="#api-%s"><span class="tm tm-%s">%s</span>%s</a>`,
			12+depth*14, api.ID, strings.ToLower(api.Method), strings.ToUpper(api.Method), html.EscapeString(api.Name)))
	}
	for _, d := range childDirs(dirs, parentID) {
		sb.WriteString(fmt.Sprintf(`<div class="toc-dir" style="padding-left:%dpx">%s</div>`, 12+depth*14, html.EscapeString(d.Name)))
		htmlToc(sb, dirs, apis, d.ID, depth+1)
	}
}

func buildHTML(title, rootID string, dirs []Directory, apis []ApiInfo) string {
	var body, toc strings.Builder
	if dirs == nil && len(apis) == 1 && rootID == "" {
		htmlApi(&body, apis[0])
	} else {
		htmlDir(&body, dirs, apis, rootID, 0)
		htmlToc(&toc, dirs, apis, rootID, 0)
	}
	tocHTML := ""
	if toc.Len() > 0 {
		tocHTML = `<nav class="toc"><div class="toc-title">目录</div>` + toc.String() + `</nav>`
	}
	css := `
:root{color-scheme:light}
*{box-sizing:border-box}body{margin:0;font-family:"Segoe UI","Microsoft YaHei",sans-serif;color:#1f2329;background:#f7f8fa}
.layout{display:flex;max-width:1280px;margin:0 auto}
.toc{width:260px;flex-shrink:0;position:sticky;top:0;height:100vh;overflow:auto;padding:24px 8px;border-right:1px solid #e5e6eb;background:#fff}
.toc-title{font-weight:700;padding:0 12px 12px;font-size:15px}
.toc-dir{font-weight:600;font-size:13px;color:#4e5969;padding:8px 12px 4px}
.toc a{display:block;padding:5px 12px;font-size:13px;color:#1f2329;text-decoration:none;border-radius:6px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.toc a:hover{background:#f2f3f5}
.tm{display:inline-block;font-size:10px;font-weight:700;margin-right:6px;width:44px;text-align:center;border-radius:3px;padding:1px 0;color:#fff}
main{flex:1;padding:32px 40px;min-width:0}
h1{font-size:26px;margin:0 0 4px}
.meta{color:#86909c;font-size:13px;margin-bottom:28px}
h2.dir{font-size:20px;margin:36px 0 8px;padding-bottom:8px;border-bottom:2px solid #e5e6eb}
.api{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:20px 24px;margin:16px 0}
.api h3{margin:0 0 12px;font-size:18px}
.api h4{margin:18px 0 8px;font-size:14px;color:#4e5969}
.urlbar{display:flex;align-items:center;gap:10px;background:#f7f8fa;border-radius:6px;padding:8px 12px}
.urlbar code{font-size:14px;word-break:break-all}
.method,.tm{font-family:inherit}
.method{font-size:12px;font-weight:700;padding:3px 10px;border-radius:4px;color:#fff}
.m-get,.tm-get{background:#0fc6c2}.m-post,.tm-post{background:#165dff}.m-put,.tm-put{background:#ff7d00}
.m-delete,.tm-delete{background:#f53f3f}.m-patch,.tm-patch{background:#722ed1}.m-head,.tm-head,.m-options,.tm-options{background:#86909c}
.desc{color:#4e5969;font-size:14px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{border:1px solid #e5e6eb;padding:7px 12px;text-align:left;word-break:break-all}
th{background:#f7f8fa;font-weight:600}
.fname{font-family:Consolas,monospace}
.ftype{color:#165dff;font-size:12px}
.fex{color:#86909c}
pre{background:#1d2129;color:#e5e6eb;border-radius:8px;padding:14px;font-size:13px;overflow:auto;max-height:400px}
@media print{.toc{display:none}}
`
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s - 接口文档</title><style>%s</style></head>
<body><div class="layout">%s<main><h1>%s</h1><div class="meta">共 %d 个接口 · 导出时间 %s · 由 ApiTool 生成</div>%s</main></div></body></html>`,
		html.EscapeString(title), css, tocHTML, html.EscapeString(title), len(apis),
		time.Now().Format("2006-01-02 15:04"), body.String())
}

// ---------------- OpenAPI ----------------

func schemaFromFields(fields []*Field) map[string]interface{} {
	props := map[string]interface{}{}
	var required []string
	for _, f := range fields {
		props[f.Name] = schemaFromField(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	schema := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func schemaFromField(f *Field) map[string]interface{} {
	t := f.Type
	desc := f.Description
	base := func(typ string) map[string]interface{} {
		m := map[string]interface{}{"type": typ}
		if desc != "" {
			m["description"] = desc
		}
		if f.Example != "" && typ != "object" && typ != "array" {
			m["example"] = f.Example
		}
		return m
	}
	switch {
	case t == "object":
		m := schemaFromFields(f.Children)
		if desc != "" {
			m["description"] = desc
		}
		return m
	case strings.HasPrefix(t, "array"):
		var items map[string]interface{}
		if t == "array[object]" || len(f.Children) > 0 {
			items = schemaFromFields(f.Children)
		} else {
			inner := strings.TrimSuffix(strings.TrimPrefix(t, "array["), "]")
			if inner == "" || inner == "array" {
				inner = "string"
			}
			items = map[string]interface{}{"type": inner}
		}
		m := map[string]interface{}{"type": "array", "items": items}
		if desc != "" {
			m["description"] = desc
		}
		return m
	case t == "integer", t == "number", t == "boolean", t == "string":
		return base(t)
	case t == "null":
		return base("string")
	default:
		return base("string")
	}
}

func buildOpenAPI(title string, apis []ApiInfo) (string, error) {
	paths := map[string]map[string]interface{}{}
	for _, api := range apis {
		p := api.URL
		if u, err := url.Parse(api.URL); err == nil && u.Path != "" {
			p = u.Path
		}
		if p == "" {
			p = "/"
		}
		if paths[p] == nil {
			paths[p] = map[string]interface{}{}
		}
		op := map[string]interface{}{
			"summary":     api.Name,
			"description": api.Description,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "成功",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": schemaFromFields(api.RespFields),
						},
					},
				},
			},
		}
		var params []map[string]interface{}
		for _, kv := range enabledKVs(api.Query) {
			params = append(params, map[string]interface{}{
				"name": kv.Key, "in": "query", "description": kv.Description,
				"schema": map[string]string{"type": "string"}, "example": kv.Value,
			})
		}
		for _, kv := range enabledKVs(api.Headers) {
			params = append(params, map[string]interface{}{
				"name": kv.Key, "in": "header", "description": kv.Description,
				"schema": map[string]string{"type": "string"}, "example": kv.Value,
			})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if len(api.ReqFields) > 0 {
			op["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": schemaFromFields(api.ReqFields),
					},
				},
			}
		}
		method := strings.ToLower(api.Method)
		if method == "" {
			method = "get"
		}
		paths[p][method] = op
	}
	doc := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":   title,
			"version": "1.0.0",
		},
		"paths": paths,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

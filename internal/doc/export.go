package doc

import (
	"apitool/internal/model"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func childDirs(dirs []model.Directory, parentID string) []model.Directory {
	var out []model.Directory
	for _, d := range dirs {
		if d.ParentID == parentID {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out
}

func dirApis(apis []model.ApiInfo, dirID string) []model.ApiInfo {
	var out []model.ApiInfo
	for _, a := range apis {
		if a.DirID == dirID {
			out = append(out, a)
		}
	}
	return out
}

func EnabledKVs(kvs []model.KV) []model.KV {
	var out []model.KV
	for _, kv := range kvs {
		if kv.Enabled && kv.Key != "" {
			out = append(out, kv)
		}
	}
	return out
}

func MdEscape(s string) string {
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

// escAttr 用于 HTML 属性值转义（额外转义双引号）
func escAttr(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// ---------------- Markdown ----------------

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

func mdApi(sb *strings.Builder, api model.ApiInfo, level int, common model.CommonParams) {
	h := strings.Repeat("#", minInt(level, 6))
	sb.WriteString(fmt.Sprintf("%s %s\n\n", h, api.Name))
	sb.WriteString(fmt.Sprintf("`%s` `%s`\n\n", strings.ToUpper(api.Method), api.URL))
	if api.Description != "" {
		sb.WriteString("> " + api.Description + "\n\n")
	}
	mdKVTable(sb, "请求头", api.Headers)
	mdKVTable(sb, "Query 参数", api.Query)
	mdFormTable(sb, "表单参数（Form / 文件上传）", api.FormItems)
	// 公共参数：项目级，自动附加到所有接口（接口同名覆盖公共）
	mdKVTable(sb, "公共请求头", common.Headers)
	mdKVTable(sb, "公共 Query 参数", common.Query)
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

func mdDir(sb *strings.Builder, dirs []model.Directory, apis []model.ApiInfo, parentID string, level int, common model.CommonParams) {
	for _, api := range dirApis(apis, parentID) {
		mdApi(sb, api, level, common)
	}
	for _, d := range childDirs(dirs, parentID) {
		h := strings.Repeat("#", minInt(level, 6))
		sb.WriteString(fmt.Sprintf("%s %s\n\n", h, d.Name))
		mdDir(sb, dirs, apis, d.ID, level+1, common)
	}
}

func buildMarkdown(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams) string {
	var sb strings.Builder
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("> 导出时间：" + time.Now().Format("2006-01-02 15:04:05") + "\n\n")
	if dirs == nil && len(apis) == 1 && rootID == "" {
		mdApi(&sb, apis[0], 2, common)
	} else {
		mdDir(&sb, dirs, apis, rootID, 2, common)
	}
	return sb.String()
}

// BuildMarkdown 生成 Markdown 文档文本（供分享/复制场景复用内部渲染逻辑）。
func BuildMarkdown(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams) string {
	return buildMarkdown(title, rootID, dirs, apis, common)
}

// ---------------- HTML ----------------

func htmlFieldRows(sb *strings.Builder, fields []*model.Field, depth int) {
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

func htmlFieldTable(sb *strings.Builder, title string, fields []*model.Field) {
	if len(fields) == 0 {
		return
	}
	sb.WriteString(`<h4>` + title + `</h4><table><thead><tr><th>字段名</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr></thead><tbody>`)
	htmlFieldRows(sb, fields, 0)
	sb.WriteString(`</tbody></table>`)
}

// htmlFormTable 渲染表单参数（含文件类型）
func htmlFormTable(sb *strings.Builder, title string, kvs []model.KV) {
	kvs = EnabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	sb.WriteString(`<h4>` + title + `</h4><table><thead><tr><th>字段名</th><th>类型</th><th>值/文件</th><th>说明</th></tr></thead><tbody>`)
	for _, kv := range kvs {
		typ := "文本"
		val := kv.Value
		if kv.Type == model.FormTypeFile {
			typ = `<span style="color:#165dff">文件</span>`
			if val == "" {
				val = "（未选择文件）"
			} else {
				_, name := filepath.Split(val)
				val = "📎 " + name
			}
		}
		sb.WriteString(fmt.Sprintf(`<tr><td class="fname">%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(kv.Key), typ, html.EscapeString(val), html.EscapeString(kv.Description)))
	}
	sb.WriteString(`</tbody></table>`)
}

func htmlKVTable(sb *strings.Builder, title string, kvs []model.KV) {
	kvs = EnabledKVs(kvs)
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

func htmlApi(sb *strings.Builder, api model.ApiInfo, dirName string, common model.CommonParams) {
	method := strings.ToUpper(api.Method)
	sb.WriteString(fmt.Sprintf(`<div class="api" id="api-%s" data-name="%s" data-url="%s" data-dir="%s"><h3>%s</h3>`,
		api.ID, escAttr(api.Name), escAttr(api.URL), escAttr(dirName), html.EscapeString(api.Name)))
	sb.WriteString(fmt.Sprintf(`<div class="urlbar"><span class="method m-%s">%s</span><code>%s</code></div>`,
		strings.ToLower(method), method, html.EscapeString(api.URL)))
	if api.Description != "" {
		sb.WriteString(`<p class="desc">` + html.EscapeString(api.Description) + `</p>`)
	}
	htmlKVTable(sb, "请求头", api.Headers)
	htmlKVTable(sb, "Query 参数", api.Query)
	htmlFormTable(sb, "表单参数（Form / 文件上传）", api.FormItems)
	htmlKVTable(sb, "公共请求头", common.Headers)
	htmlKVTable(sb, "公共 Query 参数", common.Query)
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

func htmlDir(sb *strings.Builder, dirs []model.Directory, apis []model.ApiInfo, parentID string, level int, dirName string, common model.CommonParams) {
	for _, api := range dirApis(apis, parentID) {
		htmlApi(sb, api, dirName, common)
	}
	for _, d := range childDirs(dirs, parentID) {
		sb.WriteString(fmt.Sprintf(`<h2 class="dir" data-dir="%s">%s</h2>`, escAttr(d.Name), html.EscapeString(d.Name)))
		htmlDir(sb, dirs, apis, d.ID, level+1, d.Name, common)
	}
}

func htmlToc(sb *strings.Builder, dirs []model.Directory, apis []model.ApiInfo, parentID string, depth int, dirName string) {
	for _, api := range dirApis(apis, parentID) {
		sb.WriteString(fmt.Sprintf(`<a class="toc-api" style="padding-left:%dpx" href="#api-%s" data-name="%s" data-url="%s" data-dir="%s"><span class="tm tm-%s">%s</span>%s</a>`,
			12+depth*14, api.ID, escAttr(api.Name), escAttr(api.URL), escAttr(dirName),
			strings.ToLower(api.Method), strings.ToUpper(api.Method), html.EscapeString(api.Name)))
	}
	for _, d := range childDirs(dirs, parentID) {
		sb.WriteString(fmt.Sprintf(`<div class="toc-dir" style="padding-left:%dpx" data-dir="%s">%s</div>`, 12+depth*14, escAttr(d.Name), html.EscapeString(d.Name)))
		htmlToc(sb, dirs, apis, d.ID, depth+1, d.Name)
	}
}

// docCSS 分享/导出 HTML 文档的内联样式，自包含，无需外部样式表。
const docCSS = `
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
.doc-head{margin:0}
h1{font-size:26px;margin:0 0 4px}
.meta{color:#86909c;font-size:13px;margin-bottom:18px}
.doc-search{position:sticky;top:0;background:#f7f8fa;padding:6px 0 14px;z-index:5}
.doc-search input{width:100%;box-sizing:border-box;padding:9px 12px;border:1px solid #e5e6eb;border-radius:8px;font-size:14px;outline:none;background:#fff}
.doc-search input:focus{border-color:#165dff;box-shadow:0 0 0 2px rgba(22,93,255,.12)}
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
.empty-tip{color:#86909c;font-size:14px;padding:40px 0;text-align:center;display:none}
@media print{.toc,.doc-search{display:none}}
`

// docJS 分享/导出 HTML 文档的内联脚本：目录搜索过滤（纯原生 JS，无依赖）。
const docJS = `
<script>
function apitoolMatch(q,hay){hay=(hay||'').toLowerCase();if(hay.indexOf(q)>=0)return true;var i=0;for(var j=0;j<hay.length&&i<q.length;j++){if(hay[j]===q[i])i++;}return i===q.length;}
function apitoolFilter(){var box=document.getElementById('docSearch');var q=box?box.value.trim().toLowerCase():'';var apis=document.querySelectorAll('.api');var tocApis=document.querySelectorAll('.toc-api');var tocDirs=document.querySelectorAll('.toc-dir');var dirs=document.querySelectorAll('h2.dir');
if(!q){apis.forEach(function(a){a.style.display='';});dirs.forEach(function(d){d.style.display='';});tocApis.forEach(function(a){a.style.display='';});tocDirs.forEach(function(d){d.style.display='';});var t=document.getElementById('docEmpty');if(t)t.style.display='none';return;}
apis.forEach(function(a){var hay=(a.getAttribute('data-name')||'')+' '+(a.getAttribute('data-url')||'')+' '+(a.getAttribute('data-dir')||'');a.style.display=apitoolMatch(q,hay)?'':'none';});
tocApis.forEach(function(a){var hay=(a.getAttribute('data-name')||'')+' '+(a.getAttribute('data-url')||'')+' '+(a.getAttribute('data-dir')||'');a.style.display=apitoolMatch(q,hay)?'':'none';});
dirs.forEach(function(d){var name=(d.getAttribute('data-dir')||'').toLowerCase();var any=false;document.querySelectorAll('.api').forEach(function(a){if((a.getAttribute('data-dir')||'')===(d.getAttribute('data-dir')||'')&&a.style.display!=='none')any=true;});d.style.display=(apitoolMatch(q,name)||any)?'':'none';});
tocDirs.forEach(function(d){var name=(d.getAttribute('data-dir')||'').toLowerCase();var any=false;document.querySelectorAll('.toc-api').forEach(function(a){if((a.getAttribute('data-dir')||'')===(d.getAttribute('data-dir')||'')&&a.style.display!=='none')any=true;});d.style.display=(apitoolMatch(q,name)||any)?'':'none';});
var visible=0;apis.forEach(function(a){if(a.style.display!=='none')visible++;});var t=document.getElementById('docEmpty');if(t)t.style.display=visible?'none':'block';}
</script>`

func BuildHTML(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams) string {
	var body, toc strings.Builder
	if dirs == nil && len(apis) == 1 && rootID == "" {
		htmlApi(&body, apis[0], "", common)
	} else {
		htmlDir(&body, dirs, apis, rootID, 0, "", common)
		htmlToc(&toc, dirs, apis, rootID, 0, "")
	}
	tocHTML := ""
	if toc.Len() > 0 {
		tocHTML = `<nav class="toc"><div class="toc-title">目录</div>` + toc.String() + `</nav>`
	}
	css := docCSS
	js := docJS
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s - 接口文档</title><style>%s</style></head>
<body><div class="layout">%s<main>
<div class="doc-head"><h1>%s</h1><div class="meta">共 %d 个接口 · 导出时间 %s · 由 ApiTool 生成</div>
<div class="doc-search"><input id="docSearch" type="search" placeholder="搜索目录 / 接口名称 / 接口地址" oninput="apitoolFilter()" autocomplete="off"></div></div>
%s
<div id="docEmpty" class="empty-tip">没有匹配的接口</div>
</main></div>%s</body></html>`,
		html.EscapeString(title), css, tocHTML, html.EscapeString(title), len(apis),
		time.Now().Format("2006-01-02 15:04"), body.String(), js)
}

// ---------------- OpenAPI ----------------

func schemaFromFields(fields []*model.Field) map[string]interface{} {
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

func schemaFromField(f *model.Field) map[string]interface{} {
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

// BuildOpenAPI 生成标准 OpenAPI 3.0.3 文档。
// hostMode:
//   - "original"：接口地址使用抓包实际完整地址（含 host/path/query），不附加 servers，保证导入后地址不被替换；
//   - "env"：将 host 替换为环境变量 {{host}}，servers=[{url:"{{host}}"}]，path 仅保留路径（Query 以 parameters 形式导出，标准做法）。
func BuildOpenAPI(title string, dirs []model.Directory, apis []model.ApiInfo, rootID string, common model.CommonParams, hostMode string) (string, error) {
	paths := map[string]map[string]interface{}{}
	usedTags := []string{}
	envHost := hostMode == "env"
	for _, api := range apis {
		var p string
		if envHost {
			// 环境变量模式：host 抽成 {{host}}，path 仅保留路径（Query 以 parameters 形式导出）
			if u, err := url.Parse(api.URL); err == nil && (u.Path != "" || u.RawQuery != "") {
				base := u.Path
				if base == "" {
					base = "/"
				}
				p = base
			} else {
				p = api.URL
			}
		} else {
			// 原地址模式：保留抓包实际完整地址（host+path+query），导入其他平台即真实地址
			p = api.URL
		}
		if p == "" {
			p = "/"
		}
		if paths[p] == nil {
			paths[p] = map[string]interface{}{}
		}
		// 目录分组：取接口所属目录的完整层级路径（如 "用户中心 / 用户管理"），无目录则归入"未分类"
		tag := dirPathOf(dirs, api.DirID)
		if tag == "" {
			tag = "未分类"
		}
		usedTags = append(usedTags, tag)
		op := map[string]interface{}{
			"tags":        []string{tag},
			"summary":     p,
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
		// 原地址模式下 Query 已包含在路径完整 URL 中，不再重复；env 模式按标准以 parameters 导出
		if envHost {
			for _, kv := range EnabledKVs(api.Query) {
				params = append(params, map[string]interface{}{
					"name": kv.Key, "in": "query", "description": kv.Description,
					"schema": map[string]string{"type": "string"}, "example": kv.Value,
				})
			}
		}
		for _, kv := range EnabledKVs(api.Headers) {
			params = append(params, map[string]interface{}{
				"name": kv.Key, "in": "header", "description": kv.Description,
				"schema": map[string]string{"type": "string"}, "example": kv.Value,
			})
		}
		// 公共参数：项目级自动附加（接口同名覆盖公共），仅追加接口未定义的
		defined := map[string]bool{}
		for _, kv := range append(append([]model.KV{}, api.Headers...), api.Query...) {
			if kv.Enabled && kv.Key != "" {
				defined[kv.Key] = true
			}
		}
		for _, kv := range EnabledKVs(common.Query) {
			if !defined[kv.Key] {
				params = append(params, map[string]interface{}{
					"name": kv.Key, "in": "query", "description": "[公共参数] " + kv.Description,
					"schema": map[string]string{"type": "string"}, "example": kv.Value,
				})
			}
		}
		for _, kv := range EnabledKVs(common.Headers) {
			if !defined[kv.Key] {
				params = append(params, map[string]interface{}{
					"name": kv.Key, "in": "header", "description": "[公共参数] " + kv.Description,
					"schema": map[string]string{"type": "string"}, "example": kv.Value,
				})
			}
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
		// 表单参数（Form / 文件上传）：生成 multipart/form-data requestBody
		formKVs := EnabledKVs(api.FormItems)
		if len(formKVs) > 0 {
			props := map[string]interface{}{}
			required := []string{}
			for _, kv := range formKVs {
				if kv.Type == model.FormTypeFile {
					props[kv.Key] = map[string]interface{}{
						"type":        "string",
						"format":      "binary",
						"description": kv.Description,
					}
				} else {
					props[kv.Key] = map[string]interface{}{
						"type":        "string",
						"description": kv.Description,
						"example":     kv.Value,
					}
				}
				required = append(required, kv.Key)
			}
			op["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					"multipart/form-data": map[string]interface{}{
						"schema": map[string]interface{}{
							"type":     "object",
							"properties": props,
							"required": required,
						},
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
	servers := []map[string]interface{}{}
	if envHost {
		servers = append(servers, map[string]interface{}{"url": "{{host}}"})
	}
	doc := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":   title,
			"version": "1.0.0",
		},
		"servers": servers,
		"tags":    collectTags(usedTags),
		"paths":   paths,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// dirPathOf 根据目录列表，返回从根到该目录的完整层级路径（如 "用户中心 / 用户管理"）。
func dirPathOf(dirs []model.Directory, id string) string {
	if id == "" {
		return ""
	}
	byID := map[string]model.Directory{}
	for _, d := range dirs {
		byID[d.ID] = d
	}
	chain := []string{}
	cur := id
	guard := 0
	for cur != "" {
		d, ok := byID[cur]
		if !ok {
			break
		}
		chain = append([]string{d.Name}, chain...)
		cur = d.ParentID
		if guard++; guard > 50 {
			break
		}
	}
	return strings.Join(chain, " / ")
}

// collectTags 根据出现的目录路径收集根 tags 声明（含每一层级），便于 Swagger/Postman 按目录分组展示。
func collectTags(usedPaths []string) []map[string]interface{} {
	exists := map[string]bool{}
	var ordered []string
	for _, p := range usedPaths {
		parts := strings.Split(p, " / ")
		prefix := ""
		for _, part := range parts {
			if prefix == "" {
				prefix = part
			} else {
				prefix = prefix + " / " + part
			}
			if !exists[prefix] {
				exists[prefix] = true
				ordered = append(ordered, prefix)
			}
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if strings.Count(ordered[i], " / ") != strings.Count(ordered[j], " / ") {
			return strings.Count(ordered[i], " / ") < strings.Count(ordered[j], " / ")
		}
		return ordered[i] < ordered[j]
	})
	tags := make([]map[string]interface{}, 0, len(ordered))
	for _, p := range ordered {
		tags = append(tags, map[string]interface{}{"name": p})
	}
	return tags
}

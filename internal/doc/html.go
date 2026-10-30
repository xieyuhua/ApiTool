package doc

// HTML 渲染：生成自包含的网页文档（目录导航 + 搜索），同时作为 PDF 导出的输入。

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"time"

	"apitool/internal/model"
)

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
			typ = `<span>文件</span>`
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

func htmlApi(sb *strings.Builder, api model.ApiInfo, dirName string, common model.CommonParams, num docNumbering) {
	method := strings.ToUpper(api.Method)
	sb.WriteString(fmt.Sprintf(`<div class="api" id="api-%s" data-name="%s" data-url="%s" data-dir="%s"><h3>%s</h3>`,
		api.ID, escAttr(api.Name), escAttr(api.URL), escAttr(dirName), html.EscapeString(num.api(api.ID, api.Name))))
	sb.WriteString(fmt.Sprintf(`<div class="urlbar"><span class="method m-%s">%s</span><code>%s</code></div>`,
		strings.ToLower(method), method, html.EscapeString(api.URL)))
	if api.Description != "" {
		sb.WriteString(`<p class="desc">` + html.EscapeString(api.Description) + `</p>`)
	}
	htmlKVTable(sb, labelReqHeaders, api.Headers)
	htmlKVTable(sb, labelQueryParams, api.Query)
	htmlFormTable(sb, labelFormParams, api.FormItems)
	htmlKVTable(sb, labelCommonHeaders, common.Headers)
	htmlKVTable(sb, labelCommonQuery, common.Query)
	htmlFieldTable(sb, labelReqFields, api.ReqFields)
	htmlFieldTable(sb, labelRespFields, api.RespFields)
	if api.BodyType == "json" && strings.TrimSpace(api.Body) != "" {
		sb.WriteString(`<h4>请求示例</h4><pre>` + html.EscapeString(api.Body) + `</pre>`)
	}
	if body := responseSampleBody(api); body != "" {
		sb.WriteString(`<h4>响应示例</h4><pre>` + html.EscapeString(body) + `</pre>`)
	}
	sb.WriteString(`</div>`)
}

func htmlDir(sb *strings.Builder, dirs []model.Directory, apis []model.ApiInfo, parentID string, level int, dirName string, common model.CommonParams, num docNumbering) {
	for _, api := range dirApis(apis, parentID) {
		htmlApi(sb, api, dirName, common, num)
	}
	for _, d := range childDirs(dirs, parentID) {
		sb.WriteString(fmt.Sprintf(`<h2 class="dir" id="dir-%s" data-dir="%s">%s</h2>`, d.ID, escAttr(d.Name), html.EscapeString(num.dir(d.ID, d.Name))))
		htmlDir(sb, dirs, apis, d.ID, level+1, d.Name, common, num)
	}
}

func htmlToc(sb *strings.Builder, dirs []model.Directory, apis []model.ApiInfo, parentID string, depth int, dirName string, num docNumbering) {
	for _, api := range dirApis(apis, parentID) {
		sb.WriteString(fmt.Sprintf(`<a class="toc-api" style="padding-left:%dpx" href="#api-%s" data-name="%s" data-url="%s" data-dir="%s">%s</a>`,
			12+depth*14, api.ID, escAttr(api.Name), escAttr(api.URL), escAttr(dirName),
			html.EscapeString(num.api(api.ID, api.Name))))
	}
	for _, d := range childDirs(dirs, parentID) {
		sb.WriteString(fmt.Sprintf(`<a class="toc-dir" style="padding-left:%dpx" href="#dir-%s" data-dir="%s">%s</a>`, 12+depth*14, d.ID, escAttr(d.Name), html.EscapeString(num.dir(d.ID, d.Name))))
		htmlToc(sb, dirs, apis, d.ID, depth+1, d.Name, num)
	}
}

// docCSS 分享/导出 HTML 文档的内联样式，自包含，无需外部样式表。
const docCSS = `
:root{color-scheme:light}
*{box-sizing:border-box}body{margin:0;font-family:"Segoe UI","Microsoft YaHei",sans-serif;color:#000;background:#f7f8fa}
.layout{display:flex;max-width:1280px;margin:0 auto}
.toc{width:260px;flex-shrink:0;position:sticky;top:0;height:100vh;overflow:auto;padding:24px 8px;border-right:1px solid #e5e6eb;background:#fff}
.toc-title{font-weight:700;padding:0 12px 12px;font-size:15px}
.toc-dir{font-weight:600;font-size:13px;color:#000;padding:8px 12px 4px}
.toc a{display:block;padding:5px 12px;font-size:13px;color:#000;text-decoration:none;border-radius:6px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.toc a:hover{background:#f2f3f5}
.tm{display:inline-block;font-size:10px;font-weight:700;margin-right:6px;width:44px;text-align:center;border-radius:3px;padding:1px 0;color:#fff}
main{flex:1;padding:32px 40px;min-width:0}
.doc-head{margin:0}
h1{font-size:26px;margin:0 0 4px}
.meta{color:#000;font-size:13px;margin-bottom:18px}
.doc-search{position:sticky;top:0;background:#f7f8fa;padding:6px 0 14px;z-index:5}
.doc-search input{width:100%;box-sizing:border-box;padding:9px 12px;border:1px solid #e5e6eb;border-radius:8px;font-size:14px;outline:none;background:#fff}
.doc-search input:focus{border-color:#000;box-shadow:0 0 0 2px rgba(0,0,0,.08)}
h2.dir{font-size:20px;margin:36px 0 8px;padding-bottom:8px;border-bottom:2px solid #e5e6eb}
.api{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:20px 24px;margin:16px 0}
.api h3{margin:0 0 12px;font-size:18px}
.api h4{margin:18px 0 8px;font-size:14px;color:#000}
.urlbar{display:flex;align-items:center;gap:10px;background:#f7f8fa;border-radius:6px;padding:8px 12px}
.urlbar code{font-size:14px;word-break:break-all}
.method,.tm{font-family:inherit}
.method{font-size:12px;font-weight:700;padding:3px 10px;border-radius:4px;color:#fff}
.m-get,.tm-get,.m-post,.tm-post,.m-put,.tm-put{background:#1f2329}
.m-delete,.tm-delete,.m-patch,.tm-patch,.m-head,.tm-head,.m-options,.tm-options{background:#1f2329}
.desc{color:#000;font-size:14px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{border:1px solid #e5e6eb;padding:7px 12px;text-align:left;word-break:break-all}
th{background:#f7f8fa;font-weight:600}
.fname{font-family:Consolas,monospace}
.ftype{color:#000;font-size:12px}
.fex{color:#000}
pre{background:#f7f8fa;color:#000;border:1px solid #e5e6eb;border-radius:8px;padding:14px;font-size:13px;overflow:auto;max-height:400px}
.empty-tip{color:#000;font-size:14px;padding:40px 0;text-align:center;display:none}
/* ---------- 企业文档章节（封面 / 目录 / 前言 / 规范 / 附录） ---------- */
.doc-sec{margin:18px 0}
.sec-title{font-size:20px;margin:26px 0 12px;padding-bottom:8px;border-bottom:2px solid #e5e6eb}
.doc-cover{padding:96px 24px 72px;text-align:center;border-bottom:1px solid #e5e6eb;margin-bottom:18px}
.cover-title{font-size:32px;font-weight:700;margin-bottom:10px;line-height:1.3}
.cover-subtitle{color:#000;font-size:15px;margin-bottom:40px}
.cover-lines{display:inline-block;text-align:left;font-size:14px;line-height:2.1}
.cover-lines span{display:inline-block;width:56px;color:#000}
.doc-table{width:100%;border-collapse:collapse;font-size:13px;margin:10px 0}
.doc-toc{padding:6px 0}
.doc-toc .toc-body{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:8px}
.doc-toc .toc-body a{display:block;padding:4px 10px;font-size:13px;color:#000;text-decoration:none;border-radius:6px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.doc-toc .toc-body a:hover{background:#f2f3f5}
.doc-toc .toc-body .toc-sec{font-weight:600;color:#000}
.mini-h{margin:16px 0 8px}
.mini-p{margin:8px 0;line-height:1.8}
.mini-ul,.mini-ol{margin:8px 0 8px 22px;line-height:1.8}
.mini-quote{margin:10px 0;padding:8px 14px;border-left:3px solid #c9cdd4;background:#f7f8fa;color:#000}
.mini-code{margin:10px 0;background:#f7f8fa;color:#000;border:1px solid #e5e6eb;border-radius:8px;padding:12px;font-size:12.5px;overflow:auto;white-space:pre-wrap;word-break:break-all}
.mini-code-inline{background:#f2f3f5;padding:1px 5px;border-radius:4px;font-family:Consolas,monospace;font-size:12.5px}
.mini-table{width:100%;border-collapse:collapse;font-size:13px;margin:10px 0}@media print{
  @page{size:A4;margin:14mm 12mm}
  body{background:#fff}
  .toc,.doc-search{display:none}
  .layout{display:block;max-width:none}
  main{padding:0}
  h1{font-size:22px}
  h2.dir{break-after:avoid;margin-top:22px}
  .api{border:none;padding:0;margin:14px 0;break-inside:auto}
  .api h3{break-after:avoid}
  table{break-inside:auto}
  tr{break-inside:avoid}
  pre{max-height:none;overflow:visible;white-space:pre-wrap;word-break:break-all;background:#f7f8fa;color:#000;border:1px solid #e5e6eb;-webkit-print-color-adjust:exact;print-color-adjust:exact}
  .doc-cover{break-after:page;padding:28% 12px 0;border-bottom:none;text-align:center}
  .doc-toc{break-after:page}
  .doc-sec.pb{break-before:page}
  .doc-sec{break-inside:auto}
  .sec-title{break-after:avoid}
  .mini-code{max-height:none;overflow:visible}
}
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
	return buildHTML(title, rootID, dirs, apis, common, model.DocWatermark{}, model.DocLayout{})
}

// BuildHTMLWithLayout 生成带企业文档排版（封面 / 目录 / 前言 / 规范 / 附录 / 修订记录）的 HTML。
func BuildHTMLWithLayout(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, layout model.DocLayout) string {
	return buildHTML(title, rootID, dirs, apis, common, model.DocWatermark{}, layout)
}

// BuildHTMLForExport 生成用于 PDF 导出（打印）的 HTML：与分享 HTML 版式一致，
// 但会按水印配置叠加水印层（Chromium 打印时固定定位水印会重复绘制到每一页）。
func BuildHTMLForExport(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, wm model.DocWatermark) string {
	return buildHTML(title, rootID, dirs, apis, common, wm, model.DocLayout{})
}

// BuildHTMLForExportWithLayout 生成 PDF 导出用的打印版 HTML（水印 + 企业文档排版）。
func BuildHTMLForExportWithLayout(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, wm model.DocWatermark, layout model.DocLayout) string {
	return buildHTML(title, rootID, dirs, apis, common, wm, layout)
}

// previewAnchorScript 让弹窗（iframe / srcdoc）里的目录点击一定生效：
// 拦截 #anchor 点击并 scrollIntoView，避免 about:srcdoc 下 fragment 导航失效。
const previewAnchorScript = `<script>(function(){` +
	`document.addEventListener('click',function(e){` +
	`var t=e.target;while(t&&t.tagName!=='A'){t=t.parentElement;}` +
	`if(!t)return;var h=t.getAttribute('href')||'';if(h.charAt(0)!=='#')return;` +
	`var el=h.length>1?document.getElementById(h.slice(1)):null;if(!el)return;` +
	`e.preventDefault();el.scrollIntoView({behavior:'smooth',block:'start'});` +
	`});` +
	`})();</script>`

// withPreviewScript 在 </body> 前注入锚点跳转脚本（仅预览用，导出文件不受影响）。
func withPreviewScript(html string) string {
	if i := strings.LastIndex(html, "</body>"); i >= 0 {
		return html[:i] + previewAnchorScript + html[i:]
	}
	return html + previewAnchorScript
}

// BuildTemplatePreviewHTML 生成「模板结构预览」HTML：只渲染章节结构（封面 / 目录 / 前言 / 规范 / 附录…），
// 接口正文用占位说明代替，便于快速核对模板版式而不夹带任何具体接口。
func BuildTemplatePreviewHTML(title string, common model.CommonParams, wm model.DocWatermark, layout model.DocLayout) string {
	return buildHTMLOpt(title, "", nil, nil, common, wm, layout, true)
}

// buildHTML 渲染自包含的 HTML 文档：按 layout 输出各章节，
// wm 启用时额外叠加水印层（供 PDF 导出使用）。
func buildHTML(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, wm model.DocWatermark, layout model.DocLayout) string {
	return buildHTMLOpt(title, rootID, dirs, apis, common, wm, layout, false)
}

// buildHTMLOpt apisPlaceholder 为 true 时，接口正文渲染为占位说明（模板预览用）。
func buildHTMLOpt(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, wm model.DocWatermark, layout model.DocLayout, apisPlaceholder bool) string {
	now := time.Now()
	sections := buildDocSections(title, layout, now)
	num := buildDocNumbering(sections, dirs, apis, rootID, layout.Numbering)
	multi := len(sections) > 1 // 配置了封面/前言等章节时才输出章节标题

	var body strings.Builder
	withTitle := true
	prevKind := docSectionKind("")
	for _, s := range sections {
		// 封面之后每个章节都从新页开始（PDF/打印与 Word 保持一致）；封面自身用 break-after
		pb := prevKind != "" && prevKind != sectionCover
		prevKind = s.Kind
		switch s.Kind {
		case sectionCover:
			withTitle = false
			body.WriteString(htmlCover(s.Cover))
		case sectionRevisions:
			body.WriteString(htmlRevisions(num.section(s.ID, s.Title), s.Revisions, pb))
		case sectionTOC:
			body.WriteString(htmlTOCSection(sections, dirs, apis, rootID, pb, num))
		case sectionPreface, sectionConvention:
			body.WriteString(htmlChapter(num.section(s.ID, s.Title), s, pb))
		case sectionAPIs:
			body.WriteString(`<section id="` + s.ID + `" class="doc-sec` + pageBreakClass(pb) + `">`)
			if multi || apisPlaceholder {
				body.WriteString(`<h2 class="sec-title">` + htmlText(num.section(s.ID, labelAPISection)) + `</h2>`)
			}
			if apisPlaceholder {
				body.WriteString(`<div class="api"><p class="desc">模板预览不包含具体接口。</p>` +
					`<p class="desc">实际导出时，这里会按所选范围输出每个接口的：请求参数 / 响应参数、请求与响应示例、` +
					`公共请求头与公共参数。</p></div>`)
				break
			}
			if dirs == nil && len(apis) == 1 && rootID == "" {
				htmlApi(&body, apis[0], "", common, num)
			} else {
				htmlDir(&body, dirs, apis, rootID, 0, "", common, num)
			}
			body.WriteString(`</section>`)
		case sectionAppendix:
			body.WriteString(htmlStatusAppendix(s.ID, num.section(s.ID, s.Title), pb))
		case sectionBizCodes, sectionEnums, sectionSupport:
			body.WriteString(htmlChapter(num.section(s.ID, s.Title), s, pb))
		}
	}

	var nav strings.Builder
	htmlDocNav(&nav, sections, dirs, apis, rootID, num)
	tocHTML := ""
	if nav.Len() > 0 {
		tocHTML = `<nav class="toc"><div class="toc-title">目录</div>` + nav.String() + `</nav>`
	}

	head := `<div class="doc-search"><input id="docSearch" type="search" placeholder="搜索目录 / 接口名称 / 接口地址" oninput="apitoolFilter()" autocomplete="off"></div>`
	if withTitle {
		head = fmt.Sprintf(`<div class="doc-head"><h1>%s</h1><div class="meta">共 %d 个接口 · 导出时间 %s · 由 ApiTool 生成</div>%s</div>`,
			html.EscapeString(title), len(apis), now.Format("2006-01-02 15:04"), head)
	}

	css := docCSS
	wmCSS, wmMarkup := watermarkHTMLCSS(wm)
	if wmCSS != "" {
		css += "\n" + wmCSS
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s - 接口文档</title><style>%s</style></head>
<body>%s<div class="layout">%s<main>
%s
%s
<div id="docEmpty" class="empty-tip">没有匹配的接口</div>
</main></div>%s</body></html>`,
		html.EscapeString(title), css, wmMarkup, tocHTML, head, body.String(), docJS)
}

// ---------------- 企业文档章节（HTML） ----------------

// htmlCover 输出封面页（打印时独占一页）。
func htmlCover(c model.DocCover) string {
	var sb strings.Builder
	sb.WriteString(`<section class="doc-cover" id="cover">`)
	sb.WriteString(`<div class="cover-title">` + htmlText(c.Title) + `</div>`)
	if c.Subtitle != "" {
		sb.WriteString(`<div class="cover-subtitle">` + htmlText(c.Subtitle) + `</div>`)
	}
	sb.WriteString(`<div class="cover-lines">`)
	for _, line := range [][2]string{{"作者", c.Author}, {"单位", c.Org}, {"版本", c.Version}, {"日期", c.Date}} {
		if strings.TrimSpace(line[1]) != "" {
			sb.WriteString(`<div><span>` + line[0] + `</span>` + htmlText(line[1]) + `</div>`)
		}
	}
	sb.WriteString(`</div></section>`)
	return sb.String()
}

// htmlRevisions 输出文档修订记录表。
func htmlRevisions(title string, rows []model.DocRevision, pb bool) string {
	var sb strings.Builder
	sb.WriteString(`<section class="doc-sec` + pageBreakClass(pb) + `" id="revisions"><h2 class="sec-title">` + htmlText(title) + `</h2>`)
	sb.WriteString(`<table class="doc-table"><thead><tr><th>版本</th><th>日期</th><th>作者</th><th>修订说明</th></tr></thead><tbody>`)
	for _, r := range rows {
		sb.WriteString(`<tr><td>` + htmlText(r.Version) + `</td><td>` + htmlText(r.Date) + `</td><td>` +
			htmlText(r.Author) + `</td><td>` + htmlText(r.Note) + `</td></tr>`)
	}
	sb.WriteString(`</tbody></table></section>`)
	return sb.String()
}

// htmlChapter 输出自定义章节（公共描述 / 统一定义规范）。
func htmlChapter(title string, s docSection, pb bool) string {
	var sb strings.Builder
	sb.WriteString(`<section class="doc-sec` + pageBreakClass(pb) + `" id="` + s.ID + `">`)
	sb.WriteString(`<h2 class="sec-title">` + htmlText(title) + `</h2>`)
	sb.WriteString(miniHTML(s.Content))
	sb.WriteString(`</section>`)
	return sb.String()
}

// htmlStatusAppendix 输出 HTTP 状态码附录。
func htmlStatusAppendix(id, title string, pb bool) string {
	var sb strings.Builder
	sb.WriteString(`<section class="doc-sec` + pageBreakClass(pb) + `" id="` + id + `"><h2 class="sec-title">` + htmlText(title) + `</h2>`)
	sb.WriteString(`<table class="doc-table"><thead><tr><th>状态码</th><th>名称</th><th>含义</th><th>典型场景</th></tr></thead><tbody>`)
	for _, sc := range commonStatusCodes {
		sb.WriteString(fmt.Sprintf(`<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			sc.Code, htmlText(sc.Name), htmlText(sc.Meaning), htmlText(sc.Usage)))
	}
	sb.WriteString(`</tbody></table></section>`)
	return sb.String()
}

// htmlTOCSection 输出正文中的目录页（PDF 打印时侧边栏被隐藏，靠这一页导航）。
func htmlTOCSection(sections []docSection, dirs []model.Directory, apis []model.ApiInfo, rootID string, pb bool, num docNumbering) string {
	var sb strings.Builder
	sb.WriteString(`<section class="doc-sec doc-toc` + pageBreakClass(pb) + `" id="toc"><h2 class="sec-title">目录</h2><div class="toc-body">`)
	htmlDocNav(&sb, sections, dirs, apis, rootID, num)
	sb.WriteString(`</div></section>`)
	return sb.String()
}

// pageBreakClass 打印时让章节另起一页（屏幕预览不受影响）。
func pageBreakClass(pb bool) string {
	if pb {
		return " pb"
	}
	return ""
}

// htmlDocNav 输出完整目录（章节 + 接口树），供侧边栏与正文目录页共用。
func htmlDocNav(sb *strings.Builder, sections []docSection, dirs []model.Directory, apis []model.ApiInfo, rootID string, num docNumbering) {
	for _, s := range sections {
		switch s.Kind {
		case sectionCover, sectionTOC:
			continue
		case sectionAPIs:
			sb.WriteString(`<a class="toc-sec" href="#` + s.ID + `">` + htmlText(num.section(s.ID, labelAPISection)) + `</a>`)
			htmlToc(sb, dirs, apis, rootID, 0, "", num)
		default:
			sb.WriteString(`<a class="toc-sec" href="#` + s.ID + `">` + htmlText(num.section(s.ID, s.Title)) + `</a>`)
		}
	}
}

package doc

import (
	"fmt"
	"strings"

	"apitool/internal/model"
)

// ReportCSS 测试/压测报告共用的 HTML 内联样式
const ReportCSS = `
:root{color-scheme:light}
*{box-sizing:border-box}body{margin:0;font-family:"Segoe UI","Microsoft YaHei",sans-serif;color:#1f2329;background:#f7f8fa}
.wrap{max-width:1100px;margin:0 auto;padding:32px 28px}
h1{font-size:26px;margin:0 0 6px}
.meta{color:#86909c;font-size:13px;margin-bottom:18px}
.stats{display:flex;gap:14px;flex-wrap:wrap;margin:18px 0}
.stat{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:14px 20px;min-width:120px}
.stat .n{font-size:24px;font-weight:700}
.stat.ok .n{color:#00b42a}.stat.fail .n{color:#f53f3f}.stat.tot .n{color:#165dff}
.card{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:18px 22px;margin:16px 0}
.card h2{margin:0 0 12px;font-size:18px}
.summary{white-space:pre-wrap;line-height:1.8;font-size:14px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{border:1px solid #e5e6eb;padding:8px 12px;text-align:left;word-break:break-all}
th{background:#f7f8fa;font-weight:600}
.pass{color:#00b42a;font-weight:600}.fail{color:#f53f3f;font-weight:600}
.tag{display:inline-block;font-size:11px;padding:1px 8px;border-radius:10px;background:#f2f3f5;color:#4e5969;margin-right:6px}
.assert{font-size:12px;padding:3px 0;border-bottom:1px dashed #eee}
.assert .ok{color:#00b42a}.assert .no{color:#f53f3f}
.block{margin-top:12px;border-top:1px solid #f0f0f0;padding-top:10px}
.block-h{font-size:13px;font-weight:700;color:#1f2329;margin-bottom:6px}
.req-line{font-size:13px;margin-bottom:6px;word-break:break-all}
.req-line .method{display:inline-block;font-weight:700;color:#165dff;margin-right:6px}
.req-line code{background:#f2f3f5;padding:2px 6px;border-radius:4px}
.kv{margin:6px 0}
.kv-h{font-size:12px;font-weight:600;color:#4e5969;margin-bottom:4px}
.kv table{width:100%;border-collapse:collapse;font-size:12px}
.kv th,.kv td{border:1px solid #e5e6eb;padding:5px 8px;text-align:left;word-break:break-all;vertical-align:top}
.kv th{background:#f7f8fa;font-weight:600;width:160px}
.kv pre{margin:0;border:1px solid #e5e6eb;padding:10px;white-space:pre-wrap;word-break:break-all;font-family:Consolas,'Courier New',monospace;font-size:12px;max-height:320px;overflow:auto;background:#fff}
.resp-meta{font-size:12px;color:#4e5969;margin-bottom:6px}
`

// HTMLEscape 转义 HTML 特殊字符（保留中文，不转义为实体名），供其他报告渲染复用。
func HTMLEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

// kvHTML 将启用的键值对渲染为表格（标题为空时仅返回内容）
func kvHTML(title string, rows []model.KV) string {
	rows = EnabledKVs(rows)
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	if title != "" {
		sb.WriteString(`<div class="kv-h">` + HTMLEscape(title) + `</div>`)
	}
	sb.WriteString(`<table><thead><tr><th>名称</th><th>值</th></tr></thead><tbody>`)
	for _, kv := range rows {
		sb.WriteString(`<tr><td>` + HTMLEscape(kv.Key) + `</td><td>` + HTMLEscape(kv.Value) + `</td></tr>`)
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String()
}

// respHeadersHTML 将响应头（map）渲染为表格
func respHeadersHTML(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<div class="kv-h">响应头</div><table><thead><tr><th>名称</th><th>值</th></tr></thead><tbody>`)
	for k, v := range headers {
		sb.WriteString(`<tr><td>` + HTMLEscape(k) + `</td><td>` + HTMLEscape(v) + `</td></tr>`)
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String()
}

// BuildTestReportMarkdown 将测试报告渲染为 Markdown 文本
func BuildTestReportMarkdown(r model.TestReport) string {
	var sb strings.Builder
	sb.WriteString("# 接口测试报告\n\n")
	fmt.Fprintf(&sb, "- 计划：%s\n", r.PlanName)
	if r.EnvName != "" {
		fmt.Fprintf(&sb, "- 运行环境：%s\n", r.EnvName)
	}
	fmt.Fprintf(&sb, "- 生成时间：%s\n", r.CreatedAt)
	fmt.Fprintf(&sb, "- 用例总数：%d，通过：%d，失败：%d\n", r.Total, r.Passed, r.Failed)
	fmt.Fprintf(&sb, "- 总耗时：%d ms\n\n", r.DurationMs)

	if r.Summary != "" {
		sb.WriteString("## AI 分析摘要\n\n")
		sb.WriteString(r.Summary + "\n\n")
	}

	sb.WriteString("## 用例结果\n\n")
	sb.WriteString("| 用例 | 分类 | 状态 | 状态码 | 耗时 | 结果 |\n|---|---|---|---|---|---|\n")
	for _, res := range r.Results {
		status := "失败"
		if res.Passed {
			status = "通过"
		}
		errMsg := res.Error
		if errMsg != "" {
			errMsg = "（" + errMsg + "）"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %d ms | %s |\n",
			MdEscape(res.CaseName), MdEscape(res.Category), status, res.Status, res.DurationMs, MdEscape(errMsg)))
	}

	sb.WriteString("\n## 请求与响应明细\n\n")
	for _, res := range r.Results {
		sb.WriteString(fmt.Sprintf("### %s\n", MdEscape(res.CaseName)))
		if res.Error != "" {
			sb.WriteString("- 请求错误：" + MdEscape(res.Error) + "\n")
		}
		// 请求
		sb.WriteString(fmt.Sprintf("\n**请求：** `%s %s`\n", strings.ToUpper(res.RequestMethod), res.RequestURL))
		if len(EnabledKVs(res.RequestHeaders)) > 0 {
			sb.WriteString("\n请求头：\n")
			for _, kv := range EnabledKVs(res.RequestHeaders) {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", kv.Key, kv.Value))
			}
		}
		if len(EnabledKVs(res.RequestQuery)) > 0 {
			sb.WriteString("\n查询参数：\n")
			for _, kv := range EnabledKVs(res.RequestQuery) {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", kv.Key, kv.Value))
			}
		}
		if res.RequestBodyType == "form" {
			if len(EnabledKVs(res.RequestFormItems)) > 0 {
				sb.WriteString("\n表单字段：\n")
				for _, kv := range EnabledKVs(res.RequestFormItems) {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", kv.Key, kv.Value))
				}
			}
		} else if res.RequestBody != "" {
			sb.WriteString(fmt.Sprintf("\n请求体（%s）：\n\n~~~\n%s\n~~~\n", res.RequestBodyType, res.RequestBody))
		}
		// 响应
		sb.WriteString(fmt.Sprintf("\n**响应：** 状态码 %d %s · 耗时 %d ms · 大小 %d 字节\n", res.Status, res.StatusText, res.DurationMs, res.Size))
		if len(res.ResponseHeaders) > 0 {
			sb.WriteString("\n响应头：\n")
			for k, v := range res.ResponseHeaders {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
			}
		}
		if res.ResponseBody != "" {
			sb.WriteString("\n响应体：\n\n~~~\n" + res.ResponseBody + "\n~~~\n")
		}
		// 断言
		if len(res.AssertionResults) == 0 {
			sb.WriteString("\n- 无断言（按 HTTP 2xx 判定）\n")
		} else {
			sb.WriteString("\n断言：\n")
			for _, ar := range res.AssertionResults {
				mark := "✓"
				if !ar.Passed {
					mark = "✗"
				}
				sb.WriteString(fmt.Sprintf("- %s %s —— %s\n", mark, MdEscape(ar.Description), MdEscape(ar.Detail)))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// BuildTestReportHTML 将测试报告渲染为独立 HTML 源码
func BuildTestReportHTML(r model.TestReport) string {
	var stats strings.Builder
	fmt.Fprintf(&stats, `<div class="stats">
<div class="stat tot"><div class="n">%d</div><div>用例总数</div></div>
<div class="stat ok"><div class="n">%d</div><div>通过</div></div>
<div class="stat fail"><div class="n">%d</div><div>失败</div></div>
<div class="stat"><div class="n">%d ms</div><div>总耗时</div></div></div>`,
		r.Total, r.Passed, r.Failed, r.DurationMs)

	var summary strings.Builder
	if r.Summary != "" {
		summary.WriteString(`<div class="card"><h2>AI 分析摘要</h2><div class="summary">` + HTMLEscape(r.Summary) + `</div></div>`)
	}

	var rows strings.Builder
	for _, res := range r.Results {
		cls := "pass"
		label := "通过"
		if !res.Passed {
			cls = "fail"
			label = "失败"
		}
		errMsg := ""
		if res.Error != "" {
			errMsg = ` <span style="color:#f53f3f">（` + HTMLEscape(res.Error) + `）</span>`
		}
		rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%d</td><td>%d ms</td><td class="%s">%s%s</td></tr>`,
			HTMLEscape(res.CaseName), HTMLEscape(res.Category), res.Status, res.DurationMs, cls, label, errMsg))
	}

	var details strings.Builder
	for _, res := range r.Results {
		details.WriteString(fmt.Sprintf(`<div class="card"><h2>%s</h2>`, HTMLEscape(res.CaseName)))
		if res.Error != "" {
			details.WriteString(`<div class="assert"><span class="no">✗</span> 请求错误：` + HTMLEscape(res.Error) + `</div>`)
		}
		// 请求
		details.WriteString(`<div class="block"><div class="block-h">请求</div>`)
		details.WriteString(fmt.Sprintf(`<div class="req-line"><span class="method">%s</span> <code>%s</code></div>`,
			HTMLEscape(strings.ToUpper(res.RequestMethod)), HTMLEscape(res.RequestURL)))
		details.WriteString(kvHTML("请求头", res.RequestHeaders))
		details.WriteString(kvHTML("查询参数", res.RequestQuery))
		if res.RequestBodyType == "form" {
			details.WriteString(kvHTML("表单字段", res.RequestFormItems))
		} else if res.RequestBody != "" {
			details.WriteString(`<div class="kv"><div class="kv-h">请求体（` + HTMLEscape(res.RequestBodyType) + `）</div><pre>` + HTMLEscape(res.RequestBody) + `</pre></div>`)
		}
		details.WriteString(`</div>`)
		// 响应
		details.WriteString(`<div class="block"><div class="block-h">响应</div>`)
		details.WriteString(fmt.Sprintf(`<div class="resp-meta">状态码：%d %s · 耗时：%d ms · 大小：%d 字节</div>`,
			res.Status, HTMLEscape(res.StatusText), res.DurationMs, res.Size))
		details.WriteString(respHeadersHTML(res.ResponseHeaders))
		if res.ResponseBody != "" {
			details.WriteString(`<div class="kv"><div class="kv-h">响应体</div><pre>` + HTMLEscape(res.ResponseBody) + `</pre></div>`)
		}
		details.WriteString(`</div>`)
		// 断言明细
		if len(res.AssertionResults) == 0 {
			details.WriteString(`<div class="assert">无断言</div>`)
		}
		for _, ar := range res.AssertionResults {
			mark := `<span class="ok">✓</span>`
			if !ar.Passed {
				mark = `<span class="no">✗</span>`
			}
			details.WriteString(fmt.Sprintf(`<div class="assert">%s %s —— %s</div>`,
				mark, HTMLEscape(ar.Description), HTMLEscape(ar.Detail)))
		}
		details.WriteString(`</div>`)
	}

	meta := fmt.Sprintf("计划：%s · 生成时间：%s", HTMLEscape(r.PlanName), HTMLEscape(r.CreatedAt))
	if r.EnvName != "" {
		meta += " · 环境：" + HTMLEscape(r.EnvName)
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>接口测试报告 - %s</title><style>%s</style></head>
<body><div class="wrap">
<h1>接口测试报告</h1>
<div class="meta">%s</div>
%s
%s
<div class="card"><h2>用例结果</h2>
<table><thead><tr><th>用例</th><th>分类</th><th>状态码</th><th>耗时</th><th>结果</th></tr></thead><tbody>%s</tbody></table></div>
%s
</div></body></html>`,
		HTMLEscape(r.PlanName), ReportCSS, meta,
		stats.String(), summary.String(), rows.String(), details.String())
}

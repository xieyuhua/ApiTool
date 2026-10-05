package agent

// 会话导出：把某个会话（含思考过程与工具调用明细）导出为自包含 HTML 或 PDF。
//
// 设计要点：
//  1. 零新增依赖：HTML 由内置轻量 Markdown 渲染器生成（规则与前端 markdown.js 保持一致），
//     所有样式内联，导出的单个 .html 可直接双击打开、可邮件转发。
//  2. PDF 复用本机 Chromium 内核浏览器（Edge / Chrome）的无头打印能力，
//     避免引入体积庞大的前端 PDF 库；未安装时给出明确提示并引导改用 HTML。
//  3. mermaid 图表：HTML 内嵌 CDN 脚本自动渲染，加载失败时降级显示图表源码，
//     因此无网络环境下导出的 PDF 依然可读（图表以源码形式呈现）。

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"regexp"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 行内语法正则（与前端 markdown.js 保持一致）
var (
	codeInlineRe = regexp.MustCompile("`([^`]+)`")
	boldRe       = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRe     = regexp.MustCompile(`(^|[^*])\*([^*]+)\*`)
	linkRe       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

// sessionCSS 会话导出文档的内联样式：屏幕阅读与 A4 打印两套规则。
const sessionCSS = `
:root{color-scheme:light}
*{box-sizing:border-box}
body{margin:0;background:#f5f6f8;color:#1f2329;
  font-family:"Segoe UI","Microsoft YaHei",-apple-system,sans-serif;font-size:14px;line-height:1.7}
.wrap{max-width:900px;margin:0 auto;padding:28px 24px 40px}
.doc-head h1{font-size:24px;margin:0 0 6px;line-height:1.4}
.doc-head .meta{color:#86909c;font-size:12px}
.stats{display:flex;gap:12px;flex-wrap:wrap;margin:16px 0 22px}
.stat{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:10px 18px;min-width:110px}
.stat .n{font-size:20px;font-weight:700;color:#165dff}
.stat .l{font-size:12px;color:#86909c}
.msg{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:14px 18px;margin:0 0 16px}
.msg.user{background:#f0f6ff;border-color:#cfe0ff}
.msg-head{display:flex;align-items:center;gap:8px;margin-bottom:8px}
.who{font-size:13px;font-weight:700}
.who.user{color:#165dff}
.who.ai{color:#722ed1}
.msg-head .time{margin-left:auto;font-size:11px;color:#86909c}
details{margin:8px 0}
summary{cursor:pointer;font-size:12px;color:#722ed1;font-weight:600;outline:none}
summary .badge{font-weight:400}
pre{margin:6px 0 0;padding:10px 12px;background:#f7f8fa;border:1px solid #e5e6eb;border-radius:6px;
  font-family:Consolas,"Courier New",monospace;font-size:12px;white-space:pre-wrap;word-break:break-word;
  max-height:320px;overflow:auto}
pre.err{color:#f53f3f;border-color:#ffccc7;background:#fff1f0}
.step{border:1px solid #e5e6eb;border-radius:8px;padding:4px 10px;background:#fbfcfd}
.step-name{font-size:12px;font-weight:600;color:#1f2329}
.step-name .srv{font-weight:400;color:#86909c;margin-left:4px}
.badge{font-size:11px;padding:0 8px;border-radius:10px;margin-left:6px;font-weight:400}
.badge.ok{color:#00b42a;background:#e8ffea}
.badge.err{color:#f53f3f;background:#ffece8}
.badge.skill{color:#722ed1;background:#f5eef8}
.sec-h{font-size:11px;color:#165dff;font-weight:600;margin-top:6px}
/* markdown 正文 */
.md-p{margin:6px 0}
.md-h{margin:12px 0 6px;font-weight:600;line-height:1.4}
.md-h1{font-size:19px}.md-h2{font-size:17px}.md-h3{font-size:15px}.md-h4{font-size:14px}
.md-ul,.md-ol{padding-left:22px;margin:6px 0}
.md-table{border-collapse:collapse;width:100%;margin:8px 0;font-size:13px;display:block;overflow-x:auto}
.md-table th,.md-table td{border:1px solid #e5e6eb;padding:6px 10px;text-align:left;vertical-align:top}
.md-table th{background:#f7f8fa;font-weight:600}
.md-quote{border-left:3px solid #c9cdd4;padding-left:12px;color:#4e5969;margin:8px 0}
.md-hr{border:none;border-top:1px solid #e5e6eb;margin:12px 0}
.md-code-inline{background:#f2f3f5;padding:1px 5px;border-radius:4px;font-family:Consolas,monospace;font-size:13px}
.md a{color:#165dff}
.md-mermaid{text-align:center;margin:10px 0;overflow-x:auto}
.md-mermaid svg{max-width:100%;height:auto}
.md-mermaid-tip{font-size:12px;color:#86909c}
.doc-foot{color:#86909c;font-size:12px;text-align:center;margin-top:24px;border-top:1px solid #e5e6eb;padding-top:12px}
@media print{
  @page{size:A4;margin:14mm}
  body{background:#fff;font-size:12px}
  .wrap{max-width:none;padding:0}
  .msg,.stat{break-inside:avoid;page-break-inside:avoid;box-shadow:none}
  pre{max-height:none;overflow:visible}
  .md-table{display:table}
}
`

// sessionMermaidScript mermaid 渲染脚本：成功则渲染为 SVG，失败（无网络/CDN 不可达）降级显示源码。
const sessionMermaidScript = `<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
<script>
(function(){
  function fallback(msg){
    document.querySelectorAll('.md-mermaid').forEach(function(el){
      var src = el.textContent;
      el.innerHTML = '<pre><code>' + src.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;') +
        '</code></pre><div class="md-mermaid-tip">' + msg + '</div>';
    });
  }
  if (!window.mermaid) { fallback('（图表渲染需要网络，已显示图表源码）'); return; }
  try { window.mermaid.initialize({ startOnLoad: true, theme: 'default' }); }
  catch (e) { fallback('（图表渲染失败，已显示图表源码）'); }
})();
</script>`

// ExportMeta 会话导出文档头部的附加信息（模型、模式、用户等）。
type ExportMeta struct {
	Title      string
	Model      string
	Mode       string
	User       string
	ExportedAt string
}

// BuildSessionHTML 生成指定会话（sessionID 为空时取当前会话）的 HTML 文档文本，不落盘。
// 供前端预览/调试使用，导出请调用 ExportAgentSession。
func (m *Manager) BuildSessionHTML(sessionID string) (string, error) {
	s, meta, err := m.sessionExportContext(sessionID)
	if err != nil {
		return "", err
	}
	return buildSessionHTML(s, meta), nil
}

// ExportAgentSession 把会话导出为文件（format: html / pdf），返回保存路径。
// 用户取消保存时返回空路径与 nil 错误。
func (m *Manager) ExportAgentSession(sessionID, format string) (string, error) {
	s, meta, err := m.sessionExportContext(sessionID)
	if err != nil {
		return "", err
	}
	html := buildSessionHTML(s, meta)
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "html"
	}
	var ext, filter, title string
	switch format {
	case "html":
		ext, filter, title = ".html", "HTML 网页 (*.html)|*.html", "导出会话记录（HTML）"
	case "pdf":
		ext, filter, title = ".pdf", "PDF 文件 (*.pdf)|*.pdf", "导出会话记录（PDF）"
	default:
		return "", fmt.Errorf("不支持的导出格式: %s（可选 html / pdf）", format)
	}
	path, err := m.b.SaveFileDialog(wruntime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: sanitizeExportName(s.Title) + "-" + time.Now().Format("20060102-150405") + ext,
		Filters:         []wruntime.FileFilter{{DisplayName: filter, Pattern: "*" + ext}},
	})
	if err != nil || path == "" {
		return "", err
	}
	// 补全/修正扩展名：用户可能在对话框里改掉了后缀
	if ext2 := strings.ToLower(filepath.Ext(path)); ext2 != ext {
		if ext2 == "" {
			path += ext
		} else {
			path = strings.TrimSuffix(path, filepath.Ext(path)) + ext
		}
	}
	if ext == ".html" {
		return path, os.WriteFile(path, []byte(html), 0o644)
	}
	return path, htmlToPDF(html, path)
}

// sessionExportContext 读取待导出的会话与其元信息。
func (m *Manager) sessionExportContext(sessionID string) (AgentSession, ExportMeta, error) {
	d := m.readAgentData()
	var s *AgentSession
	if sessionID == "" {
		s = d.activeSession()
	} else {
		for i := range d.Sessions {
			if d.Sessions[i].ID == sessionID {
				s = &d.Sessions[i]
				break
			}
		}
	}
	if s == nil {
		return AgentSession{}, ExportMeta{}, fmt.Errorf("会话不存在")
	}
	meta := ExportMeta{
		Title:      s.Title,
		Mode:       d.Config.Mode,
		ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if meta.Title == "" {
		meta.Title = "未命名会话"
	}
	if model := strings.TrimSpace(m.host.ReadData().Settings.AIModel); model != "" {
		meta.Model = model
	}
	for _, u := range d.Users {
		if u.ID == d.Config.CurrentUserID {
			meta.User = u.Name
			break
		}
	}
	return *s, meta, nil
}

// buildSessionHTML 组装完整、自包含的 HTML 文档。
func buildSessionHTML(sess AgentSession, meta ExportMeta) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>`)
	sb.WriteString(esc(meta.Title))
	sb.WriteString(` · AI Agent 会话记录</title>
<style>`)
	sb.WriteString(sessionCSS)
	sb.WriteString(`</style></head><body><div class="wrap">
<div class="doc-head"><h1>`)
	sb.WriteString(esc(meta.Title))
	sb.WriteString(`</h1><div class="meta">`)
	sb.WriteString(sessionMetaLine(sess, meta))
	sb.WriteString(`</div></div>
<div class="stats">`)
	sb.WriteString(statCard(strconv.Itoa(len(sess.Messages)), "消息数"))
	sb.WriteString(statCard(strconv.FormatInt(sess.Usage.TotalTokens, 10), "Token 总计"))
	sb.WriteString(statCard(strconv.FormatInt(sess.Usage.PromptTokens, 10), "输入 Token"))
	sb.WriteString(statCard(strconv.FormatInt(sess.Usage.CompletionTokens, 10), "输出 Token"))
	sb.WriteString(`</div>`)

	for _, msg := range sess.Messages {
		sb.WriteString(messageHTML(msg))
	}
	sb.WriteString(`<div class="doc-foot">由 ApiTool AI Agent 导出 · 导出时间 `)
	sb.WriteString(esc(meta.ExportedAt))
	sb.WriteString(`</div>
</div>`)
	sb.WriteString(sessionMermaidScript)
	sb.WriteString(`</body></html>`)
	return sb.String()
}

// sessionMetaLine 生成头部元信息行。
func sessionMetaLine(sess AgentSession, meta ExportMeta) string {
	var parts []string
	if c := fmtTime(sess.CreatedAt); c != "" {
		parts = append(parts, "创建于 "+c)
	}
	if u := fmtTime(sess.UpdatedAt); u != "" {
		parts = append(parts, "更新于 "+u)
	}
	if meta.Model != "" {
		parts = append(parts, "模型 "+meta.Model)
	}
	if meta.Mode != "" {
		mode := meta.Mode
		if mode == "plan" {
			mode = "Plan 模式"
		} else if mode == "react" {
			mode = "ReAct 模式"
		}
		parts = append(parts, mode)
	}
	if meta.User != "" {
		parts = append(parts, "用户 "+meta.User)
	}
	return esc(strings.Join(parts, " · "))
}

func statCard(n, label string) string {
	return `<div class="stat"><div class="n">` + esc(n) + `</div><div class="l">` + esc(label) + `</div></div>`
}

// messageHTML 渲染单条消息：思考过程 + 工具调用明细 + 正文。
func messageHTML(msg AgentMsg) string {
	isUser := msg.Role == "user"
	who, avatar := "AI 助手", "ai"
	if isUser {
		who, avatar = "用户", "user"
	} else if msg.Role == "system" {
		who = "系统"
	}
	var sb strings.Builder
	sb.WriteString(`<section class="msg `)
	sb.WriteString(avatar)
	sb.WriteString(`"><div class="msg-head"><span class="who `)
	sb.WriteString(avatar)
	sb.WriteString(`">`)
	sb.WriteString(esc(who))
	sb.WriteString(`</span><span class="time">`)
	sb.WriteString(esc(msg.Time))
	sb.WriteString(`</span></div>`)
	if !isUser {
		sb.WriteString(thinkingHTML(msg.Thinking, msg.Steps))
	}
	sb.WriteString(`<div class="md">`)
	sb.WriteString(renderMD(msg.Content))
	sb.WriteString(`</div></section>`)
	return sb.String()
}

// thinkingHTML 渲染思考过程与工具/skill 调用明细（默认展开，保证 PDF 打印不丢内容）。
func thinkingHTML(thinking string, steps []AgentStep) string {
	var sb strings.Builder
	tools := 0
	for _, s := range steps {
		if s.Type != "thought" {
			tools++
		}
	}
	if thinking == "" && tools == 0 {
		return ""
	}
	sb.WriteString(`<details class="think" open><summary>💭 思考过程`)
	sb.WriteString(toolCountText(tools))
	sb.WriteString(`</summary>`)
	if thinking != "" {
		sb.WriteString(`<pre class="thinking">`)
		sb.WriteString(esc(thinking))
		sb.WriteString(`</pre>`)
	}
	for _, s := range steps {
		if s.Type == "thought" {
			continue
		}
		sb.WriteString(stepHTML(s))
	}
	sb.WriteString(`</details>`)
	return sb.String()
}

func toolCountText(n int) string {
	if n == 0 {
		return ""
	}
	return `　·　` + strconv.Itoa(n) + ` 个工具调用`
}

// stepHTML 渲染单个工具/skill 调用卡片。
func stepHTML(s AgentStep) string {
	icon := map[string]string{"tool": "🔧", "skill": "✨", "plan": "📋"}[s.Type]
	if icon == "" {
		icon = "🔧"
	}
	failed := s.Error != "" || s.Type == "tool-failed"
	badge, cls := "成功", "ok"
	switch {
	case failed:
		badge, cls = "失败", "err"
	case s.Type == "skill":
		badge, cls = "技能", "skill"
	case s.Type == "plan":
		badge, cls = "计划", "ok"
	}
	var sb strings.Builder
	sb.WriteString(`<details class="step" open><summary><span class="step-name">`)
	sb.WriteString(icon)
	sb.WriteString(` `)
	sb.WriteString(esc(s.Name))
	if s.Server != "" {
		sb.WriteString(`<span class="srv">@`)
		sb.WriteString(esc(s.Server))
		sb.WriteString(`</span>`)
	}
	sb.WriteString(`<span class="badge `)
	sb.WriteString(cls)
	sb.WriteString(`">`)
	sb.WriteString(badge)
	sb.WriteString(`</span></span></summary>`)
	if s.Input != "" {
		sb.WriteString(`<div class="sec-h">执行参数</div><pre>`)
		sb.WriteString(esc(s.Input))
		sb.WriteString(`</pre>`)
	}
	if s.Error != "" {
		sb.WriteString(`<div class="sec-h" style="color:#f53f3f">错误信息</div><pre class="err">`)
		sb.WriteString(esc(s.Error))
		sb.WriteString(`</pre>`)
	} else if s.Output != "" {
		sb.WriteString(`<div class="sec-h">返回结果</div><pre>`)
		sb.WriteString(esc(s.Output))
		sb.WriteString(`</pre>`)
	}
	sb.WriteString(`</details>`)
	return sb.String()
}

// ============================ Markdown 渲染 ============================

// renderMD 轻量 Markdown → HTML（规则与前端 markdown.js 对齐）：
// 标题、粗体/斜体/行内代码、围栏代码块（mermaid 单独处理）、有序/无序列表、引用、表格、链接、水平线。
func renderMD(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	lines := strings.ReplaceAll(md, "\r\n", "\n")
	all := strings.Split(lines, "\n")
	out := make([]string, 0, len(all))
	i := 0
	for i < len(all) {
		line := all[i]

		// 围栏代码块
		if lang, ok := fenceLang(line); ok {
			buf := []string{}
			i++
			for i < len(all) && !isFenceEnd(all[i]) {
				buf = append(buf, all[i])
				i++
			}
			i++ // 跳过结束的 ```
			code := strings.Join(buf, "\n")
			if lang == "mermaid" {
				out = append(out, `<div class="md-mermaid">`+esc(code)+`</div>`)
			} else {
				out = append(out, `<pre><code class="lang-`+esc(lang)+`">`+esc(code)+`</code></pre>`)
			}
			continue
		}

		// 表格：当前行含 | 且下一行是分隔行
		if strings.Contains(line, "|") && i+1 < len(all) && isTableSep(all[i+1]) {
			header := splitRow(line)
			i += 2
			rows := [][]string{}
			for i < len(all) && strings.Contains(all[i], "|") && strings.TrimSpace(all[i]) != "" {
				rows = append(rows, splitRow(all[i]))
				i++
			}
			var sb strings.Builder
			sb.WriteString(`<table class="md-table"><thead><tr>`)
			for _, h := range header {
				sb.WriteString(`<th>`)
				sb.WriteString(inlineMD(h))
				sb.WriteString(`</th>`)
			}
			sb.WriteString(`</tr></thead><tbody>`)
			for _, r := range rows {
				sb.WriteString(`<tr>`)
				for idx := range header {
					cell := ""
					if idx < len(r) {
						cell = r[idx]
					}
					sb.WriteString(`<td>`)
					sb.WriteString(inlineMD(cell))
					sb.WriteString(`</td>`)
				}
				sb.WriteString(`</tr>`)
			}
			sb.WriteString(`</tbody></table>`)
			out = append(out, sb.String())
			continue
		}

		// 标题
		if lv, text, ok := headingOf(line); ok {
			out = append(out, `<h`+strconv.Itoa(lv)+` class="md-h md-h`+strconv.Itoa(lv)+`">`+inlineMD(text)+`</h`+strconv.Itoa(lv)+`>`)
			i++
			continue
		}

		// 水平线
		if isHR(line) {
			out = append(out, `<hr class="md-hr" />`)
			i++
			continue
		}

		// 引用
		if isQuote(line) {
			buf := []string{}
			for i < len(all) && isQuote(all[i]) {
				buf = append(buf, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(all[i]), ">")))
				i++
			}
			out = append(out, `<blockquote class="md-quote">`+nl2br(inlineMD(strings.Join(buf, "\n")))+`</blockquote>`)
			continue
		}

		// 无序列表
		if isULItem(line) {
			buf := []string{}
			for i < len(all) && isULItem(all[i]) {
				buf = append(buf, `<li>`+inlineMD(trimListMark(all[i], false))+`</li>`)
				i++
			}
			out = append(out, `<ul class="md-ul">`+strings.Join(buf, "")+`</ul>`)
			continue
		}

		// 有序列表
		if isOLItem(line) {
			buf := []string{}
			for i < len(all) && isOLItem(all[i]) {
				buf = append(buf, `<li>`+inlineMD(trimListMark(all[i], true))+`</li>`)
				i++
			}
			out = append(out, `<ol class="md-ol">`+strings.Join(buf, "")+`</ol>`)
			continue
		}

		// 空行
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		// 段落：合并后续非空、非块级起始的行
		buf := []string{line}
		i++
		for i < len(all) && strings.TrimSpace(all[i]) != "" && !isBlockStartAt(all, i) {
			buf = append(buf, all[i])
			i++
		}
		out = append(out, `<p class="md-p">`+nl2br(inlineMD(strings.Join(buf, "\n")))+`</p>`)
	}
	return strings.Join(out, "\n")
}

func fenceLang(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(t, "```"))
	if strings.Contains(rest, " ") || strings.Contains(rest, "{") {
		return "", false
	}
	return strings.ToLower(rest), true
}

func isFenceEnd(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") && strings.TrimSpace(strings.TrimPrefix(t, "```")) == ""
}

func isTableSep(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "-") {
		return false
	}
	for _, r := range t {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return true
}

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	// 去掉首尾空单元（对齐写法 | a | b | 产生的空串）
	for len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

func headingOf(line string) (int, string, bool) {
	t := strings.TrimLeft(line, " ")
	lv := 0
	for lv < len(t) && t[lv] == '#' {
		lv++
	}
	if lv == 0 || lv > 6 || lv >= len(t) || t[lv] != ' ' {
		return 0, "", false
	}
	return lv, strings.TrimSpace(t[lv+1:]), true
}

func isHR(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != c && t[i] != ' ' {
			return false
		}
	}
	return true
}

func isQuote(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), ">")
}

func isULItem(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) > 1 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && t[1] == ' '
}

func isOLItem(line string) bool {
	t := strings.TrimSpace(line)
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(t) && t[i] == '.' && t[i+1] == ' '
}

func trimListMark(line string, ordered bool) string {
	t := strings.TrimSpace(line)
	if ordered {
		if i := strings.Index(t, ". "); i > 0 {
			return t[i+2:]
		}
		return t
	}
	return strings.TrimSpace(t[1:])
}

// isBlockStartAt 判断第 idx 行是否为块级语法起始（用于段落合并的终止条件）。
// 表格需结合下一行（分隔行）判断。
func isBlockStartAt(all []string, idx int) bool {
	line := all[idx]
	if _, ok := fenceLang(line); ok {
		return true
	}
	if _, _, ok := headingOf(line); ok {
		return true
	}
	if isHR(line) || isQuote(line) || isULItem(line) || isOLItem(line) {
		return true
	}
	return strings.Contains(line, "|") && idx+1 < len(all) && isTableSep(all[idx+1])
}

func nl2br(s string) string {
	return strings.ReplaceAll(s, "\n", "<br/>")
}

// inlineMD 行内语法：先转义，再依次处理行内代码、粗体、斜体、链接。
func inlineMD(text string) string {
	t := esc(text)
	// 行内代码
	t = codeInlineRe.ReplaceAllString(t, `<code class="md-code-inline">$1</code>`)
	// 粗体
	t = boldRe.ReplaceAllString(t, `<strong>$1</strong>`)
	// 斜体
	t = italicRe.ReplaceAllString(t, `$1<em>$2</em>`)
	// 链接
	t = linkRe.ReplaceAllString(t, `<a href="$2" target="_blank" rel="noopener">$1</a>`)
	return t
}

// ============================ PDF 转换 ============================

// htmlToPDF 用本机 Chromium 内核浏览器的无头打印把 HTML 文本转换为 PDF 文件。
func htmlToPDF(html, pdfPath string) error {
	browser, err := findChromium()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "apitool-agent-export")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "session.html")
	if err := os.WriteFile(src, []byte(html), 0o644); err != nil {
		return err
	}
	if err := printToPDF(browser, src, pdfPath, true); err != nil {
		// 部分旧版本浏览器不认 --no-pdf-header-footer，去掉后再试一次
		if err2 := printToPDF(browser, src, pdfPath, false); err2 != nil {
			return err
		}
	}
	return nil
}

// printToPDF 执行一次无头打印。noHeader 控制是否隐藏浏览器默认页眉页脚。
func printToPDF(browser, src, pdfPath string, noHeader bool) error {
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-extensions",
		"--hide-scrollbars",
		"--run-all-compositor-stages-before-draw",
		// 给 mermaid CDN 脚本留出加载与渲染时间（虚拟时间会加速推进）
		"--virtual-time-budget=15000",
	}
	if noHeader {
		args = append(args, "--no-pdf-header-footer")
	}
	args = append(args, "--print-to-pdf="+pdfPath, fileURL(src))
	cmd := exec.Command(browser, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("调用浏览器生成 PDF 失败: %s", tailText(string(out), 300))
	}
	if fi, statErr := os.Stat(pdfPath); statErr != nil || fi.Size() == 0 {
		return fmt.Errorf("浏览器未生成 PDF 文件: %s", tailText(string(out), 300))
	}
	return nil
}

// findChromium 查找本机可用的 Chromium 内核浏览器（Edge / Chrome / Chromium）。
func findChromium() (string, error) {
	var candidates []string
	switch goruntime.GOOS {
	case "windows":
		paths := []struct{ env, rel string }{
			{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
			{"LOCALAPPDATA", `Microsoft\Edge\Application\msedge.exe`},
			{"LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`},
		}
		for _, p := range paths {
			if base := os.Getenv(p.env); base != "" {
				candidates = append(candidates, filepath.Join(base, p.rel))
			}
		}
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
	default:
		for _, name := range []string{"microsoft-edge", "microsoft-edge-stable", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if p, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, p)
			}
		}
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	// 兜底：PATH 中的 chrome / msedge
	for _, name := range []string{"chrome", "msedge", "chromium", "google-chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 Chrome / Edge 浏览器，无法直接生成 PDF；请改用「导出 HTML」，再在浏览器中打印为 PDF")
}

// fileURL 把本地路径转为 file:// URL（Windows 下盘符需转为 /C:/ 形式）。
func fileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

// ============================ 通用小工具 ============================

// esc 转义 HTML 特殊字符。
func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// sanitizeExportName 清理文件名中的非法字符。
func sanitizeExportName(s string) string {
	repl := strings.NewReplacer(`\`, "_", "/", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")
	out := strings.TrimSpace(repl.Replace(s))
	if out == "" {
		return "agent-session"
	}
	return out
}

// fmtTime 把 RFC3339 时间格式化为 "2006-01-02 15:04:05"，解析失败时返回空串。
func fmtTime(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04:05")
}

// tailText 截取文本末尾 n 个字符（用于浏览器错误输出摘要），按 rune 截断避免乱码。
func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "..." + string(r[len(r)-n:])
}

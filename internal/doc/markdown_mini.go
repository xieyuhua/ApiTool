package doc

// 简化 Markdown 渲染：用于「文档公共描述」「接口统一定义规范」这类由用户编写的前言正文。
//
// 只支持企业文档常用的子集：段落、标题（#/##/###/####）、无序/有序列表、引用、围栏代码块、
// 表格，以及行内 `code` 与 **加粗**。先解析成块，再由 HTML（PDF）与 DOCX 两个渲染器各自输出，
// 保证同段文字在不同格式下结构一致。

import (
	"fmt"
	"strings"
)

// miniBlockKind 块类型。
type miniBlockKind int

const (
	miniParagraph miniBlockKind = iota
	miniHeading
	miniBulletList
	miniOrderedList
	miniQuote
	miniCode
	miniTable
)

// miniBlock 解析后的一个块。
type miniBlock struct {
	Kind   miniBlockKind
	Level  int      // 标题级别（1-4）
	Text   string   // 段落 / 引用 / 代码内容
	Items  []string // 列表项
	Header []string // 表格表头
	Rows   [][]string
}

// miniInline 行内片段（加粗 / 代码）。
type miniInline struct {
	Text string
	Bold bool
	Code bool
}

// parseMiniMarkdown 解析简化 Markdown。
func parseMiniMarkdown(src string) []miniBlock {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var blocks []miniBlock
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		blocks = append(blocks, miniBlock{Kind: miniParagraph, Text: strings.Join(para, "\n")})
		para = nil
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)

		// 围栏代码块
		if strings.HasPrefix(trimmed, "```") {
			flushPara()
			var code []string
			for i++; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					break
				}
				code = append(code, lines[i])
			}
			blocks = append(blocks, miniBlock{Kind: miniCode, Text: strings.Join(code, "\n")})
			continue
		}
		// 空行
		if trimmed == "" {
			flushPara()
			continue
		}
		// 标题
		if level, text, ok := miniHeadingOf(trimmed); ok {
			flushPara()
			blocks = append(blocks, miniBlock{Kind: miniHeading, Level: level, Text: text})
			continue
		}
		// 表格（当前行是表头，下一行是分隔行）
		if strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && isMiniTableSep(lines[i+1]) {
			flushPara()
			header := miniSplitRow(trimmed)
			rows := [][]string{}
			for i += 2; i < len(lines); i++ {
				row := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(row, "|") {
					i--
					break
				}
				rows = append(rows, miniSplitRow(row))
			}
			blocks = append(blocks, miniBlock{Kind: miniTable, Header: header, Rows: rows})
			continue
		}
		// 引用
		if strings.HasPrefix(trimmed, "> ") {
			flushPara()
			blocks = append(blocks, miniBlock{Kind: miniQuote, Text: strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))})
			continue
		}
		// 无序列表
		if isMiniBullet(trimmed) {
			items := miniCollectList(lines, &i, false)
			flushPara()
			blocks = append(blocks, miniBlock{Kind: miniBulletList, Items: items})
			continue
		}
		// 有序列表
		if isMiniOrdered(trimmed) {
			items := miniCollectList(lines, &i, true)
			flushPara()
			blocks = append(blocks, miniBlock{Kind: miniOrderedList, Items: items})
			continue
		}
		para = append(para, trimmed)
	}
	flushPara()
	return blocks
}

// miniHeadingOf 识别标题行。
func miniHeadingOf(line string) (level int, text string, ok bool) {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 4 || n >= len(line) || line[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(line[n+1:]), true
}

// isMiniBullet 判断是否无序列表项。
func isMiniBullet(line string) bool {
	return strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ")
}

// isMiniOrdered 判断是否有序列表项（1. xxx）。
func isMiniOrdered(line string) bool {
	dot := strings.Index(line, ". ")
	if dot <= 0 {
		return false
	}
	for _, r := range line[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// miniCollectList 收集连续的列表项（含缩进续行），并把 i 推进到列表之后。
func miniCollectList(lines []string, idx *int, ordered bool) []string {
	var items []string
	for ; *idx < len(lines); *idx++ {
		line := strings.TrimSpace(lines[*idx])
		if line == "" {
			break
		}
		if ordered && isMiniOrdered(line) {
			items = append(items, strings.TrimSpace(line[strings.Index(line, ". ")+2:]))
			continue
		}
		if !ordered && isMiniBullet(line) {
			items = append(items, strings.TrimSpace(line[2:]))
			continue
		}
		// 续行：并入上一项
		if len(items) > 0 && !strings.HasPrefix(line, "|") {
			items[len(items)-1] += " " + line
			continue
		}
		break
	}
	*idx--
	return items
}

// isMiniTableSep 判断表格分隔行（|---|---|）。
func isMiniTableSep(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return false
	}
	for _, r := range strings.Trim(line, "|") {
		if r != '-' && r != '|' && r != ':' && r != ' ' && r != '\t' {
			return false
		}
	}
	return strings.Contains(line, "-")
}

// miniSplitRow 拆分表格行。
func miniSplitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// parseMiniInline 解析行内 `code` 与 **加粗**（支持两者嵌套的常见写法）。
func parseMiniInline(text string) []miniInline {
	var out []miniInline
	for len(text) > 0 {
		idx := strings.Index(text, "`")
		boldIdx := strings.Index(text, "**")
		switch {
		case idx >= 0 && (boldIdx < 0 || idx < boldIdx):
			if idx > 0 {
				out = append(out, miniInline{Text: text[:idx]})
			}
			rest := text[idx+1:]
			end := strings.Index(rest, "`")
			if end < 0 {
				out = append(out, miniInline{Text: text[idx:]})
				return out
			}
			out = append(out, miniInline{Text: rest[:end], Code: true})
			text = rest[end+1:]
		case boldIdx >= 0:
			if boldIdx > 0 {
				out = append(out, miniInline{Text: text[:boldIdx]})
			}
			rest := text[boldIdx+2:]
			end := strings.Index(rest, "**")
			if end < 0 {
				out = append(out, miniInline{Text: text[boldIdx:]})
				return out
			}
			out = append(out, miniInline{Text: rest[:end], Bold: true})
			text = rest[end+2:]
		default:
			out = append(out, miniInline{Text: text})
			return out
		}
	}
	return out
}

// ---------------- HTML 渲染 ----------------

// miniHTML 把简化 Markdown 渲染为 HTML（正文内嵌到文档章节里）。
func miniHTML(src string) string {
	var sb strings.Builder
	for _, b := range parseMiniMarkdown(src) {
		switch b.Kind {
		case miniHeading:
			lv := b.Level + 2 // 章节标题是 h2，正文标题从 h4 起
			if lv > 6 {
				lv = 6
			}
			fmt.Fprintf(&sb, `<h%d class="mini-h">%s</h%d>`, lv, miniInlineHTML(b.Text), lv)
		case miniBulletList:
			sb.WriteString(`<ul class="mini-ul">`)
			for _, it := range b.Items {
				sb.WriteString(`<li>` + miniInlineHTML(it) + `</li>`)
			}
			sb.WriteString(`</ul>`)
		case miniOrderedList:
			sb.WriteString(`<ol class="mini-ol">`)
			for _, it := range b.Items {
				sb.WriteString(`<li>` + miniInlineHTML(it) + `</li>`)
			}
			sb.WriteString(`</ol>`)
		case miniQuote:
			sb.WriteString(`<blockquote class="mini-quote">` + miniInlineHTML(b.Text) + `</blockquote>`)
		case miniCode:
			sb.WriteString(`<pre class="mini-code">` + htmlText(b.Text) + `</pre>`)
		case miniTable:
			sb.WriteString(`<table class="mini-table"><thead><tr>`)
			for _, h := range b.Header {
				sb.WriteString(`<th>` + miniInlineHTML(h) + `</th>`)
			}
			sb.WriteString(`</tr></thead><tbody>`)
			for _, row := range b.Rows {
				sb.WriteString(`<tr>`)
				for _, cell := range row {
					sb.WriteString(`<td>` + miniInlineHTML(cell) + `</td>`)
				}
				sb.WriteString(`</tr>`)
			}
			sb.WriteString(`</tbody></table>`)
		default:
			for _, line := range strings.Split(b.Text, "\n") {
				sb.WriteString(`<p class="mini-p">` + miniInlineHTML(line) + `</p>`)
			}
		}
	}
	return sb.String()
}

// miniInlineHTML 渲染行内片段。
func miniInlineHTML(text string) string {
	var sb strings.Builder
	for _, r := range parseMiniInline(text) {
		switch {
		case r.Code:
			sb.WriteString(`<code class="mini-code-inline">` + htmlText(r.Text) + `</code>`)
		case r.Bold:
			sb.WriteString(`<strong>` + htmlText(r.Text) + `</strong>`)
		default:
			sb.WriteString(htmlText(r.Text))
		}
	}
	return sb.String()
}

// miniPlain 去掉行内标记，得到纯文本（用于 Markdown / 目录标题等场景）。
func miniPlain(text string) string {
	var sb strings.Builder
	for _, r := range parseMiniInline(text) {
		sb.WriteString(r.Text)
	}
	return sb.String()
}

// miniMarkdownToText 把简化 Markdown 原样输出为 Markdown 正文，仅把标题层级下移一级，
// 使其嵌套在所属章节（## 标题）之下。
func miniMarkdownToText(src string) string {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if level, text, ok := miniHeadingOf(strings.TrimSpace(line)); ok {
			lv := level + 1
			if lv > 6 {
				lv = 6
			}
			lines[i] = strings.Repeat("#", lv) + " " + text
		}
	}
	return strings.Join(lines, "\n")
}

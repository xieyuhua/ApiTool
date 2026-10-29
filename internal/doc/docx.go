package doc

// .docx 正文渲染：直接把接口文档写成 OOXML 片段（不引入第三方 docx 库）。
//   - 标题 / 目录层级 / 表格 / 代码块均为 Word 原生排版元素，可在 Word、WPS 中编辑；
//   - 包结构装配见 docx_parts.go；页眉水印见 watermark_vml.go。
//
// 版式：A4 纵向，左右页边距 2cm、上下 2.5cm，正文可用宽度 9638 twips。

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"apitool/internal/model"
)

const (
	docxPageWidth  = 11906 // A4 宽（twips）
	docxPageHeight = 16838 // A4 高（twips）
	docxMarginX    = 1134  // 左右页边距 2cm
	docxMarginY    = 1418  // 上下页边距 2.5cm
	docxContentW   = docxPageWidth - docxMarginX*2

	// docxIndentTwips 表格单元格每级缩进（用于嵌套字段）
	docxIndentTwips = 170
)

// docxCell 表格单元格内容（支持加粗、等宽字体与层级缩进）。
type docxCell struct {
	Text   string
	Bold   bool
	Mono   bool
	Indent int // 缩进层级（每级 docxIndentTwips）
}

// docxRun 段落内的文本片段（支持混合字体与加粗）。
type docxRun struct {
	Text string
	Bold bool
	Mono bool
}

// docxBuilder 累积 word/document.xml 的正文片段。
type docxBuilder struct {
	body strings.Builder
	seen int // 已输出的接口数量（判断是否需要插入分页符）
}

// xmlEscape 转义 XML 文本内容（同时剔除 XML 1.0 不允许的控制字符）。
func xmlEscape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&apos;")
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// writeRuns 把文本片段渲染为 w:r 序列（换行转 w:br）。
func writeRuns(sb *strings.Builder, runs []docxRun) {
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		sb.WriteString("<w:r><w:rPr>")
		if r.Mono {
			sb.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:eastAsia="Consolas"/>`)
		}
		if r.Bold {
			sb.WriteString("<w:b/>")
		}
		sb.WriteString("</w:rPr>")
		for i, line := range strings.Split(strings.ReplaceAll(r.Text, "\r\n", "\n"), "\n") {
			if i > 0 {
				sb.WriteString("<w:br/>")
			}
			if line == "" {
				continue
			}
			sb.WriteString(`<w:t xml:space="preserve">` + xmlEscape(line) + `</w:t>`)
		}
		sb.WriteString("</w:r>")
	}
}

// para 输出一个段落。
func (b *docxBuilder) para(style string, runs ...docxRun) {
	b.body.WriteString("<w:p><w:pPr>")
	if style != "" {
		b.body.WriteString(`<w:pStyle w:val="` + style + `"/>`)
	}
	b.body.WriteString("</w:pPr>")
	writeRuns(&b.body, runs)
	b.body.WriteString("</w:p>")
}

// text 输出纯文本段落（空文本自动跳过）。
func (b *docxBuilder) text(style, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	b.para(style, docxRun{Text: s})
}

// heading 输出标题段落（1-3 级对应 Word 内置 heading 样式）。
func (b *docxBuilder) heading(level int, s string) {
	b.para(fmt.Sprintf("Heading%d", clampInt(level, 1, 3)), docxRun{Text: s})
}

// code 输出代码块（按行拆段，等宽字体 + 浅灰底纹）。
func (b *docxBuilder) code(s string) {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if strings.TrimSpace(s) == "" {
		return
	}
	for _, line := range strings.Split(s, "\n") {
		b.para("DocCode", docxRun{Text: line, Mono: true})
	}
	b.spacer()
}

// spacer 输出一个空行段落（表格与后续内容之间留白）。
func (b *docxBuilder) spacer() {
	b.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="DocSpacer"/></w:pPr></w:p>`)
}

// pageBreak 插入分页符。
func (b *docxBuilder) pageBreak() {
	b.body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// table 输出表格：首行为表头（浅灰底纹、加粗、跨页重复）。
func (b *docxBuilder) table(headers []string, rows [][]docxCell, widths []int) {
	if len(headers) == 0 || len(rows) == 0 {
		return
	}
	widths = fitWidths(widths, docxContentW)

	var sb strings.Builder
	sb.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="` + fmt.Sprint(docxContentW) + `" w:type="dxa"/>` + docxTableBorders +
		`<w:tblLayout w:type="fixed"/>` + docxTableCellMargin + `</w:tblPr><w:tblGrid>`)
	for _, w := range widths {
		sb.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, w))
	}
	sb.WriteString(`</w:tblGrid>`)

	sb.WriteString(`<w:tr><w:trPr><w:tblHeader/></w:trPr>`)
	for i, h := range headers {
		writeCell(&sb, columnWidth(widths, i), true, docxCell{Text: h, Bold: true})
	}
	sb.WriteString(`</w:tr>`)

	for _, row := range rows {
		sb.WriteString(`<w:tr>`)
		for i := range headers {
			cell := docxCell{}
			if i < len(row) {
				cell = row[i]
			}
			writeCell(&sb, columnWidth(widths, i), false, cell)
		}
		sb.WriteString(`</w:tr>`)
	}
	sb.WriteString(`</w:tbl>`)
	b.body.WriteString(sb.String())
	b.spacer()
}

// writeCell 输出一个单元格；header 为真时附加浅灰底纹（表头样式）。
func writeCell(sb *strings.Builder, width int, header bool, cell docxCell) {
	sb.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, width))
	if header {
		sb.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F7F8FA"/>`)
	}
	sb.WriteString(`</w:tcPr><w:p><w:pPr><w:spacing w:before="0" w:after="0"/>`)
	if indent := cell.Indent * docxIndentTwips; indent > 0 {
		sb.WriteString(fmt.Sprintf(`<w:ind w:left="%d"/>`, indent))
	}
	sb.WriteString(`</w:pPr>`)
	writeRuns(sb, []docxRun{{Text: cell.Text, Bold: cell.Bold, Mono: cell.Mono}})
	sb.WriteString(`</w:p></w:tc>`)
}

// fitWidths 把列宽按正文可用宽度等比缩放，零散误差补到最后一列。
func fitWidths(widths []int, total int) []int {
	sum := 0
	for _, w := range widths {
		sum += w
	}
	if sum <= 0 || sum == total {
		return widths
	}
	scaled := make([]int, len(widths))
	used := 0
	for i, w := range widths {
		scaled[i] = w * total / sum
		used += scaled[i]
	}
	scaled[len(scaled)-1] += total - used
	return scaled
}

// columnWidth 取第 i 列宽度，越界时回退为整行宽度。
func columnWidth(widths []int, i int) int {
	if i < len(widths) {
		return widths[i]
	}
	return docxContentW
}

// ---------------- 内容渲染 ----------------

// docxKVTable 渲染「参数名 / 值 / 说明」三列表格（请求头、Query 参数等）。
func (b *docxBuilder) kvTable(title string, kvs []model.KV) {
	kvs = EnabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	b.heading(3, title)
	rows := make([][]docxCell, 0, len(kvs))
	for _, kv := range kvs {
		rows = append(rows, []docxCell{
			{Text: kv.Key, Mono: true},
			{Text: kv.Value, Mono: true},
			{Text: kv.Description},
		})
	}
	b.table([]string{"参数名", "值/示例", "说明"}, rows, docxKVColWidths)
}

// docxFormTable 渲染带文件类型的表单参数表。
func (b *docxBuilder) formTable(title string, kvs []model.KV) {
	kvs = EnabledKVs(kvs)
	if len(kvs) == 0 {
		return
	}
	b.heading(3, title)
	rows := make([][]docxCell, 0, len(kvs))
	for _, kv := range kvs {
		typ, val := "文本", kv.Value
		if kv.Type == model.FormTypeFile {
			typ = "文件"
			if val == "" {
				val = "（未选择文件）"
			} else {
				_, name := filepath.Split(val)
				val = "📎 " + name
			}
		}
		rows = append(rows, []docxCell{
			{Text: kv.Key, Mono: true},
			{Text: typ},
			{Text: val, Mono: true},
			{Text: kv.Description},
		})
	}
	b.table([]string{"字段名", "类型", "值/文件", "说明"}, rows, docxFormColWidths)
}

// docxFieldTable 渲染结构化字段表（嵌套字段按层级缩进）。
func (b *docxBuilder) fieldTable(title string, fields []*model.Field) {
	if len(fields) == 0 {
		return
	}
	b.heading(3, title)
	rows := [][]docxCell{}
	appendFieldRows(&rows, fields, 0)
	b.table([]string{"字段名", "类型", "必填", "说明", "示例"}, rows, docxFieldColWidths)
}

// appendFieldRows 递归展开字段（含子字段）。
func appendFieldRows(rows *[][]docxCell, fields []*model.Field, depth int) {
	for _, f := range fields {
		*rows = append(*rows, []docxCell{
			{Text: f.Name, Mono: true, Indent: depth},
			{Text: f.Type},
			{Text: boolCN(f.Required)},
			{Text: f.Description},
			{Text: f.Example},
		})
		appendFieldRows(rows, f.Children, depth+1)
	}
}

// api 渲染单个接口（每个接口另起一页，便于打印与分发）。
func (b *docxBuilder) api(api model.ApiInfo, level int, common model.CommonParams, num docNumbering) {
	if b.seen > 0 {
		b.pageBreak()
	}
	b.seen++
	// 接口标题比所属目录低一级（最深到 3 级），与 Markdown/HTML 导出的层级一致
	b.heading(level-1, num.api(api.ID, api.Name))
	b.para("DocURL", docxRun{Text: strings.ToUpper(api.Method) + "  " + api.URL, Mono: true})
	b.text("", api.Description)

	b.kvTable(labelReqHeaders, api.Headers)
	b.kvTable(labelQueryParams, api.Query)
	b.formTable(labelFormParams, api.FormItems)
	b.kvTable(labelCommonHeaders, common.Headers)
	b.kvTable(labelCommonQuery, common.Query)
	b.fieldTable(labelReqFields, api.ReqFields)
	b.fieldTable(labelRespFields, api.RespFields)

	if api.BodyType == "json" && strings.TrimSpace(api.Body) != "" {
		b.heading(3, labelReqExample)
		b.code(api.Body)
	}
	if curl := curlSample(api, common); curl != "" {
		b.text("DocH4", labelCurlSample)
		b.code(curl)
	}
	if body := responseSampleBody(api); body != "" {
		b.heading(3, labelRespExample)
		b.code(body)
	}
}

// dir 递归渲染目录及其接口。
func (b *docxBuilder) dir(dirs []model.Directory, apis []model.ApiInfo, parentID string, level int, common model.CommonParams, num docNumbering) {
	for _, api := range dirApis(apis, parentID) {
		b.api(api, level, common, num)
	}
	for _, d := range childDirs(dirs, parentID) {
		if b.seen > 0 {
			b.pageBreak()
		}
		b.heading(level-1, num.dir(d.ID, d.Name))
		b.dir(dirs, apis, d.ID, level+1, common, num)
	}
}

// buildDocxBody 生成正文 XML：按 layout 输出封面 / 修订记录 / 目录 / 公共描述 /
// 统一定义规范 / 接口正文 / 附录；未配置时退化为「标题 + 接口树」的最小文档。
func buildDocxBody(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, layout model.DocLayout) string {
	b := &docxBuilder{}
	now := time.Now()
	sections := buildDocSections(title, layout, now)
	num := buildDocNumbering(sections, dirs, apis, rootID, layout.Numbering)
	multi := len(sections) > 1
	covered := false
	first := true

	for _, s := range sections {
		// 每个章节另起一页（文档第一块除外）
		if !first {
			b.pageBreak()
		}
		first = false
		switch s.Kind {
		case sectionCover:
			covered = true
			b.cover(s.Cover)
		case sectionRevisions:
			b.heading(1, num.section(s.ID, s.Title))
			rows := make([][]docxCell, 0, len(s.Revisions))
			for _, r := range s.Revisions {
				rows = append(rows, []docxCell{{Text: r.Version}, {Text: r.Date}, {Text: r.Author}, {Text: r.Note}})
			}
			b.table([]string{"版本", "日期", "作者", "修订说明"}, rows, []int{1400, 2000, 1800, 4438})
		case sectionTOC:
			b.tocField()
		case sectionPreface, sectionConvention:
			b.heading(1, num.section(s.ID, s.Title))
			b.miniMarkdown(s.Content)
		case sectionAPIs:
			if !covered {
				b.para("DocTitle", docxRun{Text: title})
				b.para("DocMeta", docxRun{Text: fmt.Sprintf("共 %d 个接口 · 导出时间 %s · 由 ApiTool 生成",
					len(apis), now.Format("2006-01-02 15:04"))})
			} else if multi {
				b.heading(1, num.section(s.ID, labelAPISection))
			}
			if dirs == nil && len(apis) == 1 && rootID == "" {
				b.api(apis[0], 2, common, num)
			} else {
				b.dir(dirs, apis, rootID, 2, common, num)
			}
		case sectionBizCodes, sectionEnums, sectionSupport:
			b.heading(1, num.section(s.ID, s.Title))
			b.miniMarkdown(s.Content)
		case sectionAppendix:
			b.heading(1, num.section(s.ID, s.Title))
			rows := make([][]docxCell, 0, len(commonStatusCodes))
			for _, sc := range commonStatusCodes {
				rows = append(rows, []docxCell{
					{Text: fmt.Sprint(sc.Code), Mono: true}, {Text: sc.Name}, {Text: sc.Meaning}, {Text: sc.Usage},
				})
			}
			b.table([]string{"状态码", "名称", "含义", "典型场景"}, rows, []int{1100, 2200, 2600, 3738})
		}
	}
	return b.body.String()
}

// cover 输出封面页内容（大标题 + 副标题 + 作者/单位/版本/日期）。
func (b *docxBuilder) cover(c model.DocCover) {
	b.para("DocCoverTitle", docxRun{Text: c.Title})
	if strings.TrimSpace(c.Subtitle) != "" {
		b.para("DocCoverSubtitle", docxRun{Text: c.Subtitle})
	}
	for _, line := range [][2]string{{"作者", c.Author}, {"单位", c.Org}, {"版本", c.Version}, {"日期", c.Date}} {
		if strings.TrimSpace(line[1]) == "" {
			continue
		}
		b.para("DocCoverLine", docxRun{Text: line[0] + "：" + line[1]})
	}
}

// tocField 输出 Word 目录域：Word 中可一键更新出带页码的目录。
func (b *docxBuilder) tocField() {
	b.heading(1, "目录")
	b.body.WriteString(`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:rPr><w:color w:val="000000"/></w:rPr><w:t>（打开文档后按 Ctrl+A 再按 F9，或右键「更新域」，即可生成带页码的目录）</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
}

// miniMarkdown 渲染简化 Markdown 正文（前言 / 统一定义规范）。
func (b *docxBuilder) miniMarkdown(src string) {
	for _, block := range parseMiniMarkdown(src) {
		switch block.Kind {
		case miniHeading:
			b.heading(clampInt(block.Level+1, 2, 3), miniPlain(block.Text))
		case miniBulletList:
			for _, it := range block.Items {
				b.para("DocBullet", docxRun{Text: "• " + miniPlain(it)})
			}
		case miniOrderedList:
			for i, it := range block.Items {
				b.para("DocBullet", docxRun{Text: fmt.Sprintf("%d. %s", i+1, miniPlain(it))})
			}
		case miniQuote:
			b.para("DocQuote", docxRun{Text: miniPlain(block.Text)})
		case miniCode:
			b.code(block.Text)
		case miniTable:
			rows := make([][]docxCell, 0, len(block.Rows))
			for _, row := range block.Rows {
				cells := make([]docxCell, 0, len(row))
				for _, c := range row {
					cells = append(cells, docxCell{Text: miniPlain(c)})
				}
				rows = append(rows, cells)
			}
			widths := make([]int, len(block.Header))
			for i := range widths {
				widths[i] = 1 // 交给 fitWidths 均分正文宽度
			}
			b.table(miniPlainAll(block.Header), rows, widths)
		default:
			for _, line := range strings.Split(block.Text, "\n") {
				b.text("", miniPlain(line))
			}
		}
	}
}

// miniPlainAll 批量去行内标记。
func miniPlainAll(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, miniPlain(it))
	}
	return out
}

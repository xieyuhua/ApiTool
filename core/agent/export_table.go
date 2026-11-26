package agent

// 表格导出：把回答中的数据（Markdown 表格 / CSV / JSON 数组）导出成
// Excel(xlsx) / CSV / HTML / Markdown 文件。
//
// 不引入第三方 Excel 库：xlsx 本质是「zip 包 + XML」，用标准库
// archive/zip 与 encoding/xml 即可生成（项目为 vendor 模式，新增依赖会
// 显著增大构建体积）。

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TableData 表格数据：首行为表头，其余为数据行。
type TableData struct {
	Title   string
	Headers []string
	Rows    [][]string
}

// mdSepRowRe 匹配 Markdown 表格的分隔行（如 ---、:--:、---:）
var mdSepRowRe = regexp.MustCompile(`^[|\s:\-]+$`)

// ============================ 解析 ============================

// parseTableData 从多种输入形态解析表格。
// 支持 Markdown 表格文本、CSV/TSV 文本、JSON 二维数组与对象数组。
func parseTableData(title, raw string) (*TableData, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("data 为空：请把要导出的表格内容（Markdown 表格、CSV 文本或 JSON 数组）传入 data")
	}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		if t, err := parseJSONTable(s); err == nil && len(t.Rows) > 0 {
			t.Title = title
			return t, nil
		}
	}
	if looksLikeMarkdownTable(s) {
		if t, err := parseMarkdownTable(s); err == nil {
			t.Title = title
			return t, nil
		}
	}
	if t, err := parseCSVTable(s); err == nil && len(t.Rows) > 0 {
		t.Title = title
		return t, nil
	}
	if t, err := parseMarkdownTable(s); err == nil {
		t.Title = title
		return t, nil
	}
	return nil, fmt.Errorf("无法解析 data：既不是合法的 Markdown 表格、CSV 文本，也不是 JSON 数组")
}

func looksLikeMarkdownTable(s string) bool {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "|") && strings.Count(l, "|") >= 2 {
			n++
		}
	}
	return n >= 2
}

// parseMarkdownTable 解析 Markdown 表格
func parseMarkdownTable(s string) (*TableData, error) {
	t := &TableData{}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "```") {
			continue
		}
		cells := splitMarkdownRow(l)
		if len(cells) == 0 {
			continue
		}
		// 表头之后遇到的第一个分隔行要跳过
		if mdSepRowRe.MatchString(strings.Trim(l, "|")) && len(t.Headers) > 0 && len(t.Rows) == 0 {
			continue
		}
		if len(t.Headers) == 0 {
			t.Headers = cells
			continue
		}
		t.Rows = append(t.Rows, cells)
	}
	if len(t.Headers) == 0 {
		return nil, fmt.Errorf("未解析到表头")
	}
	// 补齐列数不一致的行
	n := len(t.Headers)
	for i, r := range t.Rows {
		for len(r) < n {
			r = append(r, "")
		}
		if len(r) > n {
			t.Rows[i] = r[:n]
		}
	}
	return t, nil
}

// splitMarkdownRow 拆分一行单元格，支持转义竖线
func splitMarkdownRow(line string) []string {
	l := strings.TrimSpace(line)
	if !strings.HasPrefix(l, "|") {
		return nil
	}
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(l); i++ {
		c := l[i]
		if c == 92 && i+1 < len(l) && l[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if c == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

// parseCSVTable 解析 CSV / TSV 文本
func parseCSVTable(s string) (*TableData, error) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	delim := ','
	first := strings.SplitN(s, "\n", 2)[0]
	if !strings.Contains(first, ",") && strings.Contains(first, "\t") {
		delim = '\t'
	}
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	recs, err := r.ReadAll()
	if err != nil || len(recs) == 0 {
		return nil, fmt.Errorf("CSV 解析失败")
	}
	return &TableData{Headers: recs[0], Rows: recs[1:]}, nil
}

// parseJSONTable 解析 JSON 二维数组或对象数组
func parseJSONTable(s string) (*TableData, error) {
	var arr []interface{}
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("数组为空")
	}
	if _, ok := arr[0].(map[string]interface{}); ok {
		var headers []string
		seen := map[string]bool{}
		for _, it := range arr {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			for k := range m {
				if !seen[k] {
					seen[k] = true
					headers = append(headers, k)
				}
			}
		}
		sort.Strings(headers)
		t := &TableData{Headers: headers}
		for _, it := range arr {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			row := make([]string, len(headers))
			for i, h := range headers {
				row[i] = anyToString(m[h])
			}
			t.Rows = append(t.Rows, row)
		}
		return t, nil
	}
	t := &TableData{}
	for i, row := range arr {
		r2, ok := row.([]interface{})
		if !ok {
			return nil, fmt.Errorf("非二维数组")
		}
		cells := make([]string, len(r2))
		for j, v := range r2 {
			cells[j] = anyToString(v)
		}
		if i == 0 {
			t.Headers = cells
			continue
		}
		t.Rows = append(t.Rows, cells)
	}
	if len(t.Headers) == 0 {
		return nil, fmt.Errorf("无表头")
	}
	return t, nil
}

func anyToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []interface{}, map[string]interface{}:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// ============================ 导出 ============================

// exportTableSupportedFormats 支持的导出格式
var exportTableSupportedFormats = []string{"xlsx", "csv", "html", "md"}

// exportTable 把表格写入 dir，返回生成的文件绝对路径
func exportTable(t *TableData, format, dir, baseName string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "xlsx"
	}
	ok := false
	for _, f := range exportTableSupportedFormats {
		if f == format {
			ok = true
			break
		}
	}
	if !ok {
		return "", fmt.Errorf("不支持的格式 %q，可选：%s", format, strings.Join(exportTableSupportedFormats, " / "))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := sanitizeFileName(baseName)
	if name == "" {
		name = "export"
	}
	path := filepath.Join(dir, name+"_"+time.Now().Format("20060102_150405")+"."+format)

	switch format {
	case "csv":
		b, err := tableToCSV(t)
		if err != nil {
			return "", err
		}
		err = os.WriteFile(path, b, 0o644)
		return path, err
	case "md":
		return path, os.WriteFile(path, []byte(tableToMarkdown(t)), 0o644)
	case "html":
		return path, os.WriteFile(path, []byte(tableToHTML(t)), 0o644)
	default:
		return path, writeXlsx(path, t)
	}
}

// sanitizeFileName 去掉文件名中的非法字符
func sanitizeFileName(s string) string {
	s = strings.TrimSpace(s)
	rep := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\n", " ", "\r", " ",
	)
	s = rep.Replace(s)
	if len([]rune(s)) > 60 {
		s = string([]rune(s)[:60])
	}
	return strings.TrimSpace(s)
}

func tableToCSV(t *TableData) ([]byte, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	if err := w.Write(t.Headers); err != nil {
		return nil, err
	}
	for _, r := range t.Rows {
		if err := w.Write(r); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	// 加 UTF-8 BOM，Excel 打开中文不乱码
	return append([]byte{0xEF, 0xBB, 0xBF}, []byte(buf.String())...), nil
}

func tableToMarkdown(t *TableData) string {
	var sb strings.Builder
	if t.Title != "" {
		sb.WriteString("## " + t.Title + "\n\n")
	}
	sb.WriteString("| " + strings.Join(t.Headers, " | ") + " |\n")
	sep := make([]string, len(t.Headers))
	for i := range sep {
		sep[i] = "---"
	}
	sb.WriteString("| " + strings.Join(sep, " | ") + " |\n")
	for _, r := range t.Rows {
		sb.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	return sb.String()
}

func tableToHTML(t *TableData) string {
	var sb strings.Builder
	sb.WriteString("<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	if t.Title != "" {
		sb.WriteString("<title>" + htmlEscape(t.Title) + "</title>")
	}
	sb.WriteString("<style>body{font-family:system-ui,'Microsoft YaHei',sans-serif;padding:24px;color:#1f2329}")
	sb.WriteString("h2{font-size:18px;margin:0 0 12px}table{border-collapse:collapse;width:100%;font-size:13px}")
	sb.WriteString("th,td{border:1px solid #d0d3d6;padding:6px 10px;text-align:left}")
	sb.WriteString("th{background:#f2f3f5;font-weight:600}tr:nth-child(even) td{background:#fafbfc}")
	sb.WriteString("</style></head><body>")
	if t.Title != "" {
		sb.WriteString("<h2>" + htmlEscape(t.Title) + "</h2>")
	}
	sb.WriteString("<table><thead><tr>")
	for _, h := range t.Headers {
		sb.WriteString("<th>" + htmlEscape(h) + "</th>")
	}
	sb.WriteString("</tr></thead><tbody>")
	for _, r := range t.Rows {
		sb.WriteString("<tr>")
		for _, c := range r {
			sb.WriteString("<td>" + htmlEscape(c) + "</td>")
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("</tbody></table>")
	fmt.Fprintf(&sb, "<p style=\"color:#86909c;font-size:12px\">共 %d 行 · 导出于 %s</p>",
		len(t.Rows), time.Now().Format("2006-01-02 15:04:05"))
	sb.WriteString("</body></html>")
	return sb.String()
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}

// xmlEscape 转义 XML 文本（复用 HTML 转义规则，够用且避免重复实现）
func xmlEscape(s string) string { return htmlEscape(s) }

// ============================ 最小 XLSX 生成 ============================

// colName 列序号转 Excel 列名：0->A, 25->Z, 26->AA
func colName(n int) string {
	name := ""
	for n >= 0 {
		name = string(rune('A'+n%26)) + name
		n = n/26 - 1
	}
	return name
}

// xlsxEscape XML 转义，并剔除非法控制字符（否则 Excel 报文件已损坏）
func xlsxEscape(s string) string {
	var sb strings.Builder
	for _, ru := range s {
		if ru < 0x20 && ru != '\t' && ru != '\n' && ru != '\r' {
			continue
		}
		sb.WriteRune(ru)
	}
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(sb.String())
}

// isNumeric 判断能否写成 xlsx 数值单元格
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, ",%") {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// writeXlsx 生成 xlsx（zip + XML，无第三方依赖）
func writeXlsx(path string, t *TableData) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	add := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(content))
		return err
	}
	if err := add("[Content_Types].xml", xlsxContentTypes); err != nil {
		return err
	}
	if err := add("_rels/.rels", xlsxRels); err != nil {
		return err
	}
	if err := add("xl/workbook.xml", xlsxWorkbook(sheetName(t.Title))); err != nil {
		return err
	}
	if err := add("xl/_rels/workbook.xml.rels", xlsxWorkbookRels); err != nil {
		return err
	}
	if err := add("xl/styles.xml", xlsxStyles); err != nil {
		return err
	}
	if err := add("xl/worksheets/sheet1.xml", xlsxSheet(t)); err != nil {
		return err
	}
	return zw.Close()
}

func sheetName(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Sheet1"
	}
	title = strings.NewReplacer("[", "", "]", "", ":", "", "*", "", "?", "", "/", "", "\\", "").Replace(title)
	if len([]rune(title)) > 31 {
		title = string([]rune(title)[:31])
	}
	return title
}

// xlsxSheet 生成 sheet XML：表头加粗，纯数字写成数值单元格（Excel 可直接求和排序）
func xlsxSheet(t *TableData) string {
	var sb strings.Builder
	sb.WriteString(xml.Header)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols>`)
	for i := range t.Headers {
		sb.WriteString(fmt.Sprintf(`<col min="%d" max="%d" width="14" customWidth="1"/>`, i+1, i+1))
	}
	sb.WriteString(`</cols><sheetData>`)
	row := func(idx int, cells []string, header bool) {
		sb.WriteString(fmt.Sprintf(`<row r="%d">`, idx))
		for i, c := range cells {
			ref := fmt.Sprintf("%s%d", colName(i), idx)
			if isNumeric(c) {
				sb.WriteString(fmt.Sprintf(`<c r="%s"><v>%s</v></c>`, ref, xlsxEscape(c)))
				continue
			}
			style := ""
			if header {
				style = ` s="1"`
			}
			sb.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`,
				ref, style, xlsxEscape(c)))
		}
		sb.WriteString(`</row>`)
	}
	row(1, t.Headers, true)
	for i, r := range t.Rows {
		row(i+2, r, false)
	}
	sb.WriteString(`</sheetData></worksheet>`)
	return sb.String()
}

const xlsxContentTypes = xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
	`</Types>`

const xlsxRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const xlsxWorkbookRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

// xlsxStyles 最小样式表：s=1 加粗（表头）
const xlsxStyles = xml.Header + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font>` +
	`<font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="1"><fill><patternFill patternType="none"/></fill></fills>` +
	`<borders count="1"><border/></borders>` +
	`<cellStyleXfs count="1"><xf/></cellStyleXfs>` +
	`<cellXfs count="2"><xf xfId="0"/><xf xfId="0" fontId="1" applyFont="1"/></cellXfs>` +
	`</styleSheet>`

func xlsxWorkbook(sheet string) string {
	return xml.Header +
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<sheets><sheet name="` + xmlEscape(sheet) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`
}

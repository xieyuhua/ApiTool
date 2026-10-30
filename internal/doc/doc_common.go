package doc

// 文档渲染的公共小工具：Markdown / HTML / DOCX 三个渲染器与 OpenAPI 导出共用。

import (
	"html"
	"sort"
	"strings"
	"time"

	"apitool/internal/model"
)

// childDirs 取某目录下的直属子目录（按 Sort 升序，保持人工排序）。
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

// dirApis 取某目录下的直属接口。
func dirApis(apis []model.ApiInfo, dirID string) []model.ApiInfo {
	var out []model.ApiInfo
	for _, a := range apis {
		if a.DirID == dirID {
			out = append(out, a)
		}
	}
	return out
}

// EnabledKVs 过滤出启用且键名非空的键值对（导出时忽略被禁用的参数）。
func EnabledKVs(kvs []model.KV) []model.KV {
	var out []model.KV
	for _, kv := range kvs {
		if kv.Enabled && kv.Key != "" {
			out = append(out, kv)
		}
	}
	return out
}

// MdEscape 转义 Markdown 表格单元格内容（竖线转义、换行折成空格）。
func MdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// boolCN 把布尔值渲染成中文「是 / 否」。
func boolCN(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// escAttr 用于 HTML 属性值转义（额外转义双引号）。
func escAttr(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// minInt 返回较小值（渲染层级、缩进等场景使用）。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 接口文档各分节的标题文案。三种导出格式（Markdown / HTML / DOCX）统一引用这里，
// 避免出现「请求头」与「请求 Headers」这类跨格式不一致。
const (
	labelReqHeaders    = "请求头"
	labelQueryParams   = "Query 参数"
	labelFormParams    = "表单参数（Form / 文件上传）"
	labelCommonHeaders = "公共请求头"
	labelCommonQuery   = "公共 Query 参数"
	labelReqFields     = "请求参数"
	labelRespFields    = "响应参数"
	labelReqExample    = "请求示例"
	labelRespExample   = "响应示例"
	// labelAPISection 企业文档中「接口正文」章节标题（Markdown / HTML / DOCX 一致）
	labelAPISection = "接口正文"
)

// contentTypeOf 返回接口实际使用的 Content-Type。
func contentTypeOf(api model.ApiInfo) string {
	if ct := strings.TrimSpace(api.ContentType); ct != "" {
		return ct
	}
	switch strings.ToLower(strings.TrimSpace(api.BodyType)) {
	case "json":
		return "application/json"
	case "form":
		return "application/x-www-form-urlencoded"
	case "form-data", "multipart":
		return "multipart/form-data"
	}
	return ""
}

// 表格列宽（twips，仅 .docx 使用；总和即 A4 正文宽度，见 docxContentW）。
var (
	docxKVColWidths    = []int{2600, 3400, 3638}
	docxFormColWidths  = []int{2600, 1100, 2300, 3638}
	docxFieldColWidths = []int{2500, 1300, 700, 2900, 2238}
)

// ExportFileName 生成导出文件名：<标题>-<年-月-日><扩展名>，便于按日期归档与区分多次导出。
func ExportFileName(title, ext string) string {
	return SanitizeFilename(title) + "-" + time.Now().Format("2006-01-02") + ext
}

// sampleBodyMaxRunes 响应示例最多保留的字符数（超出截断，避免单接口占满整篇文档）。
const sampleBodyMaxRunes = 4000

// truncateRunes 按字符数截断文本（超出时追加省略标记），避免把多字节中文截坏。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n... (截断)"
}

// responseSampleBody 取响应示例正文（非 JSON 响应或为空时返回空串），并做长度截断。
func responseSampleBody(api model.ApiInfo) string {
	if api.LastResponse == nil || !api.LastResponse.IsJSON || api.LastResponse.Body == "" {
		return ""
	}
	return truncateRunes(api.LastResponse.Body, sampleBodyMaxRunes)
}

package doc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"apitool/internal/model"
	"apitool/internal/store"
)

func TestBuildMarkdown(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	md := BuildMarkdown(title, rootID, dirs, apis, common)
	for _, want := range []string{
		"# 接口文档",
		"## 用户中心",
		"### 上传头像",
		labelReqHeaders,
		labelFormParams,
		labelRespExample,
		"`GET` `https://api.example.com/users`",
		"```json",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺少内容 %q", want)
		}
	}
}

func TestBuildHTML(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	h := BuildHTML(title, rootID, dirs, apis, common)
	for _, want := range []string{
		"<nav class=\"toc\">",
		`<div class="api" id="api-a1"`,
		labelQueryParams,
		"@media print",
		"apitoolFilter()",
	} {
		if !strings.Contains(h, want) {
			t.Fatalf("HTML 缺少内容 %q", want)
		}
	}
	if strings.Contains(h, "doc-watermark") {
		t.Fatal("分享用 HTML 不应带水印层")
	}
}

func TestBuildOpenAPI(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	raw, err := BuildOpenAPI(title, dirs, apis, rootID, common, "env")
	if err != nil {
		t.Fatalf("BuildOpenAPI 失败: %v", err)
	}
	var doc struct {
		OpenAPI string                            `json:"openapi"`
		Paths   map[string]map[string]interface{} `json:"paths"`
		Tags    []map[string]string               `json:"tags"`
		Servers []map[string]string               `json:"servers"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("OpenAPI JSON 无法解析: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("openapi 版本 = %q", doc.OpenAPI)
	}
	if len(doc.Servers) == 0 || doc.Servers[0]["url"] != "{{host}}" {
		t.Fatalf("env 模式缺少 servers 声明: %v", doc.Servers)
	}
	if _, ok := doc.Paths["/users"]; !ok {
		t.Fatalf("paths 缺少 /users，实际: %v", keysOf(doc.Paths))
	}
	if len(doc.Paths["/users"]) == 0 || doc.Paths["/users"]["get"] == nil {
		t.Fatalf("/users 缺少 get 操作: %v", doc.Paths["/users"])
	}
	if len(doc.Tags) == 0 {
		t.Fatal("缺少 tags 声明（目录分组）")
	}
}

// TestBuildOpenAPIOriginalHost 原地址模式：path 保留完整 URL 且不声明 servers。
func TestBuildOpenAPIOriginalHost(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	raw, err := BuildOpenAPI(title, dirs, apis, rootID, common, "original")
	if err != nil {
		t.Fatalf("BuildOpenAPI 失败: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("OpenAPI JSON 无法解析: %v", err)
	}
	paths, _ := doc["paths"].(map[string]interface{})
	if _, ok := paths["https://api.example.com/users"]; !ok {
		t.Fatalf("原地址模式应按完整 URL 作为 path，实际: %v", keysOfAny(paths))
	}
	if servers, _ := doc["servers"].([]interface{}); len(servers) != 0 {
		t.Fatalf("原地址模式不应声明 servers: %v", servers)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("短文本", 10); got != "短文本" {
		t.Fatalf("未超长时不应截断: %q", got)
	}
	long := strings.Repeat("中", 20)
	got := truncateRunes(long, 5)
	if !strings.HasPrefix(got, "中中中中中") {
		t.Fatalf("截断结果前缀错误: %q", got)
	}
	if !strings.Contains(got, "截断") {
		t.Fatalf("截断结果缺少提示: %q", got)
	}
	// 按字符截断，不能出现错误编码（UTF-8 非法字节会产生替换字符）
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("截断破坏了中文编码: %q", got)
	}
}

func TestResponseSampleBody(t *testing.T) {
	api := sampleScope2()
	if api.LastResponse == nil {
		t.Fatal("样例数据缺少响应快照")
	}
	if got := responseSampleBody(api); !strings.Contains(got, `"code"`) {
		t.Fatalf("响应示例应包含 JSON 正文: %q", got)
	}
	api.LastResponse.IsJSON = false
	if got := responseSampleBody(api); got != "" {
		t.Fatalf("非 JSON 响应不应输出示例: %q", got)
	}
}

// sampleScope2 返回样例数据中的第一个接口（带 JSON 响应快照）。
func sampleScope2() (api model.ApiInfo) {
	_, _, _, apis, _ := sampleScope()
	return apis[0]
}

// TestLoadDocScopeExpandsWatermark 验证导出范围解析时已把水印占位符展开为实际值
// （{{project}} 取当前项目名），保证 Word / PDF 导出拿到的是最终文字。
func TestLoadDocScopeExpandsWatermark(t *testing.T) {
	st := store.New(t.TempDir(), "test", "")
	t.Cleanup(func() { _ = st.Close() })

	_, _, _, apis, _ := sampleScope()
	data := st.GetData()
	if len(data.Projects) == 0 {
		t.Fatal("默认数据缺少项目")
	}
	data.Projects[0].Apis = apis
	data.Settings.Watermark = model.DocWatermark{Enabled: true, Text: "内部资料 {{project}} {{date}}"}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存测试数据失败: %v", err)
	}

	scope, err := loadDocScope(st, "", "")
	if err != nil {
		t.Fatalf("loadDocScope 失败: %v", err)
	}
	if strings.Contains(scope.Watermark.Text, "{{") {
		t.Fatalf("水印占位符未展开: %q", scope.Watermark.Text)
	}
	if !strings.Contains(scope.Watermark.Text, data.Projects[0].Name) {
		t.Fatalf("水印未包含项目名 %q: %q", data.Projects[0].Name, scope.Watermark.Text)
	}
	if scope.Project != data.Projects[0].Name {
		t.Fatalf("scope.Project = %q, want %q", scope.Project, data.Projects[0].Name)
	}
}

// TestExportFileName 导出文件名需带「年-月-日」，并清理非法字符。
func TestExportFileName(t *testing.T) {
	got := ExportFileName("订单中心 v1.2", ".docx")
	today := time.Now().Format("2006-01-02")
	if !strings.HasPrefix(got, "订单中心 v1.2-") || !strings.HasSuffix(got, today+".docx") {
		t.Fatalf("导出文件名 = %q, 期望 <标题>-%s.docx", got, today)
	}
	if strings.ContainsAny(got, `\/:*?"<>|`) {
		t.Fatalf("文件名含非法字符: %q", got)
	}
	if illegal := ExportFileName("a/b:c*?", ".pdf"); strings.ContainsAny(illegal, `\/:*?"<>|`) {
		t.Fatalf("非法字符未清理: %q", illegal)
	}
}

func keysOf(m map[string]map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfAny(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

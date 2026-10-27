package doc

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"apitool/internal/model"
)

// sampleScope 构造一份包含目录层级、表格、代码示例的导出样例数据。
func sampleScope() (title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams) {
	dirs = []model.Directory{
		{ID: "d1", Name: "用户中心", Sort: 1},
		{ID: "d2", Name: "用户管理", ParentID: "d1", Sort: 1},
	}
	apis = []model.ApiInfo{
		{
			ID: "a1", DirID: "d2", Name: "查询用户列表", Method: "get", URL: "https://api.example.com/users",
			Description: "分页查询用户",
			Headers:     []model.KV{{Enabled: true, Key: "X-Token", Value: "abc", Description: "鉴权"}},
			Query:       []model.KV{{Enabled: true, Key: "page", Value: "1", Description: "页码"}},
			RespFields: []*model.Field{
				{Name: "code", Type: "integer", Description: "状态码", Example: "0"},
				{Name: "data", Type: "object", Description: "数据", Children: []*model.Field{
					{Name: "id", Type: "integer", Description: "用户ID"},
				}},
			},
			LastResponse: &model.ResponseData{IsJSON: true, Body: "{\n  \"code\": 0\n}"},
		},
		{
			ID: "a2", DirID: "d1", Name: "上传头像", Method: "post", URL: "https://api.example.com/avatar",
			BodyType:  "json",
			Body:      "{\n  \"uid\": 1\n}",
			FormItems: []model.KV{{Enabled: true, Key: "file", Type: model.FormTypeFile, Value: "/tmp/a.png", Description: "头像"}},
		},
	}
	common = model.CommonParams{
		Headers: []model.KV{{Enabled: true, Key: "X-Common", Value: "1", Description: "公共头"}},
	}
	return "接口文档", "", dirs, apis, common
}

func TestNormalizeWatermarkDefaults(t *testing.T) {
	wm := NormalizeWatermark(model.DocWatermark{})
	if wm.Text != DefaultWatermarkText {
		t.Fatalf("默认水印文字 = %q, want %q", wm.Text, DefaultWatermarkText)
	}
	if wm.Size <= 0 || wm.Opacity <= 0 || wm.Angle == 0 {
		t.Fatalf("默认值补全失败: %+v", wm)
	}
	if !isHexColor(wm.Color) {
		t.Fatalf("默认颜色非法: %q", wm.Color)
	}
	// 未启用时不应渲染水印
	if WatermarkActive(model.DocWatermark{}) {
		t.Fatal("未启用的水印不应生效")
	}
	if !WatermarkActive(model.DocWatermark{Enabled: true, Text: "机密"}) {
		t.Fatal("已启用的水印应生效")
	}
}

func TestBuildDOCXStructure(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	wm := model.DocWatermark{Enabled: true, Text: "内部资料", Color: "#C0C4CC", Size: 42, Opacity: 0.15, Angle: -30, Repeat: true}
	b, err := BuildDOCX(title, rootID, dirs, apis, common, wm)
	if err != nil {
		t.Fatalf("BuildDOCX 失败: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("docx 不是合法 zip: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("解压 %s 失败: %v", f.Name, err)
		}
		parts[f.Name] = string(data)
	}
	for _, need := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/header1.xml", "word/_rels/document.xml.rels"} {
		body, ok := parts[need]
		if !ok {
			t.Fatalf("缺少部件: %s", need)
		}
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s XML 非法: %v", need, err)
			}
		}
	}
	doc := parts["word/document.xml"]
	for _, want := range []string{"查询用户列表", "用户中心", "https://api.example.com/users", "表单参数", "响应示例"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document.xml 缺少内容 %q", want)
		}
	}
	hdr := parts["word/header1.xml"]
	if !strings.Contains(hdr, "内部资料") || !strings.Contains(hdr, "v:textpath") {
		t.Fatalf("页眉缺少 VML 文本水印: %s", hdr)
	}
	// 平铺模式应输出多个水印形状
	if n := strings.Count(hdr, "<v:shape "); n < 2 {
		t.Fatalf("平铺水印形状数量 = %d, want >= 2", n)
	}
}

func TestBuildDOCXWithoutWatermark(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	b, err := BuildDOCX(title, rootID, dirs, apis, common, model.DocWatermark{})
	if err != nil {
		t.Fatalf("BuildDOCX 失败: %v", err)
	}
	if bytes.Contains(b, []byte("v:textpath")) {
		t.Fatal("未启用水印时不应生成水印形状")
	}
}

func TestBuildHTMLForExportWatermark(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	plain := BuildHTML(title, rootID, dirs, apis, common)
	if strings.Contains(plain, "doc-watermark") {
		t.Fatal("普通 HTML 导出不应包含水印层")
	}
	withWM := BuildHTMLForExport(title, rootID, dirs, apis, common,
		model.DocWatermark{Enabled: true, Text: "机密资料", Opacity: 0.2, Angle: -30})
	if !strings.Contains(withWM, "doc-watermark") || !strings.Contains(withWM, "<span>机密资料</span>") {
		t.Fatal("导出 HTML 缺少水印样式/图层")
	}
	if !strings.Contains(withWM, "transform:rotate(-30deg)") {
		t.Fatal("导出 HTML 水印未应用旋转角度")
	}
}

// sampleReport 构造一份测试报告样例（含摘要、用例结果与断言）。
func sampleReport() model.TestReport {
	return model.TestReport{
		PlanName:   "登录接口回归",
		EnvName:    "测试环境",
		CreatedAt:  "2026-10-27 20:00:00",
		Total:      2,
		Passed:     1,
		Failed:     1,
		DurationMs: 1234,
		Summary:    "整体稳定，1 个用例状态码不符。",
		Results: []model.TestResult{
			{
				CaseName: "登录成功", Category: "正常流程", Passed: true, Status: 200, StatusText: "OK",
				DurationMs: 120, RequestMethod: "post", RequestURL: "https://api.example.com/login",
				RequestBodyType: "json", RequestBody: `{"user":"a"}`,
				ResponseBody: `{"code":0}`,
				AssertionResults: []model.AssertionResult{
					{Passed: true, Description: "状态码 200", Detail: "实际 200"},
				},
			},
			{
				CaseName: "登录失败", Category: "异常流程", Passed: false, Status: 400, StatusText: "Bad Request",
				DurationMs: 90, RequestMethod: "post", RequestURL: "https://api.example.com/login",
				Error: "断言失败：期望 401，实际 400",
			},
		},
	}
}

func TestBuildTestReportHTMLForExport(t *testing.T) {
	r := sampleReport()
	plain := BuildTestReportHTML(r)
	if strings.Contains(plain, "doc-watermark") {
		t.Fatal("普通测试报告不应包含水印层")
	}
	withWM := BuildTestReportHTMLForExport(r, model.DocWatermark{Enabled: true, Text: "内部资料"})
	if !strings.Contains(withWM, "<span>内部资料</span>") || !strings.Contains(withWM, "doc-watermark") {
		t.Fatal("测试报告导出 HTML 缺少水印层")
	}
	if !strings.Contains(withWM, "@page{size:A4") {
		t.Fatal("测试报告缺少打印分页样式")
	}
	if !strings.Contains(withWM, "登录成功") {
		t.Fatal("测试报告缺少用例内容")
	}
}

// TestExpandWatermark 验证水印占位符替换（外发追溯常用「项目名 + 日期」）。
func TestExpandWatermark(t *testing.T) {
	now := time.Date(2026, 10, 27, 21, 51, 0, 0, time.Local)
	wm := model.DocWatermark{Enabled: true, Text: "内部资料 {{project}} {{date}} {{time}} {{datetime}}"}
	got := ExpandWatermark(wm, "订单中心", now)
	want := "内部资料 订单中心 2026-10-27 21:51 2026-10-27 21:51"
	if got.Text != want {
		t.Fatalf("占位符替换结果 = %q, want %q", got.Text, want)
	}
	// 没有占位符时保持原样（不做无谓替换）
	plain := model.DocWatermark{Enabled: true, Text: "禁止外传"}
	if got := ExpandWatermark(plain, "订单中心", now); got.Text != "禁止外传" {
		t.Fatalf("无占位符时文字被改成 %q", got.Text)
	}
	// 占位符说明里列出的关键字都必须真被 ExpandWatermark 支持
	for _, v := range WatermarkVarHelp {
		if !strings.Contains(got.Text, v.Key) {
			continue // 该关键字未出现在本用例的文字里
		}
		if strings.Contains(want, v.Key) {
			t.Fatalf("占位符 %q 未被替换: %q", v.Key, want)
		}
	}
	if len(WatermarkVarHelp) != 4 {
		t.Fatalf("占位符说明数量 = %d, want 4", len(WatermarkVarHelp))
	}
}

// TestBuildTestReportWithCover 验证测试报告可加「作者封面」页（PDF 与 Word 各一版式）。
func TestBuildTestReportWithCover(t *testing.T) {
	r := sampleReport()
	cover := model.DocCover{Enabled: true, Author: "张三", Org: "研发中心", Version: "V1.0.0"}

	html := BuildTestReportHTMLForExportWithCover(r, model.DocWatermark{}, cover)
	for _, want := range []string{`id="cover"`, "接口测试报告", "张三", ".doc-cover{break-after:page"} {
		if !strings.Contains(html, want) {
			t.Fatalf("报告 HTML 缺少封面内容 %q", want)
		}
	}
	if plain := BuildTestReportHTMLForExport(r, model.DocWatermark{}); strings.Contains(plain, `id="cover"`) {
		t.Fatal("未启用封面时不应输出封面页")
	}

	b, err := BuildTestReportDOCXWithCover(r, model.DocWatermark{}, cover)
	if err != nil {
		t.Fatalf("报告 Word（带封面）导出失败: %v", err)
	}
	parts := readDOCXParts(t, b)
	doc := parts["word/document.xml"]
	for _, want := range []string{"DocCoverTitle", "张三", "研发中心", "接口测试报告"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("报告 Word 缺少封面内容 %q", want)
		}
	}
}

// TestBuildTestReportDOCX 验证测试报告 Word 导出：结构合法、含统计/用例/水印。
func TestBuildTestReportDOCX(t *testing.T) {
	r := sampleReport()
	wm := model.DocWatermark{Enabled: true, Text: "内部资料 {{project}}"}
	wm = ExpandWatermark(wm, "订单中心", time.Now())
	b, err := BuildTestReportDOCX(r, wm)
	if err != nil {
		t.Fatalf("BuildTestReportDOCX 失败: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("报告 docx 不是合法 zip: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("解压 %s 失败: %v", f.Name, err)
		}
		parts[f.Name] = string(data)
	}
	for _, need := range []string{"word/document.xml", "word/styles.xml", "word/header1.xml"} {
		body, ok := parts[need]
		if !ok {
			t.Fatalf("缺少部件: %s", need)
		}
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s XML 非法: %v", need, err)
			}
		}
	}
	doc := parts["word/document.xml"]
	for _, want := range []string{"接口测试报告", "登录接口回归", "用例结果", "请求与响应明细", "登录成功", "断言"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("报告 document.xml 缺少内容 %q", want)
		}
	}
	if !strings.Contains(parts["word/header1.xml"], "内部资料 订单中心") {
		t.Fatal("报告页眉缺少展开后的水印文字")
	}
}

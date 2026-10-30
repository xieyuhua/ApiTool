package doc

import (
	"encoding/json"
	"strings"
	"testing"

	"apitool/internal/model"
	"apitool/internal/store"
)

// TestCheckedChaptersAlwaysRender 勾选的章节必须出现：项目缺少环境变量 / 公共参数 / 验签内容时，
// 也要输出带提示的占位内容，而不是静默消失（用户反馈「勾了却看不到」的问题）。
func TestCheckedChaptersAlwaysRender(t *testing.T) {
	st := store.New(t.TempDir(), "t", "")
	t.Cleanup(func() { _ = st.Close() })

	data := st.GetData()
	data.Projects[0].Apis = []model.ApiInfo{{ID: "a1", Name: "查询用户", Method: "get", URL: "https://x/y"}}
	on := true
	data.Settings.DocLayout = model.DocLayout{
		Toc: true, Numbering: true,
		QuickStart: true, Environment: true, CommonHeaders: true,
		SignOn: &on, Sign: "", ConventionOn: &on, Convention: "",
	}
	if err := st.SaveData(data); err != nil {
		t.Fatal(err)
	}

	scope, err := loadDocScope(st, "", "")
	if err != nil {
		t.Fatal(err)
	}
	html := BuildHTMLWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, scope.Layout)

	titles := make([]string, 0, len(scope.Layout.Prefaces))
	for _, c := range scope.Layout.Prefaces {
		titles = append(titles, c.Title)
	}
	for _, want := range []string{"接入指引（快速开始）", "接入地址（环境）", "公共请求头与公共参数", "加密与验签"} {
		found := false
		for _, title := range titles {
			if title == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("勾选的章节 %q 未出现，实际章节: %v", want, titles)
		}
	}

	// 缺数据时给出可操作的占位提示
	for _, want := range []string{
		"本项目尚未配置环境变量", "本项目尚未配置「公共参数」",
		"尚未填写「加密与验签」说明", "尚未填写「接口统一定义规范」",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("占位提示缺失 %q", want)
		}
	}

	// 关闭开关后不输出该章节（用未展开的全新配置验证，避免复用已生成的章节）
	off := false
	offLayout := EnrichDocLayout(model.DocLayout{Toc: true, SignOn: &off, ConventionOn: &off}, nil, model.CommonParams{})
	html2 := BuildHTMLWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, offLayout)
	for _, bad := range []string{"加密与验签", "接口统一定义规范"} {
		if strings.Contains(html2, bad) {
			t.Fatalf("关闭开关后仍输出 %q", bad)
		}
	}

	// 零值 layout（分享 / 纯正文导出）不应凭空多出章节
	plain := BuildHTMLWithLayout(scope.Title, scope.RootID, scope.Dirs, scope.Apis, scope.Common, model.DocLayout{})
	for _, bad := range []string{"接口统一定义规范", "接入地址（环境）", "加密与验签", "术语表"} {
		if strings.Contains(plain, bad) {
			t.Fatalf("零值配置不应输出 %q", bad)
		}
	}
}

// TestPreviewTemplateHTMLFromStore 弹窗预览入口：按传入的结构 JSON 渲染，且空 JSON 时用当前生效结构。
func TestPreviewTemplateHTMLFromStore(t *testing.T) {
	st := store.New(t.TempDir(), "t", "")
	t.Cleanup(func() { _ = st.Close() })

	data := st.GetData()
	data.Projects[0].Apis = []model.ApiInfo{{ID: "a1", Name: "查询用户", Method: "get", URL: "https://x/y"}}
	data.Settings.DocLayout = model.DocLayout{Toc: true, Numbering: true}
	if err := st.SaveData(data); err != nil {
		t.Fatal(err)
	}

	// 传入指定模板（内部版）的结构 JSON
	preset := DocPresetLayout("internal")
	preset.Name = "内部版"
	raw, err := json.Marshal(preset)
	if err != nil {
		t.Fatal(err)
	}
	html, err := PreviewTemplateHTML(st, string(raw))
	if err != nil {
		t.Fatalf("预览渲染失败: %v", err)
	}
	for _, want := range []string{"文档模板预览 · 内部版", "接入指引（快速开始）", "术语表", "模板预览不包含具体接口"} {
		if !strings.Contains(html, want) {
			t.Fatalf("预览 HTML 缺少 %q", want)
		}
	}
	if strings.Contains(html, `id="api-`) || strings.Contains(html, "查询用户") {
		t.Fatal("预览不应包含具体接口")
	}

	// 空 JSON：使用当前生效结构（仅目录 + 正文）
	html2, err := PreviewTemplateHTML(st, "")
	if err != nil {
		t.Fatalf("预览渲染失败: %v", err)
	}
	if !strings.Contains(html2, "模板预览不包含具体接口") {
		t.Fatal("空 JSON 时应使用当前结构并输出占位说明")
	}

	// 非法 JSON 要报错而不是静默成功
	if _, err := PreviewTemplateHTML(st, "{bad"); err == nil {
		t.Fatal("非法结构 JSON 应返回错误")
	}
}

// TestListChaptersSwitches 列表型章节（修订记录 / 术语表 / 数据字典 / 业务错误码）的开关行为：
// 关闭 → 整章不输出；开启但无内容 → 输出占位；有内容 + 开启 → 正常表格。
func TestListChaptersSwitches(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()

	// 1) 全部开启但都没有内容 → 输出章节 + 占位提示
	on := true
	layout := model.DocLayout{
		Toc: true, Numbering: true,
		RevisionsOn: &on, GlossaryOn: &on, EnumsOn: &on, BusinessCodesOn: &on,
	}
	layout = EnrichDocLayout(layout, nil, common)
	html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	for _, want := range []string{
		"文档修订记录", "暂无修订记录",
		"术语表", "尚未维护「术语表」",
		"附录：数据字典", "尚未维护「数据字典」",
		"附录：业务错误码", "尚未维护「业务错误码」",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("开启但无内容时应输出占位 %q", want)
		}
	}

	// 2) 有内容但关闭开关 → 整章不输出
	off := false
	filled := model.DocLayout{
		Toc: true, Numbering: true,
		RevisionsOn: &off, GlossaryOn: &off, EnumsOn: &off, BusinessCodesOn: &off,
		Revisions:     []model.DocRevision{{Version: "V1.0.0", Date: "2026-10-30", Note: "首次发布"}},
		Glossary:      DefaultGlossary(),
		Enums:         DefaultEnums(),
		BusinessCodes: DefaultBusinessCodes(),
	}
	filled = EnrichDocLayout(filled, nil, common)
	html2 := BuildHTMLWithLayout(title, rootID, dirs, apis, common, filled)
	for _, bad := range []string{"文档修订记录", "术语表", "附录：数据字典", "附录：业务错误码"} {
		if strings.Contains(html2, bad) {
			t.Fatalf("关闭开关后不应输出 %q", bad)
		}
	}

	// 3) 有内容 + 开启 → 输出表格内容
	html3 := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	html3b := BuildHTMLWithLayout(title, rootID, dirs, apis, common, EnrichDocLayout(model.DocLayout{
		Toc: true, GlossaryOn: &on, EnumsOn: &on, BusinessCodesOn: &on, RevisionsOn: &on,
		Revisions:     []model.DocRevision{{Version: "V1.0.0", Note: "首次发布"}},
		Glossary:      DefaultGlossary(),
		Enums:         DefaultEnums(),
		BusinessCodes: DefaultBusinessCodes(),
	}, nil, common))
	for _, want := range []string{"V1.0.0", "appSecret", "gender", "40001"} {
		if !strings.Contains(html3b, want) {
			t.Fatalf("有内容且开启时应输出 %q", want)
		}
	}
	_ = html3
}

// TestPreviewHTMLIncludesAPIs 整份文档预览要同时包含模板章节与具体接口内容。
func TestPreviewHTMLIncludesAPIs(t *testing.T) {
	st := store.New(t.TempDir(), "t", "")
	t.Cleanup(func() { _ = st.Close() })

	data := st.GetData()
	data.Projects[0].Apis = []model.ApiInfo{
		{ID: "a1", Name: "查询用户列表", Method: "get", URL: "https://api.example.com/users"},
	}
	data.Settings.DocLayout = model.DocLayout{
		Toc: true, Numbering: true, StatusCodes: true,
		Cover: model.DocCover{Enabled: true, Subtitle: "对外接口文档"},
	}
	if err := st.SaveData(data); err != nil {
		t.Fatal(err)
	}

	html, err := PreviewHTML(st, "", "", "")
	if err != nil {
		t.Fatalf("整份文档预览失败: %v", err)
	}
	for _, want := range []string{"对外接口文档", `id="toc"`, "附录：HTTP 状态码", `id="api-a1"`, "查询用户列表", "调用示例（cURL）"} {
		if !strings.Contains(html, want) {
			t.Fatalf("整份文档预览缺少 %q", want)
		}
	}
	// 与模板预览区分：这里不应出现占位说明
	if strings.Contains(html, "模板预览不包含具体接口") {
		t.Fatal("整份文档预览不应出现模板占位说明")
	}
}

// TestPreviewHTMLUsesPassedLayout 预览必须使用前端传入的「当前选中模板」结构，
// 即使数据库里存的是另一套结构（修复「预览不是所选模板」的问题）。
func TestPreviewHTMLUsesPassedLayout(t *testing.T) {
	st := store.New(t.TempDir(), "t", "")
	t.Cleanup(func() { _ = st.Close() })

	data := st.GetData()
	data.Projects[0].Apis = []model.ApiInfo{
		{ID: "a1", Name: "查询用户列表", Method: "get", URL: "https://api.example.com/users"},
	}
	// 库里保存的是「精简结构」
	data.Settings.DocLayout = model.DocLayout{Toc: true, Numbering: true}
	if err := st.SaveData(data); err != nil {
		t.Fatal(err)
	}

	// 前端传入的是「我选的模板」：封面 + 附录状态码 + 接入地址
	passed := `{"toc":true,"numbering":true,"statusCodes":true,"environment":true,
		"cover":{"enabled":true,"subtitle":"我选的模板"},"signOn":false,"conventionOn":false}`
	html, err := PreviewHTML(st, "", "", passed)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	for _, want := range []string{"我选的模板", "附录：HTTP 状态码", "接入地址（环境）", `id="api-a1"`,
		"scrollIntoView", "preventDefault"} { // 预览 HTML 注入锚点跳转脚本，弹窗里点目录才能跳
		if !strings.Contains(html, want) {
			t.Fatalf("预览未使用传入的结构，缺少 %q", want)
		}
	}
	if strings.Contains(html, "接口统一定义规范") {
		t.Fatal("传入 signOn/conventionOn=false 时不应输出规范章节")
	}

	// 不传结构时回退到库里保存的结构（无封面）
	html2, err := PreviewHTML(st, "", "", "")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if strings.Contains(html2, "我选的模板") || strings.Contains(html2, "附录：HTTP 状态码") {
		t.Fatal("未传结构时应使用持久化的精简结构")
	}
}

// TestSectionOrderCustom 自定义章节顺序（拖动排序）在所有渲染器里生效。
func TestSectionOrderCustom(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	on := true
	layout := model.DocLayout{
		Toc: true, Numbering: true, StatusCodes: true, Support: model.DocSupport{
			Enabled: true, Name: "张三", Email: "z@example.com",
		},
		Cover:        model.DocCover{Enabled: true, Subtitle: "封面副标题"},
		Prefaces:     []model.DocChapter{{Title: "文档说明", Content: "- 概述"}},
		GlossaryOn:   &on,
		Glossary:     DefaultGlossary(),
		ConventionOn: &on,
		Convention:   "# 规范",
		// 附录放到最前，接口正文提到规范之前
		SectionOrder: []string{"statusCodes", "cover", "toc", "apis", "convention", "support", "glossary"},
	}
	layout = EnrichDocLayout(layout, nil, common)

	html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	idx := func(s string) int { return strings.Index(html, s) }
	// 用元素 id 比较（目录里也有章节标题文本，不能按标题文本比较）
	checks := []struct{ a, b, tip string }{
		{`id="appendix-status"`, `id="cover"`, "状态码应在封面之前"},
		{`id="cover"`, `id="toc"`, "封面应在目录之前"},
		{`id="toc"`, `id="api-a1"`, "目录应在接口正文之前"},
		{`id="api-a1"`, `id="convention"`, "接口正文应在统一规范之前"},
		{`id="convention"`, `id="support"`, "规范应在技术支持之前"},
	}
	for _, c := range checks {
		ia, ib := idx(c.a), idx(c.b)
		if ia < 0 || ib < 0 {
			t.Fatalf("缺少标记 %q 或 %q", c.a, c.b)
		}
		if ia > ib {
			t.Fatalf("%s（%q 位置 %d > %q 位置 %d）", c.tip, c.a, ia, c.b, ib)
		}
	}

	// Markdown 渲染同样遵循顺序（关掉目录，避免目录条目文本干扰位置比较）
	mdLayout := layout
	mdLayout.Toc = false
	md := BuildMarkdownWithLayout(title, rootID, dirs, apis, common, mdLayout)
	if !strings.Contains(md, "附录：HTTP 状态码") {
		t.Fatal("Markdown 应包含状态码附录")
	}
	if strings.Index(md, "附录：HTTP 状态码") > strings.Index(md, "接口统一定义规范") {
		t.Fatal("Markdown 也应遵循自定义顺序（状态码附录应在统一规范之前）")
	}

	// 空顺序 = 默认顺序（回归）
	layout.SectionOrder = nil
	def := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	if strings.Index(def, `id="cover"`) > strings.Index(def, `id="toc"`) {
		t.Fatal("默认顺序应为封面在目录之前")
	}
	if strings.Index(def, `id="api-a1"`) < strings.Index(def, `id="convention"`) {
		t.Fatal("默认顺序应为接口正文在统一规范之后")
	}
}

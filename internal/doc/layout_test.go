package doc

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"apitool/internal/model"
	"apitool/internal/store"
)

// fixedTime 让章节顺序断言不受当前时间影响。
func fixedTime() time.Time { return time.Date(2026, 10, 27, 22, 0, 0, 0, time.Local) }

// sampleLayout 构造一份完整的企业文档排版配置（封面 + 修订记录 + 目录 + 公共描述 + 规范 + 附录）。
func sampleLayout() model.DocLayout {
	return model.DocLayout{
		Toc: true,
		Cover: model.DocCover{
			Enabled: true, Subtitle: "内部技术文档 · 禁止外传",
			Author: "张三", Org: "研发中心", Version: "V1.2.0", Date: "2026-10-27",
		},
		Prefaces: []model.DocChapter{{
			Title: "文档说明",
			Content: "#### 适用范围\n- 订单中心相关接口\n- 需要 `token` 鉴权的场景\n\n" +
				"| 项目 | 说明 |\n|---|---|\n| 环境 | 测试 / 生产 |\n| 版本 | V1.2.0 |\n\n" +
				"> 本文档随版本发布同步更新。\n\n```json\n{\"code\":0}\n```\n",
		}},
		Convention:  DefaultConventionTemplate(),
		StatusCodes: true,
		Revisions: []model.DocRevision{
			{Version: "V1.2.0", Date: "2026-10-27", Author: "张三", Note: "新增导出水印与文档结构"},
			{Version: "V1.1.0", Date: "2026-09-01", Author: "李四", Note: "补充错误码说明"},
		},
	}
}

func TestBuildDocSections(t *testing.T) {
	sections := buildDocSections("接口文档", sampleLayout(), fixedTime())
	kinds := make([]docSectionKind, 0, len(sections))
	for _, s := range sections {
		kinds = append(kinds, s.Kind)
	}
	want := []docSectionKind{
		sectionCover, sectionRevisions, sectionTOC, sectionPreface,
		sectionConvention, sectionAPIs, sectionAppendix,
	}
	if len(kinds) != len(want) {
		t.Fatalf("章节数量 = %d, want %d（%v）", len(kinds), len(want), kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("第 %d 个章节 = %s, want %s（完整顺序 %v）", i+1, kinds[i], want[i], kinds)
		}
	}
	// 未配置时只输出接口正文
	plain := buildDocSections("接口文档", model.DocLayout{}, fixedTime())
	if len(plain) != 1 || plain[0].Kind != sectionAPIs {
		t.Fatalf("空配置应只有接口正文，实际: %+v", plain)
	}
}

func TestBuildDOCXWithLayout(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	parts := readDOCXParts(t, mustDOCX(t, title, rootID, dirs, apis, common, sampleLayout()))

	for _, need := range []string{"word/settings.xml", "word/document.xml", "word/header1.xml"} {
		if _, ok := parts[need]; !ok {
			t.Fatalf("缺少部件 %s", need)
		}
	}
	if !strings.Contains(parts["word/settings.xml"], "updateFields") {
		t.Fatal("settings.xml 缺少 updateFields（Word 打开时应自动更新目录域）")
	}
	doc := parts["word/document.xml"]
	for _, want := range []string{
		"接口文档", "内部技术文档 · 禁止外传", "张三", "研发中心", "V1.2.0",
		"文档修订记录", "目录", `TOC \o "1-3"`, "文档说明", "适用范围",
		"接口统一定义规范", "命名与路径", "附录：HTTP 状态码", "429",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document.xml 缺少内容 %q", want)
		}
	}
	if !strings.Contains(doc, "DocCoverTitle") {
		t.Fatal("封面未使用 DocCoverTitle 样式")
	}
}

func TestBuildHTMLWithLayout(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	h := BuildHTMLWithLayout(title, rootID, dirs, apis, common, sampleLayout())
	for _, want := range []string{
		`id="cover"`, "内部技术文档 · 禁止外传",
		`id="revisions"`, "文档修订记录",
		`id="toc"`, `href="#apis"`, `href="#convention"`, `href="#appendix-status"`,
		"文档说明", "接口统一定义规范", "附录：HTTP 状态码", "429",
	} {
		if !strings.Contains(h, want) {
			t.Fatalf("HTML 缺少内容 %q", want)
		}
	}
	if !strings.Contains(h, `.doc-cover{break-after:page`) {
		t.Fatal("封面缺少打印分页样式")
	}
	// 未配置排版时不应出现封面与目录区
	plain := BuildHTML(title, rootID, dirs, apis, common)
	if strings.Contains(plain, `id="cover"`) || strings.Contains(plain, `id="toc"`) {
		t.Fatal("未配置排版时不应输出封面/目录章节")
	}
}

func TestBuildMarkdownWithLayout(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	md := buildMarkdown(title, rootID, dirs, apis, common, sampleLayout())
	for _, want := range []string{
		"# 接口文档", "内部技术文档 · 禁止外传", "- 作者：张三",
		"## 文档修订记录", "## 目录", "## 文档说明", "## 接口统一定义规范", "## 附录：HTTP 状态码",
		"429", "查询用户列表",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺少内容 %q", want)
		}
	}
}

func TestParseMiniMarkdown(t *testing.T) {
	blocks := parseMiniMarkdown(sampleLayout().Prefaces[0].Content)
	kinds := map[miniBlockKind]int{}
	for _, b := range blocks {
		kinds[b.Kind]++
	}
	if kinds[miniHeading] == 0 || kinds[miniBulletList] == 0 || kinds[miniTable] == 0 ||
		kinds[miniQuote] == 0 || kinds[miniCode] == 0 {
		t.Fatalf("块解析不全: %+v", kinds)
	}
	for _, b := range blocks {
		if b.Kind == miniTable && (len(b.Header) != 2 || len(b.Rows) != 2) {
			t.Fatalf("表格解析错误: %+v", b)
		}
	}
}

// ---------------- 章节编号 ----------------

func TestDocNumbering(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	layout := sampleLayout()
	layout.Numbering = true
	sections := buildDocSections(title, layout, fixedTime())
	num := buildDocNumbering(sections, dirs, apis, rootID, true)

	// 章节编号：修订记录 1、文档说明 2、规范 3、接口正文 4、附录 5（封面与目录不编号）
	cases := []struct{ id, title, want string }{
		{"revisions", "文档修订记录", "1 文档修订记录"},
		{"preface-1", "文档说明", "2 文档说明"},
		{"convention", "接口统一定义规范", "3 接口统一定义规范"},
		{"apis", labelAPISection, "4 接口正文"},
		{"appendix-status", "附录：HTTP 状态码", "5 附录：HTTP 状态码"},
	}
	for _, c := range cases {
		if got := num.section(c.id, c.title); got != c.want {
			t.Fatalf("章节编号 %s = %q, want %q", c.id, got, c.want)
		}
	}
	if got := num.section("toc", "目录"); got != "目录" {
		t.Fatalf("目录页不应编号: %q", got)
	}
	// 接口树编号：用户中心 4.1 → 上传头像 4.1.1 → 用户管理 4.1.2 → 查询用户列表 4.1.2.1
	tree := []struct{ kind, id, name, want string }{
		{"dir", "d1", "用户中心", "4.1 用户中心"},
		{"api", "a2", "上传头像", "4.1.1 上传头像"},
		{"dir", "d2", "用户管理", "4.1.2 用户管理"},
		{"api", "a1", "查询用户列表", "4.1.2.1 查询用户列表"},
	}
	for _, c := range tree {
		var got string
		if c.kind == "dir" {
			got = num.dir(c.id, c.name)
		} else {
			got = num.api(c.id, c.name)
		}
		if got != c.want {
			t.Fatalf("%s 编号 %s = %q, want %q", c.kind, c.id, got, c.want)
		}
	}
	// 关闭编号时全部返回原标题
	off := buildDocNumbering(sections, dirs, apis, rootID, false)
	if off.section("apis", labelAPISection) != labelAPISection || off.dir("d1", "用户中心") != "用户中心" {
		t.Fatal("关闭编号后仍带编号")
	}

	// 三种渲染器都应输出编号
	html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	for _, want := range []string{"1 文档修订记录", "4 接口正文", "4.1 用户中心", "4.1.2.1 查询用户列表"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML 缺少编号 %q", want)
		}
	}
	doc := readDOCXParts(t, mustDOCX(t, title, rootID, dirs, apis, common, layout))["word/document.xml"]
	for _, want := range []string{"1 文档修订记录", "4 接口正文", "4.1.2 用户管理"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("DOCX 缺少编号 %q", want)
		}
	}
	md := buildMarkdown(title, rootID, dirs, apis, common, layout)
	for _, want := range []string{"## 1 文档修订记录", "## 4 接口正文", "### 4.1.2.1 查询用户列表"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺少编号 %q", want)
		}
	}
}

func TestDocPresets(t *testing.T) {
	list := DocPresetList()
	if len(list) != 8 {
		t.Fatalf("预置模板数量 = %d, want 8", len(list))
	}
	// 每个模板都要有标签与描述，且 key 唯一
	seen := map[string]bool{}
	for _, p := range list {
		if p.Key == "" || p.Label == "" || p.Desc == "" || p.Tag == "" || len(p.Chips) == 0 {
			t.Fatalf("模板字段不完整: %+v", p)
		}
		if seen[p.Key] {
			t.Fatalf("模板 key 重复: %s", p.Key)
		}
		seen[p.Key] = true
	}
	// 每套模板至少要能渲染出目录 + 接口正文
	for _, p := range list {
		layout := DocPresetLayout(p.Key)
		if !layout.Toc || !layout.Numbering {
			t.Fatalf("模板 %s 未开启目录/编号: %+v", p.Key, layout)
		}
	}
	// 差异化检查：REST 版无封面，微服务版有服务约定章节
	if DocPresetLayout("rest").Cover.Enabled {
		t.Fatal("标准 RESTful 版不应带封面")
	}
	micro := DocPresetLayout("micro")
	if !micro.Cover.Enabled || len(micro.Prefaces) == 0 || !micro.StatusCodes {
		t.Fatalf("微服务版配置不完整: %+v", micro)
	}
	if review := DocPresetLayout("review"); !review.Cover.Enabled || len(review.Revisions) == 0 {
		t.Fatalf("评审版应带封面与修订记录: %+v", review)
	}
	// 开放平台版：RSA2 验签 + 业务错误码；金融合规版：SM2 验签 + 修订记录
	open := DocPresetLayout("open")
	if !open.Cover.Enabled || !strings.Contains(open.Sign, "SHA256withRSA") || len(open.BusinessCodes) == 0 {
		t.Fatalf("开放平台版配置不符合预期: %+v", open)
	}
	gov := DocPresetLayout("gov")
	if !strings.Contains(gov.Sign, "SM3") || len(gov.Revisions) == 0 || len(gov.BusinessCodes) == 0 {
		t.Fatalf("金融合规版配置不符合预期: %+v", gov)
	}
	// 多数模板应内置业务错误码
	if len(DocPresetLayout("internal").BusinessCodes) == 0 || len(DocPresetLayout("minimal").BusinessCodes) != 0 {
		t.Fatal("业务错误码开关与预置配置不一致")
	}
	internal := DocPresetLayout("internal")
	if !internal.Cover.Enabled || internal.Convention == "" || !internal.StatusCodes ||
		len(internal.Prefaces) == 0 || len(internal.Revisions) == 0 || !internal.Toc || !internal.Numbering {
		t.Fatalf("内部版模板配置不完整: %+v", internal)
	}
	external := DocPresetLayout("external")
	if !external.Cover.Enabled || len(external.Revisions) != 0 || external.Convention == "" {
		t.Fatalf("对外版模板配置不符合预期: %+v", external)
	}
	minimal := DocPresetLayout("unknown")
	if minimal.Cover.Enabled || minimal.Convention != "" || minimal.StatusCodes || !minimal.Toc {
		t.Fatalf("未知模板应回退为精简版: %+v", minimal)
	}
}

// TestDocPresetsRender 每套预置模板都要能正常渲染三种格式，且章节与配置一致。
func TestDocPresetsRender(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	for _, p := range DocPresetList() {
		layout := DocPresetLayout(p.Key)

		html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
		if !strings.Contains(html, labelAPISection) {
			t.Fatalf("[%s] HTML 缺少接口正文", p.Key)
		}
		if layout.Cover.Enabled != strings.Contains(html, `id="cover"`) {
			t.Fatalf("[%s] 封面开关与输出不一致", p.Key)
		}
		if layout.Toc != strings.Contains(html, `id="toc"`) {
			t.Fatalf("[%s] 目录开关与输出不一致", p.Key)
		}
		for _, ch := range layout.Prefaces {
			if !strings.Contains(html, ch.Title) {
				t.Fatalf("[%s] HTML 缺少章节 %q", p.Key, ch.Title)
			}
		}
		if layout.StatusCodes && !strings.Contains(html, "附录：HTTP 状态码") {
			t.Fatalf("[%s] 缺少状态码附录", p.Key)
		}

		md := buildMarkdown(title, rootID, dirs, apis, common, layout)
		if !strings.Contains(md, "## ") {
			t.Fatalf("[%s] Markdown 结构缺失", p.Key)
		}

		parts := readDOCXParts(t, mustDOCX(t, title, rootID, dirs, apis, common, layout))
		if !strings.Contains(parts["word/document.xml"], "查询用户列表") {
			t.Fatalf("[%s] DOCX 缺少接口内容", p.Key)
		}
		if layout.Toc && !strings.Contains(parts["word/document.xml"], "TOC ") {
			t.Fatalf("[%s] DOCX 缺少目录域", p.Key)
		}
	}
}

// TestEffectiveDocLayout 项目级覆盖优先于全局配置。
func TestEffectiveDocLayout(t *testing.T) {
	data := model.AppData{Settings: model.Settings{DocLayout: model.DocLayout{Toc: true}}}
	proj := model.Project{ID: "p1", Name: "订单中心"}
	if got := EffectiveDocLayout(data, proj); !got.Toc || got.Numbering || got.StatusCodes {
		t.Fatalf("无项目覆盖时应使用全局配置: %+v", got)
	}
	data.Settings.ProjectDocLayouts = map[string]model.DocLayout{
		proj.ID: {Numbering: true, StatusCodes: true},
	}
	got := EffectiveDocLayout(data, proj)
	if !got.Numbering || !got.StatusCodes || got.Toc {
		t.Fatalf("项目覆盖未生效: %+v", got)
	}
	// 其他项目仍使用全局配置
	if other := EffectiveDocLayout(data, model.Project{ID: "other"}); !other.Toc || other.StatusCodes {
		t.Fatalf("其他项目不应受影响: %+v", other)
	}
}

// TestLoadDocScopeUsesProjectLayout 导出范围解析时，项目级文档结构优先。
func TestLoadDocScopeUsesProjectLayout(t *testing.T) {
	st := store.New(t.TempDir(), "test", "")
	t.Cleanup(func() { _ = st.Close() })

	_, _, _, apis, _ := sampleScope()
	data := st.GetData()
	data.Projects[0].Apis = apis
	data.Settings.DocLayout = model.DocLayout{Toc: true}
	data.Settings.ProjectDocLayouts = map[string]model.DocLayout{
		data.Projects[0].ID: {Numbering: true, StatusCodes: true},
	}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存测试数据失败: %v", err)
	}

	scope, err := loadDocScope(st, "", "")
	if err != nil {
		t.Fatalf("loadDocScope 失败: %v", err)
	}
	if !scope.Layout.Numbering || !scope.Layout.StatusCodes || scope.Layout.Toc {
		t.Fatalf("导出未使用项目级结构: %+v", scope.Layout)
	}

	// 清除覆盖后回落到全局配置
	data = st.GetData()
	data.Settings.ProjectDocLayouts = nil
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存测试数据失败: %v", err)
	}
	scope, err = loadDocScope(st, "", "")
	if err != nil {
		t.Fatalf("loadDocScope 失败: %v", err)
	}
	if !scope.Layout.Toc || scope.Layout.StatusCodes {
		t.Fatalf("清除覆盖后未回落全局: %+v", scope.Layout)
	}
}

// sampleEnvironments 测试用的项目环境列表（含测试 / 预发 / 生产）。
func sampleEnvironments() []model.Environment {
	return []model.Environment{
		{ID: "e1", Name: "测试环境", Vars: []model.EnvVar{
			{Key: "baseUrl", Value: "https://test-api.example.com", Enabled: true},
			{Key: "appSecret", Value: "should-not-appear", Enabled: true},
		}},
		{ID: "e2", Name: "预发环境", Vars: []model.EnvVar{
			{Key: "host", Value: "https://pre-api.example.com", Enabled: true},
		}},
		{ID: "e3", Name: "正式环境", Vars: []model.EnvVar{
			{Key: "domain", Value: "https://api.example.com", Enabled: true},
		}},
	}
}

// TestEnrichDocLayout 验证「接入地址 / 公共请求头 / 加密验签」章节按开关生成。
func TestEnrichDocLayout(t *testing.T) {
	_, _, _, _, common := sampleScope()
	envs := sampleEnvironments()

	// 全部关闭：不追加任何章节
	off := EnrichDocLayout(model.DocLayout{Toc: true}, envs, common)
	if len(off.Prefaces) != 0 {
		t.Fatalf("关闭时不应追加章节: %+v", off.Prefaces)
	}

	layout := EnrichDocLayout(model.DocLayout{
		Toc: true, Environment: true, CommonHeaders: true, Sign: DefaultSignTemplate(),
	}, envs, common)
	if len(layout.Prefaces) != 3 {
		t.Fatalf("应追加 3 个章节, got %d: %+v", len(layout.Prefaces), layout.Prefaces)
	}
	if layout.Prefaces[0].Title != "接入地址（环境）" || layout.Prefaces[1].Title != "公共请求头与公共参数" ||
		layout.Prefaces[2].Title != "加密与验签" {
		t.Fatalf("章节顺序不符合预期: %+v", layout.Prefaces)
	}

	envText := layout.Prefaces[0].Content
	for _, want := range []string{"测试环境", "https://test-api.example.com", "预发环境", "https://pre-api.example.com",
		"正式环境", "https://api.example.com", "生产环境，谨慎操作"} {
		if !strings.Contains(envText, want) {
			t.Fatalf("接入地址章节缺少 %q:\n%s", want, envText)
		}
	}
	if strings.Contains(envText, "should-not-appear") {
		t.Fatal("接入地址章节不应输出 appSecret 等敏感变量")
	}

	// 公共请求头 / 公共查询参数：来自项目的公共参数配置
	layout2 := EnrichDocLayout(model.DocLayout{CommonHeaders: true}, nil, model.CommonParams{
		Headers: []model.KV{{Key: "Authorization", Value: "Bearer {{token}}"}, {Key: "X-App-Id", Value: "demo"}},
		Query:   []model.KV{{Key: "appId", Value: "demo"}},
	})
	headText := layout2.Prefaces[0].Content
	for _, want := range []string{"公共请求头", "`Authorization`", "Bearer {{token}}", "公共查询参数", "`appId`"} {
		if !strings.Contains(headText, want) {
			t.Fatalf("公共请求头章节缺少 %q:\n%s", want, headText)
		}
	}

	signText := layout.Prefaces[2].Content
	for _, want := range []string{"ASCII", "MD5", "32 位大写", "&key=appSecret", "X-Sign", "X-Timestamp", "X-Nonce",
		"示例代码", "10002", "签名不匹配", "nonce 重复", "```java", "```go", "```javascript"} {
		if !strings.Contains(signText, want) {
			t.Fatalf("加密验签章节缺少 %q", want)
		}
	}
}

// TestDocExportIsMonochrome 导出的文档正文与目录只能是黑色文字（只有黑白灰）。
func TestDocExportIsMonochrome(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	layout := EnrichDocLayout(sampleLayout(), sampleEnvironments(), common)
	layout.Numbering = true

	html := BuildHTMLForExportWithLayout(title, rootID, dirs, apis, common, model.DocWatermark{}, layout)
	for _, bad := range []string{"#165dff", "#0fc6c2", "#ff7d00", "#f53f3f", "#722ed1", "#1d2129"} {
		if strings.Contains(html, bad) {
			t.Fatalf("导出 HTML 仍含彩色 %s", bad)
		}
	}
	if !strings.Contains(html, "color:#000") {
		t.Fatal("导出 HTML 未使用黑色文字")
	}

	// Word：所有文字颜色必须是 000000（表格边框等仍可用浅灰）
	parts := readDOCXParts(t, mustDOCX(t, title, rootID, dirs, apis, common, layout))
	doc := parts["word/document.xml"]
	for _, bad := range []string{`w:color w:val="1F2329"`, `w:color w:val="165DFF"`, `w:color w:val="4E5969"`} {
		if strings.Contains(doc, bad) {
			t.Fatalf("Word 仍含彩色文字 %s", bad)
		}
	}
	styles := parts["word/styles.xml"]
	for _, bad := range []string{"1F2329", "165DFF", "4E5969", "86909C"} {
		if strings.Contains(styles, bad) {
			t.Fatalf("Word 样式仍含彩色 %s", bad)
		}
	}
}

// TestSignTemplates 预置验签模板：清单可用、内容包含规则/示例/代码/错误码。
func TestSignTemplates(t *testing.T) {
	list := SignTemplateList()
	if len(list) == 0 {
		t.Fatal("验签模板清单为空")
	}
	seen := map[string]bool{}
	for _, tpl := range list {
		if tpl.Key == "" || tpl.Label == "" || tpl.Desc == "" || tpl.Content == "" {
			t.Fatalf("验签模板字段不完整: %+v", tpl)
		}
		if seen[tpl.Key] {
			t.Fatalf("验签模板 key 重复: %s", tpl.Key)
		}
		seen[tpl.Key] = true
	}
	if list[0].Content != DefaultSignTemplate() {
		t.Fatal("默认验签模板应为清单首项")
	}
	if strings.Contains(DefaultSignTemplate(), "§") {
		t.Fatal("验签模板中的占位符未还原为反引号")
	}
	// 每套模板都要带 Java / Go / PHP 示例代码
	for _, tpl := range list {
		for _, want := range []string{"```java", "```go", "```php", "<?php"} {
			if !strings.Contains(tpl.Content, want) {
				t.Fatalf("模板 %s 缺少 %s 示例", tpl.Key, want)
			}
		}
	}
	// PHP 示例的关键细节：排序必须用 SORT_STRING、JWT 用 firebase/php-jwt
	if !strings.Contains(templateMD5Sorted, "ksort($params, SORT_STRING)") {
		t.Fatal("MD5 模板缺少 PHP 排序示例")
	}
	if !strings.Contains(templateJWT, "Firebase\\JWT\\JWT") {
		t.Fatal("JWT 模板缺少 PHP 示例")
	}
}

// TestBusinessCodesAppendix 业务错误码附录在三种格式中都要正确输出。
func TestBusinessCodesAppendix(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	layout := model.DocLayout{Toc: true, BusinessCodes: DefaultBusinessCodes()}
	layout.Numbering = true

	html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	for _, want := range []string{"附录：业务错误码", "10001", "参数校验失败", "42901", "请求过于频繁"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML 业务错误码附录缺少 %q", want)
		}
	}
	if !strings.Contains(html, `id="appendix-codes"`) {
		t.Fatal("HTML 业务错误码附录缺少锚点")
	}

	md := buildMarkdown(title, rootID, dirs, apis, common, layout)
	if !strings.Contains(md, "附录：业务错误码") || !strings.Contains(md, "| `40001` | 参数校验失败 |") {
		t.Fatalf("Markdown 业务错误码附录缺失:\n%s", md)
	}

	parts := readDOCXParts(t, mustDOCX(t, title, rootID, dirs, apis, common, layout))
	doc := parts["word/document.xml"]
	for _, want := range []string{"附录：业务错误码", "参数校验失败", "请求过于频繁"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Word 业务错误码附录缺少 %q", want)
		}
	}

	// 空清单时不应出现该章节
	empty := BuildHTMLWithLayout(title, rootID, dirs, apis, common, model.DocLayout{Toc: true})
	if strings.Contains(empty, "附录：业务错误码") {
		t.Fatal("未配置业务错误码时不应输出附录")
	}
}

// TestDocTOCWithoutMethod 目录只保留「编号 + 接口名」，不出现请求方式与地址（企业文档惯例）。
func TestDocTOCWithoutMethod(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	layout := model.DocLayout{Toc: true, Numbering: true}

	html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	if strings.Contains(html, `class="tm tm-`) {
		t.Fatal("目录项不应包含请求方式徽章")
	}
	// 正文接口标题上的方法标识仍需保留
	if !strings.Contains(html, `class="method m-`) {
		t.Fatal("正文接口标题应保留请求方法标识")
	}
	// 正文中的目录块里也不应出现方法文本
	start := strings.Index(html, `id="toc"`)
	if start < 0 {
		t.Fatal("缺少正文目录块")
	}
	end := strings.Index(html[start:], "</section>")
	if end < 0 {
		t.Fatal("目录块结构异常")
	}
	block := html[start : start+end]
	for _, bad := range []string{">GET<", ">POST<", "tm-get", "tm-post"} {
		if strings.Contains(block, bad) {
			t.Fatalf("目录块仍含请求方式 %q", bad)
		}
	}

	md := buildMarkdown(title, rootID, dirs, apis, common, layout)
	tocStart := strings.Index(md, "## 目录")
	if tocStart < 0 {
		t.Fatal("Markdown 缺少目录章节")
	}
	tocEnd := strings.Index(md[tocStart:], "\n## ")
	if tocEnd < 0 {
		tocEnd = len(md) - tocStart
	}
	toc := md[tocStart : tocStart+tocEnd]
	for _, bad := range []string{"（GET ", "（POST ", "（PUT ", "https://"} {
		if strings.Contains(toc, bad) {
			t.Fatalf("Markdown 目录仍含 %q", bad)
		}
	}
	if !strings.Contains(toc, "1 接口正文") || !strings.Contains(toc, "1.1 用户中心") || !strings.Contains(toc, "查询用户列表") {
		t.Fatalf("Markdown 目录缺少编号或接口名:\n%s", toc)
	}
}

// TestEnterpriseChapters 接入指引 / 术语表 / 数据字典 / 技术支持 与接口级 cURL 示例。
func TestEnterpriseChapters(t *testing.T) {
	title, rootID, dirs, apis, common := sampleScope()
	layout := EnrichDocLayout(model.DocLayout{
		Toc: true, Numbering: true, QuickStart: true,
		Glossary: DefaultGlossary(), Enums: DefaultEnums(),
		Support: model.DocSupport{Enabled: true, Name: "接口对接支持", Email: "api@example.com", SLA: "工作日 2 小时内响应"},
	}, sampleEnvironments(), common)

	html := BuildHTMLWithLayout(title, rootID, dirs, apis, common, layout)
	for _, want := range []string{
		"接入指引（快速开始）", "术语表", "附录：数据字典", "技术支持与联系",
		"appSecret", "gender", "api@example.com", "工作日 2 小时内响应",
		"调用示例（cURL）", "curl -X GET", "curl -X POST",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML 缺少 %q", want)
		}
	}
	// 目录也要包含这些章节
	for _, want := range []string{"接入指引（快速开始）", "术语表", "附录：数据字典", "技术支持与联系"} {
		if !strings.Contains(html, want) {
			t.Fatalf("目录缺少 %q", want)
		}
	}

	md := buildMarkdown(title, rootID, dirs, apis, common, layout)
	for _, want := range []string{"术语表", "附录：数据字典", "技术支持与联系", "```bash", "curl -X GET"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺少 %q", want)
		}
	}

	parts := readDOCXParts(t, mustDOCX(t, title, rootID, dirs, apis, common, layout))
	doc := parts["word/document.xml"]
	for _, want := range []string{"术语表", "附录：数据字典", "技术支持与联系", "curl -X GET"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Word 缺少 %q", want)
		}
	}

	// 关闭开关后不输出
	off := BuildHTMLWithLayout(title, rootID, dirs, apis, common, model.DocLayout{Toc: true})
	for _, bad := range []string{"接入指引（快速开始）", "附录：数据字典", "技术支持与联系"} {
		if strings.Contains(off, bad) {
			t.Fatalf("关闭开关后仍输出 %q", bad)
		}
	}
}

// TestCurlSample 依据接口定义生成可复制的 cURL。
func TestCurlSample(t *testing.T) {
	api := model.ApiInfo{
		ID: "a1", Name: "创建用户", Method: "post", URL: "https://api.example.com/users",
		BodyType: "json", Body: `{"name":"张三"}`,
		Headers: []model.KV{{Key: "X-Tenant-Id", Value: "t1"}},
		Query:   []model.KV{{Key: "debug", Value: "1"}},
	}
	common := model.CommonParams{
		Headers: []model.KV{{Key: "X-App-Id", Value: ""}},
		Query:   []model.KV{{Key: "appId", Value: "A001"}},
	}
	got := curlSample(api, common)
	for _, want := range []string{
		"curl -X POST 'https://api.example.com/users?debug=1&appId=A001'",
		"-H 'Content-Type: application/json'", "-H 'X-Tenant-Id: t1'", "-H 'X-App-Id: {{ X-App-Id }}'",
		`-d '{"name":"张三"}'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cURL 缺少 %q:\n%s", want, got)
		}
	}
	// 无 URL 的接口不生成示例
	if curlSample(model.ApiInfo{Method: "get"}, model.CommonParams{}) != "" {
		t.Fatal("空地址不应生成 cURL")
	}
}

// TestTemplatePreviewHTML 模板预览只渲染章节结构，不夹带任何具体接口。
func TestTemplatePreviewHTML(t *testing.T) {
	title, _, _, _, common := sampleScope()
	layout := EnrichDocLayout(DocPresetLayout("internal"), sampleEnvironments(), common)

	html := BuildTemplatePreviewHTML(title, common, model.DocWatermark{}, layout)
	for _, want := range []string{
		"文档修订记录", "目录", "接入指引（快速开始）", "接入地址（环境）",
		"公共请求头与公共参数", "加密与验签", "术语表", "接口统一定义规范", "附录：HTTP 状态码",
		"模板预览不包含具体接口",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("模板预览缺少章节 %q", want)
		}
	}
	// 不得出现任何具体接口：接口锚点、接口名与目录节点都不应渲染
	for _, bad := range []string{`id="api-`, "查询用户列表", "上传头像", `id="dir-`, `class="toc-api"`} {
		if strings.Contains(html, bad) {
			t.Fatalf("模板预览不应包含具体接口内容 %q", bad)
		}
	}
}

// ---------------- 测试辅助 ----------------

func mustDOCX(t *testing.T, title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, layout model.DocLayout) []byte {
	t.Helper()
	b, err := BuildDOCXWithLayout(title, rootID, dirs, apis, common, model.DocWatermark{}, layout)
	if err != nil {
		t.Fatalf("BuildDOCXWithLayout 失败: %v", err)
	}
	return b
}

func readDOCXParts(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("docx 不是合法 zip: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("解压 %s 失败: %v", f.Name, err)
		}
		parts[f.Name] = string(body)
	}
	return parts
}

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
	if len(list) != 6 {
		t.Fatalf("预置模板数量 = %d, want 6", len(list))
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

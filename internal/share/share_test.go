package share

import (
	"strings"
	"testing"

	"apitool/internal/model"
	"apitool/internal/store"
)

// newTestStore 构造一个含单接口项目、可写设置的临时存储。
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st := store.New(t.TempDir(), "test", "")
	t.Cleanup(func() { _ = st.Close() })
	data := st.GetData()
	data.Projects[0].Apis = []model.ApiInfo{
		{ID: "a1", Name: "查询用户列表", Method: "get", URL: "https://api.example.com/users"},
	}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("准备测试数据失败: %v", err)
	}
	return st
}

// TestBuildHTMLByAPIShareOptions 验证分享文档默认是简洁版，开启开关后套用文档结构与水印。
func TestBuildHTMLByAPIShareOptions(t *testing.T) {
	st := newTestStore(t)

	plain, err := BuildHTMLByAPI(st, "", "a1")
	if err != nil {
		t.Fatalf("默认分享 HTML 构建失败: %v", err)
	}
	if strings.Contains(plain, `id="cover"`) || strings.Contains(plain, "doc-watermark") {
		t.Fatal("未开启分享选项时不应用封面/水印")
	}

	data := st.GetData()
	// 分享结构始终跟随「文档模板」，无需额外开关
	data.Settings.ShareDocWatermark = true
	data.Settings.DocLayout = model.DocLayout{
		Toc: true, Numbering: true,
		Cover:    model.DocCover{Enabled: true, Subtitle: "内部文档"},
		Prefaces: []model.DocChapter{{Title: "文档说明", Content: "- 覆盖全部接口"}},
	}
	data.Settings.Watermark = model.DocWatermark{Enabled: true, Text: "内部资料 {{project}}"}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存分享设置失败: %v", err)
	}

	withOpts, err := BuildHTMLByAPI(st, "", "a1")
	if err != nil {
		t.Fatalf("套用结构的分享 HTML 构建失败: %v", err)
	}
	for _, want := range []string{`id="cover"`, "内部文档", `id="toc"`, "文档说明", "doc-watermark", "内部资料"} {
		if !strings.Contains(withOpts, want) {
			t.Fatalf("分享 HTML 缺少内容 %q", want)
		}
	}

	// 项目级覆盖优先：该项目改用「无封面 + 附录」的结构
	data = st.GetData()
	data.Settings.ProjectDocLayouts = map[string]model.DocLayout{
		data.Projects[0].ID: {Toc: true, StatusCodes: true},
	}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存项目级结构失败: %v", err)
	}
	proj, err := BuildHTMLByAPI(st, "", "a1")
	if err != nil {
		t.Fatalf("项目级结构的分享 HTML 构建失败: %v", err)
	}
	if strings.Contains(proj, `id="cover"`) {
		t.Fatal("项目级结构未覆盖封面配置")
	}
	if !strings.Contains(proj, "附录：HTTP 状态码") {
		t.Fatal("项目级结构未生效（缺附录状态码）")
	}
}

// TestShareFollowsTemplateAfterChange 模板变更后，分享内容（含已有链接）要跟着变。
func TestShareFollowsTemplateAfterChange(t *testing.T) {
	st := newTestStore(t)

	// 初始：分享不含附录状态码
	before, err := BuildHTMLByAPI(st, "", "a1")
	if err != nil {
		t.Fatalf("分享 HTML 构建失败: %v", err)
	}
	if strings.Contains(before, "附录：HTTP 状态码") {
		t.Fatal("初始分享不应包含状态码附录")
	}

	// 在「文档模板」里开启附录 + 封面（不打开任何分享开关）
	data := st.GetData()
	data.Settings.DocLayout = model.DocLayout{
		Toc: true, Numbering: true, StatusCodes: true,
		Cover: model.DocCover{Enabled: true, Subtitle: "对外文档"},
	}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存模板失败: %v", err)
	}
	after, err := BuildHTMLByAPI(st, "", "a1")
	if err != nil {
		t.Fatalf("分享 HTML 构建失败: %v", err)
	}
	if !strings.Contains(after, "附录：HTTP 状态码") || !strings.Contains(after, "对外文档") {
		t.Fatal("分享内容未跟随最新文档模板")
	}

	// 已有分享链接也要能按最新模板刷新
	token, _, err := CreateShare(st, "", "a1", "", 0)
	if err != nil {
		t.Fatalf("创建分享失败: %v", err)
	}
	shareMu.Lock()
	old := shareItems[token].HTML
	shareMu.Unlock()
	if !strings.Contains(old, "附录：HTTP 状态码") {
		t.Fatal("新建分享应包含当前模板内容")
	}

	data = st.GetData()
	data.Settings.DocLayout = model.DocLayout{Toc: true, Numbering: true}
	if err := st.SaveData(data); err != nil {
		t.Fatalf("保存模板失败: %v", err)
	}
	n, err := RefreshAllShares(st)
	if err != nil {
		t.Fatalf("刷新分享失败: %v", err)
	}
	if n == 0 {
		t.Fatal("应至少刷新一条分享")
	}
	shareMu.Lock()
	refreshed := shareItems[token].HTML
	shareMu.Unlock()
	if strings.Contains(refreshed, "附录：HTTP 状态码") {
		t.Fatal("刷新后分享内容未跟随新模板")
	}
	_ = StopShare(token)
}

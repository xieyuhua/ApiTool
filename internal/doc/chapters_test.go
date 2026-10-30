package doc

import (
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

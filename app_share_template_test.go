package main

import (
	"strings"
	"testing"

	"apitool/internal/model"
)

// TestBuildSharedHTMLFollowsTemplate 分享内容（网页代码 / 在线链接）必须按「文档模板」当前结构生成。
func TestBuildSharedHTMLFollowsTemplate(t *testing.T) {
	a := newTestApp(t)

	data := a.store.GetData()
	data.Projects[0].Apis = []model.ApiInfo{
		{ID: "a1", Name: "查询用户列表", Method: "get", URL: "https://api.example.com/users"},
	}
	data.Settings.DocLayout = model.DocLayout{
		Toc: true, Numbering: true, StatusCodes: true, Environment: true,
		Cover:    model.DocCover{Enabled: true, Subtitle: "对外接口文档"},
		Prefaces: []model.DocChapter{{Title: "文档说明", Content: "- 覆盖全部接口"}},
	}
	if err := a.store.SaveData(data); err != nil {
		t.Fatalf("保存模板失败: %v", err)
	}

	html, err := a.BuildSharedHTML("", "a1")
	if err != nil {
		t.Fatalf("生成分享 HTML 失败: %v", err)
	}
	for _, want := range []string{
		"对外接口文档",      // 封面
		`id="toc"`,    // 目录
		"文档说明",        // 前言章节
		"接入地址（环境）",    // 环境章节
		"附录：HTTP 状态码", // 附录
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("分享 HTML 未按模板输出 %q", want)
		}
	}

	// 关闭模板章节后分享也应跟着变
	data = a.store.GetData()
	data.Settings.DocLayout = model.DocLayout{Toc: true, Numbering: true}
	if err := a.store.SaveData(data); err != nil {
		t.Fatalf("保存模板失败: %v", err)
	}
	html2, err := a.BuildSharedHTML("", "a1")
	if err != nil {
		t.Fatalf("生成分享 HTML 失败: %v", err)
	}
	for _, bad := range []string{"对外接口文档", "附录：HTTP 状态码", "接入地址（环境）"} {
		if strings.Contains(html2, bad) {
			t.Fatalf("模板关闭后分享仍包含 %q", bad)
		}
	}

}

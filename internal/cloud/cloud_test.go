package cloud

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apitool/internal/model"
)

func sampleTemplates() []model.DocLayoutTemplate {
	return []model.DocLayoutTemplate{
		{ID: "t1", Name: "对外版", Layout: model.DocLayout{Toc: true, Numbering: true}},
		{ID: "t2", Name: "评审版", Layout: model.DocLayout{Toc: true}},
	}
}

func TestNewNormalizesBaseURL(t *testing.T) {
	cases := map[string]string{
		"example.com:8080":         "http://example.com:8080",
		"http://example.com/":      "http://example.com",
		"https://example.com/api/": "https://example.com/api",
		"  example.com  ":          "http://example.com",
	}
	for in, want := range cases {
		if got := New(in, " tok ").BaseURL; got != want {
			t.Fatalf("New(%q).BaseURL = %q, want %q", in, got, want)
		}
	}
	if got := New("example.com", " tok ").Token; got != "tok" {
		t.Fatalf("token 未去空格: %q", got)
	}
}

func TestPushTemplatesSendsReservedProject(t *testing.T) {
	var (
		gotPath   string
		gotAuth   string
		gotMethod string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotMethod = r.URL.Path, r.Header.Get("Authorization"), r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok123")
	if err := c.PushTemplates(sampleTemplates()); err != nil {
		t.Fatalf("PushTemplates 失败: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/projects/"+TemplateProjectID {
		t.Fatalf("请求不符合约定: %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("鉴权头 = %q", gotAuth)
	}
	if gotBody["name"] != TemplateProjectName {
		t.Fatalf("项目名 = %v", gotBody["name"])
	}
	data, _ := gotBody["data"].(map[string]any)
	if data == nil {
		t.Fatalf("缺少 data 字段: %v", gotBody)
	}
	tpls, _ := data["docTemplates"].([]any)
	if len(tpls) != 2 {
		t.Fatalf("docTemplates 数量 = %d, want 2", len(tpls))
	}
	if data["id"] != TemplateProjectID {
		t.Fatalf("data.id = %v", data["id"])
	}
}

func TestPullTemplatesParsesPayload(t *testing.T) {
	payload := map[string]any{
		"updatedAt": "2026-10-29T10:00:00Z",
		"data": map[string]any{
			"id": TemplateProjectID, "name": TemplateProjectName,
			"docTemplates": []map[string]any{
				{"id": "r1", "name": "云端版", "layout": map[string]any{"toc": true, "numbering": true}},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	tpls, err := New(srv.URL, "t").PullTemplates()
	if err != nil {
		t.Fatalf("PullTemplates 失败: %v", err)
	}
	if len(tpls) != 1 || tpls[0].Name != "云端版" || !tpls[0].Layout.Numbering {
		t.Fatalf("解析结果不符合预期: %+v", tpls)
	}
}

func TestPullTemplatesNotFoundIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"项目不存在"}`))
	}))
	defer srv.Close()

	tpls, err := New(srv.URL, "t").PullTemplates()
	if err != nil {
		t.Fatalf("云端无模板库不应报错: %v", err)
	}
	if len(tpls) != 0 {
		t.Fatalf("应为空模板库, got %+v", tpls)
	}
}

func TestPushTemplatesRetriesAfterConflict(t *testing.T) {
	var pulls, pushes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			pulls++
			_, _ = w.Write([]byte(`{"data":{"id":"` + TemplateProjectID + `","docTemplates":[]}}`))
		case http.MethodPut:
			pushes++
			if pushes == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"云端版本更新，请先拉取"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	if err := New(srv.URL, "t").PushTemplates(sampleTemplates()); err != nil {
		t.Fatalf("PushTemplates 冲突重试失败: %v", err)
	}
	if pulls != 1 || pushes != 2 {
		t.Fatalf("重试次数不符合预期: pulls=%d pushes=%d", pulls, pushes)
	}
}

func TestPushTemplatesRequiresBaseURL(t *testing.T) {
	if err := New("", "").PushTemplates(nil); err == nil || !strings.Contains(err.Error(), "未配置云服务器地址") {
		t.Fatalf("未配置地址时应报错, got %v", err)
	}
}

func TestMergeTemplates(t *testing.T) {
	local := []model.DocLayoutTemplate{
		{ID: "l1", Name: "对外版", Layout: model.DocLayout{Toc: true}},
		{ID: "l2", Name: "本地独有", Layout: model.DocLayout{Numbering: true}},
	}
	remote := []model.DocLayoutTemplate{
		{ID: "r1", Name: "对外版", Layout: model.DocLayout{Toc: true, Numbering: true, StatusCodes: true}},
		{ID: "r2", Name: "云端新增", Layout: model.DocLayout{Toc: true}},
		{ID: "r3", Name: "   ", Layout: model.DocLayout{}},
	}
	merged, changed := MergeTemplates(local, remote)
	if changed != 2 {
		t.Fatalf("变更数量 = %d, want 2", changed)
	}
	if len(merged) != 3 {
		t.Fatalf("合并后数量 = %d, want 3", len(merged))
	}
	if merged[0].ID != "r1" || !merged[0].Layout.StatusCodes {
		t.Fatalf("同名模板应被云端覆盖: %+v", merged[0])
	}
	if merged[1].Name != "本地独有" || merged[2].Name != "云端新增" {
		t.Fatalf("合并顺序/内容不符合预期: %+v", merged)
	}
	// 空名模板被忽略
	for _, m := range merged {
		if strings.TrimSpace(m.Name) == "" {
			t.Fatalf("不应保留空名模板: %+v", merged)
		}
	}
}

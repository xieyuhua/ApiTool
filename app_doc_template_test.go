package main

import (
	"testing"

	"apitool/internal/model"
	"apitool/internal/store"
)

// newTestApp 构造只带存储的 App，用于模板管理逻辑测试。
func newTestApp(t *testing.T) *App {
	t.Helper()
	st := store.New(t.TempDir(), "test", "")
	t.Cleanup(func() { _ = st.Close() })
	return &App{store: st}
}

// TestDocTemplateManagement 自定义模板的新建（同名覆盖）/ 重命名 / 复制 / 删除。
func TestDocTemplateManagement(t *testing.T) {
	a := newTestApp(t)

	layout := `{"toc":true,"numbering":true,"name":"事业部标准","cover":{"enabled":true}}`
	tpl, err := a.SaveDocLayoutTemplate("事业部标准", layout)
	if err != nil {
		t.Fatalf("保存模板失败: %v", err)
	}
	if tpl.ID == "" || tpl.Name != "事业部标准" || !tpl.Layout.Toc {
		t.Fatalf("保存结果不符合预期: %+v", tpl)
	}

	// 同名保存视为覆盖，不新增条目
	again, err := a.SaveDocLayoutTemplate("事业部标准", `{"toc":true,"statusCodes":true}`)
	if err != nil {
		t.Fatalf("覆盖保存失败: %v", err)
	}
	if again.ID != tpl.ID {
		t.Fatal("同名保存应复用原有模板 ID")
	}
	if list := a.store.GetData().Settings.DocLayoutTemplates; len(list) != 1 {
		t.Fatalf("模板数量 = %d, want 1", len(list))
	}
	if !a.store.GetData().Settings.DocLayoutTemplates[0].Layout.StatusCodes {
		t.Fatal("覆盖后内容未更新")
	}

	// 重命名
	if err := a.RenameDocLayoutTemplate(tpl.ID, "事业部标准 v2"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	if got := a.store.GetData().Settings.DocLayoutTemplates[0].Name; got != "事业部标准 v2" {
		t.Fatalf("重命名后名称 = %q", got)
	}

	// 重名要报错
	if _, err := a.SaveDocLayoutTemplate("另一个", `{"toc":true}`); err != nil {
		t.Fatalf("保存第二个模板失败: %v", err)
	}
	second := a.store.GetData().Settings.DocLayoutTemplates[1]
	if err := a.RenameDocLayoutTemplate(second.ID, "事业部标准 v2"); err == nil {
		t.Fatal("重命名为已存在的名称应报错")
	}

	// 复制
	copy1, err := a.DuplicateDocLayoutTemplate(tpl.ID)
	if err != nil {
		t.Fatalf("复制模板失败: %v", err)
	}
	if copy1.Name != "事业部标准 v2 副本" || copy1.Layout.Name != copy1.Name {
		t.Fatalf("复制结果不符合预期: %+v", copy1)
	}
	if len(a.store.GetData().Settings.DocLayoutTemplates) != 3 {
		t.Fatalf("复制后模板数量 = %d, want 3", len(a.store.GetData().Settings.DocLayoutTemplates))
	}

	// 删除
	if err := a.DeleteDocLayoutTemplate(copy1.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	for _, item := range a.store.GetData().Settings.DocLayoutTemplates {
		if item.ID == copy1.ID {
			t.Fatal("删除后模板仍存在")
		}
	}

	// 上限保护：先清空，再填满 20 套，第 21 套应报错
	data := a.store.GetData()
	data.Settings.DocLayoutTemplates = nil
	if err := a.store.SaveData(data); err != nil {
		t.Fatalf("清空模板失败: %v", err)
	}
	for i := 0; i < 20; i++ {
		if _, err := a.SaveDocLayoutTemplate("填充"+string(rune('A'+i)), `{"toc":true}`); err != nil {
			t.Fatalf("批量保存失败（第 %d 套）: %v", i+1, err)
		}
	}
	if _, err := a.SaveDocLayoutTemplate("溢出", `{"toc":true}`); err == nil {
		t.Fatal("超过 20 套应报错")
	}
	_ = model.DocLayout{}
}

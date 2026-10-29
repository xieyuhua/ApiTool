package doc

import (
	"os"
	"path/filepath"
	"testing"

	"apitool/internal/model"
)

// TestDumpSignTemplates 仅在 APITOOL_DOC_DUMP=1 时运行：把每套验签模板渲染为 HTML，便于人工核对。
func TestDumpSignTemplates(t *testing.T) {
	if os.Getenv("APITOOL_DOC_DUMP") != "1" {
		t.Skip("跳过（未设置 APITOOL_DOC_DUMP=1）")
	}
	dir := os.Getenv("APITOOL_DOC_DUMP_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	title, rootID, dirs, apis, common := sampleScope()
	for _, tpl := range SignTemplateList() {
		layout := EnrichDocLayout(model.DocLayout{Toc: false, Sign: tpl.Content}, nil, common)
		html := BuildHTMLForExportWithLayout(title, rootID, dirs, apis, common, model.DocWatermark{}, layout)
		if err := os.WriteFile(filepath.Join(dir, "sign-"+tpl.Key+".html"), []byte(html), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

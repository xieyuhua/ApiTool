package doc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"apitool/internal/model"
)

// TestDumpArtifacts 仅在 APITOOL_DOC_DUMP=1 时运行：把样例 DOCX / PDF / HTML 输出到
// APITOOL_DOC_DUMP_DIR，便于人工检查版式与水印效果。
func TestDumpArtifacts(t *testing.T) {
	if os.Getenv("APITOOL_DOC_DUMP") != "1" {
		t.Skip("跳过产物导出（未设置 APITOOL_DOC_DUMP=1）")
	}
	dir := os.Getenv("APITOOL_DOC_DUMP_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	title, rootID, dirs, apis, common := sampleScope()
	wm := ExpandWatermark(NormalizeWatermark(model.DocWatermark{Enabled: true, Text: os.Getenv("APITOOL_DOC_WM_TEXT"), Repeat: true}),
		"示例项目", time.Now())

	b, err := BuildDOCX(title, rootID, dirs, apis, common, wm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc-sample.docx"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	html := BuildHTMLForExport(title, rootID, dirs, apis, common, wm)
	if err := os.WriteFile(filepath.Join(dir, "doc-sample.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHTMLToPDF(html, filepath.Join(dir, "doc-sample.pdf")); err != nil {
		t.Fatalf("PDF 导出失败: %v", err)
	}

	// 测试报告（HTML / PDF / Word）同样带水印，便于核对打印分页与版式
	reportHTML := BuildTestReportHTMLForExport(sampleReport(), wm)
	if err := os.WriteFile(filepath.Join(dir, "report-sample.html"), []byte(reportHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHTMLToPDF(reportHTML, filepath.Join(dir, "report-sample.pdf")); err != nil {
		t.Fatalf("测试报告 PDF 导出失败: %v", err)
	}
	reportDOCX, err := BuildTestReportDOCX(sampleReport(), wm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report-sample.docx"), reportDOCX, 0o644); err != nil {
		t.Fatal(err)
	}

	// 企业级排版样例（封面 / 修订记录 / 目录 / 公共描述 / 规范 / 附录）
	layout := sampleLayout()
	layout.Numbering = true // 打开章节编号，便于核对编号效果
	// 企业标准章节：接入地址 / 公共请求头 / 加密验签
	layout.Environment, layout.CommonHeaders, layout.Sign = true, true, DefaultSignTemplate()
	layout = EnrichDocLayout(layout, sampleEnvironments(), common)
	entDOCX, err := BuildDOCXWithLayout(title, rootID, dirs, apis, common, wm, layout)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "enterprise-sample.docx"), entDOCX, 0o644); err != nil {
		t.Fatal(err)
	}
	entHTML := BuildHTMLForExportWithLayout(title, rootID, dirs, apis, common, wm, layout)
	if err := os.WriteFile(filepath.Join(dir, "enterprise-sample.html"), []byte(entHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHTMLToPDF(entHTML, filepath.Join(dir, "enterprise-sample.pdf")); err != nil {
		t.Fatalf("企业文档 PDF 导出失败: %v", err)
	}
}

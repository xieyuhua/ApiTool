package doc

import "apitool/internal/pdfprint"

// PDF 导出：把已渲染的 HTML 文档交给本机 Chromium 内核浏览器无头打印。
// 具体的浏览器查找与打印参数由 internal/pdfprint 统一维护（agent 会话导出共用同一套实现）。

// WriteHTMLToPDF 把 HTML 文本渲染为 PDF 文件（覆盖写入 pdfPath）。
func WriteHTMLToPDF(htmlContent, pdfPath string) error {
	return pdfprint.FromHTML(htmlContent, pdfPath, pdfprint.Options{TempPrefix: "apitool-doc-pdf"})
}

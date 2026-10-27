package doc

// 水印的 HTML / PDF 侧实现：把水印渲染成一层 position:fixed 的重复文本节点。
//
// 为什么不用背景图：文本节点在 Chromium 打印时仍是矢量文字，缩放/打印清晰；
// 而 position:fixed 的元素在分页打印时会重复绘制到每一页，因此 PDF 每页都带水印。

import (
	"fmt"
	"math"
	"strings"

	"apitool/internal/model"
)

// A4 页面在 96dpi 下的近似像素尺寸，用于估算平铺网格（无需精确，只求铺满且不拥挤）。
const (
	watermarkPagePxW = 794.0
	watermarkPagePxH = 1023.0
)

// watermarkHTMLCSS 生成水印样式与占位元素（未启用时两者均为空串）。
func watermarkHTMLCSS(wm model.DocWatermark) (css string, markup string) {
	if !WatermarkActive(wm) {
		return "", ""
	}
	w := NormalizeWatermark(wm)
	// pt → px（96dpi ≈ 1.34）
	fontPx := math.Max(float64(w.Size)*1.34, 12)
	textEm := watermarkTextWidthEm(w.Text)
	textPx := textEm * fontPx
	// 文字过长时按可用宽度等比缩小字号，避免单个水印横向溢出被裁切
	if textPx > watermarkPagePxW*0.92 && textEm > 0 {
		fontPx = watermarkPagePxW * 0.92 / textEm
		textPx = watermarkPagePxW * 0.92
	}

	css = ".doc-watermark{" +
		`position:fixed;left:0;top:0;width:100%;height:100%;z-index:2147483000;` +
		`pointer-events:none;overflow:hidden;display:flex;flex-wrap:wrap;` +
		`align-items:center;justify-content:center;` +
		`-webkit-print-color-adjust:exact;print-color-adjust:exact}` +
		fmt.Sprintf(`.doc-watermark span{display:block;color:%s;opacity:%s;font-size:%spx;font-weight:600;`+
			`font-family:%s;white-space:nowrap;line-height:1.4;text-align:center;transform:rotate(%ddeg)}`,
			watermarkHex(w.Color), formatFloat(w.Opacity), formatFloat(fontPx), watermarkHTMLFont, w.Angle)

	if !w.Repeat {
		// 单个居中水印
		css += ".doc-watermark{flex-wrap:nowrap}.doc-watermark span{padding:0}"
		return css, `<div class="doc-watermark" aria-hidden="true"><span>` + htmlText(w.Text) + `</span></div>`
	}

	// 平铺：按文字旋转后的实际占用（宽/高）决定列数与行数，
	// 短文案铺得更密、长文案自动减少列行并留出足够行距，避免文字互相压叠。
	rad := math.Abs(float64(w.Angle)) * math.Pi / 180
	lineH := textPx*math.Sin(rad) + fontPx*math.Cos(rad)
	cols := clampInt(int(watermarkPagePxW/(textPx*math.Cos(rad)+fontPx*math.Sin(rad)+40)), 1, 3)
	rows := clampInt(int(watermarkPagePxH/(lineH+16)), 1, 8)
	padY := math.Max(4, (lineH-fontPx*1.4)/2)
	css += fmt.Sprintf(".doc-watermark span{flex:0 0 %.4f%%;padding:%spx 0}",
		100.0/float64(cols), formatFloat(padY))

	var sb strings.Builder
	sb.WriteString(`<div class="doc-watermark" aria-hidden="true">`)
	text := htmlText(w.Text)
	for i := 0; i < cols*rows; i++ {
		sb.WriteString("<span>" + text + "</span>")
	}
	sb.WriteString(`</div>`)
	return css, sb.String()
}

// watermarkHTMLFont 水印字体族（与 Word 侧尽量保持一致的观感）。
const watermarkHTMLFont = `"Microsoft YaHei", "PingFang SC", "Segoe UI", sans-serif`

// htmlText 转义 HTML 文本节点中的特殊字符。
func htmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

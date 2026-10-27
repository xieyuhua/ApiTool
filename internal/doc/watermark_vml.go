package doc

// 水印的 Word 侧实现：写成页眉里的 VML 文本水印（Word 原生水印机制），
// 打开后可在 Word「设计 → 水印」中看到/替换，并且打印、导出 PDF 时每页都有。

import (
	"fmt"
	"strings"

	"apitool/internal/model"
)

// A4 纸张尺寸（pt），用于平铺水印的页面定位
const (
	watermarkPageW = 595.0
	watermarkPageH = 842.0
	// 平铺网格：2 列 × 3 行
	watermarkCols = 2
	watermarkRows = 3
)

// docxWatermarkMarkup 生成页眉中的水印标记（未启用时返回空串）。
func docxWatermarkMarkup(wm model.DocWatermark) string {
	if !WatermarkActive(wm) {
		return ""
	}
	return vmlWatermark(NormalizeWatermark(wm))
}

// vmlWatermark 生成水印形状集合：默认页面居中单个；Repeat 开启时按 2 列 × 3 行平铺。
// 平铺使用 mso-position-*-relative:page 相对纸张定位，保证每页位置一致。
func vmlWatermark(wm model.DocWatermark) string {
	width, height := watermarkBox(wm)
	rot := vmlRotation(wm.Angle)

	var sb strings.Builder
	sb.WriteString(vmlShapeType)

	if !wm.Repeat {
		sb.WriteString(vmlShape(wm, 0, rot, `position:absolute;left:0;text-align:left;margin-left:0;margin-top:0;`+
			fmt.Sprintf(`width:%spt;height:%spt;rotation:%d;z-index:-251658752;`,
				formatFloat(width), formatFloat(height), rot)+
			`mso-position-horizontal:center;mso-position-horizontal-relative:margin;`+
			`mso-position-vertical:center;mso-position-vertical-relative:margin`))
		return vmlRun(sb.String())
	}

	colW, rowH := watermarkPageW/watermarkCols, watermarkPageH/watermarkRows
	index := 0
	for row := 0; row < watermarkRows; row++ {
		for col := 0; col < watermarkCols; col++ {
			left := colW*float64(col) + (colW-width)/2
			top := rowH*float64(row) + (rowH-height)/2
			sb.WriteString(vmlShape(wm, index, rot, fmt.Sprintf(
				`position:absolute;margin-left:%spt;margin-top:%spt;width:%spt;height:%spt;rotation:%d;z-index:-251658752;`+
					`mso-position-horizontal-relative:page;mso-position-vertical-relative:page`,
				formatFloat(left), formatFloat(top), formatFloat(width), formatFloat(height), rot)))
			index++
		}
	}
	return vmlRun(sb.String())
}

// vmlRun 把形状集合包进页眉段落所需的 run/pict 结构（noProof 避免拼写检查标记）。
func vmlRun(shapes string) string {
	return `<w:r><w:rPr><w:noProof/></w:rPr><w:pict>` + shapes + `</w:pict></w:r>`
}

// vmlRotation 把配置角度（顺时针为正、可为负）换算为 VML 的 0-360 整数值。
func vmlRotation(angle int) int {
	rot := angle % 360
	if rot < 0 {
		rot += 360
	}
	return rot
}

// vmlShape 生成单个文字水印形状。
// 形状宽高由字号与文字宽度估算（见 watermarkBox），textpath 的 fitshape
// 会把文字拉伸到该盒子，宽高比合理时不会变形。
func vmlShape(wm model.DocWatermark, index, rotation int, style string) string {
	return fmt.Sprintf(`<v:shape id="PowerPlusWaterMarkObject%d" o:spid="_x0000_s%d" type="#_x0000_t136"`+
		` style="%s" o:allowincell="f" fillcolor="%s" stroked="f">`+
		`<v:fill opacity="%s"/>`+
		`<v:textpath style="font-family:&quot;%s&quot;;font-size:1pt" string="%s"/>`+
		`</v:shape>`,
		1024+index, 2049+index, style, watermarkHex(wm.Color), vmlOpacity(wm.Opacity),
		xmlEscape(watermarkVMLFont), xmlEscape(wm.Text))
}

// watermarkVMLFont 水印文字字体（与 HTML/PDF 侧保持一致）。
const watermarkVMLFont = "Microsoft YaHei"

// vmlShapeType 是 Word 文字水印使用的标准形状类型（艺术字文本框 136）。
const vmlShapeType = `<v:shapetype id="_x0000_t136" coordsize="21600,21600" o:spt="136" adj="10800" path="m@7,l@8,m@5,21600l@6,21600e">` +
	`<v:formulas>` +
	`<v:f eqn="sum #0 0 10800"/><v:f eqn="prod #0 2 1"/><v:f eqn="sum 21600 0 @1"/>` +
	`<v:f eqn="sum 0 0 @2"/><v:f eqn="sum 21600 0 @3"/><v:f eqn="if @0 @3 0"/>` +
	`<v:f eqn="if @0 21600 @1"/><v:f eqn="if @0 0 @2"/><v:f eqn="if @0 @4 21600"/>` +
	`<v:f eqn="mid @5 @6"/><v:f eqn="mid @8 @5"/><v:f eqn="mid @7 @8"/>` +
	`<v:f eqn="mid @6 @7"/><v:f eqn="sum @6 0 @5"/>` +
	`</v:formulas>` +
	`<v:path textpathok="t" o:connecttype="custom" o:connectlocs="@9,0;@10,10800;@11,21600;@12,10800" o:connectangles="270,180,90,0"/>` +
	`<v:textpath on="t" fitshape="t"/>` +
	`<v:handles><v:h position="#0,bottomRight" xrange="6629,14971"/></v:handles>` +
	`<o:lock v:ext="edit" text="t" shapetype="t"/>` +
	`</v:shapetype>`

package doc

// 水印公共逻辑：配置归一化、颜色换算、几何估算与小数值格式化。
// 具体渲染见 watermark_html.go（HTML/PDF 侧）与 watermark_vml.go（Word 侧）。

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"apitool/internal/model"
)

// 水印默认值：用户未配置时使用（Enabled 保持用户选择）。
const (
	DefaultWatermarkText    = "内部资料"
	defaultWatermarkColor   = "#C0C4CC"
	defaultWatermarkSize    = 42
	defaultWatermarkOpacity = 0.14
	defaultWatermarkAngle   = -30
	watermarkMinSize        = 8
	watermarkMaxSize        = 200
)

// ---------------- 配置 ----------------

// NormalizeWatermark 补全水印配置：文字为空用默认文案，颜色/字号/透明度/角度
// 为 0 值时用默认值，并对取值做安全区间约束，保证渲染侧拿到的配置总是可用的。
func NormalizeWatermark(wm model.DocWatermark) model.DocWatermark {
	out := wm
	out.Text = strings.TrimSpace(out.Text)
	if out.Text == "" {
		out.Text = DefaultWatermarkText
	}
	if !isHexColor(out.Color) {
		out.Color = defaultWatermarkColor
	}
	if out.Size <= 0 {
		out.Size = defaultWatermarkSize
	}
	out.Size = clampInt(out.Size, watermarkMinSize, watermarkMaxSize)
	if out.Opacity <= 0 {
		out.Opacity = defaultWatermarkOpacity
	}
	if out.Opacity > 1 {
		out.Opacity = 1
	}
	if out.Angle == 0 {
		out.Angle = defaultWatermarkAngle
	}
	out.Angle = clampInt(out.Angle, -180, 180)
	return out
}

// WatermarkActive 判断是否需要渲染水印（启用且文字非空）。
func WatermarkActive(wm model.DocWatermark) bool {
	return wm.Enabled && strings.TrimSpace(NormalizeWatermark(wm).Text) != ""
}

// 水印文字支持的占位符（导出时替换为实际值，便于外发追溯）。
const (
	watermarkVarDate     = "{{date}}"
	watermarkVarTime     = "{{time}}"
	watermarkVarDateTime = "{{datetime}}"
	watermarkVarProject  = "{{project}}"
)

// WatermarkVarHelp 占位符说明，供前端提示与文档复用。
var WatermarkVarHelp = []struct{ Key, Desc string }{
	{watermarkVarDate, "导出日期，如 2026-10-27"},
	{watermarkVarTime, "导出时间，如 21:51"},
	{watermarkVarDateTime, "日期+时间"},
	{watermarkVarProject, "当前项目名称"},
}

// ExpandWatermark 把水印文字中的占位符替换为导出时的实际值：
//
//	{{date}} 2026-10-27 · {{time}} 21:51 · {{datetime}} 2026-10-27 21:51 · {{project}} 项目名
//
// 未包含占位符时原样返回，避免无谓的字符串分配。
func ExpandWatermark(wm model.DocWatermark, project string, now time.Time) model.DocWatermark {
	if !strings.Contains(wm.Text, "{{") {
		return wm
	}
	repl := strings.NewReplacer(
		watermarkVarDate, now.Format("2006-01-02"),
		watermarkVarTime, now.Format("15:04"),
		watermarkVarDateTime, now.Format("2006-01-02 15:04"),
		watermarkVarProject, strings.TrimSpace(project),
	)
	wm.Text = repl.Replace(wm.Text)
	return wm
}

// isHexColor 判断是否为 #RGB / #RRGGBB 形式的颜色值。
func isHexColor(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if !strings.HasPrefix(s, "#") {
		return false
	}
	for _, r := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// ---------------- 颜色 ----------------

// watermarkHex 把颜色规范化为 #RRGGBB（CSS 与 OOXML 均可直接使用）。
func watermarkHex(color string) string {
	r, g, b := watermarkRGB(color)
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// watermarkRGB 把 #RGB / #RRGGBB 拆分为十进制 RGB；非法值回退为默认灰。
func watermarkRGB(color string) (int, int, int) {
	const (
		fallbackR, fallbackG, fallbackB = 192, 196, 204
	)
	c := strings.TrimPrefix(strings.TrimSpace(color), "#")
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	if len(c) != 6 {
		return fallbackR, fallbackG, fallbackB
	}
	r, err1 := strconv.ParseInt(c[0:2], 16, 32)
	g, err2 := strconv.ParseInt(c[2:4], 16, 32)
	b, err3 := strconv.ParseInt(c[4:6], 16, 32)
	if err1 != nil || err2 != nil || err3 != nil {
		return fallbackR, fallbackG, fallbackB
	}
	return int(r), int(g), int(b)
}

// ---------------- 几何估算 ----------------

// watermarkTextWidthEm 估算水印文字宽度（单位：em）：CJK/全角按 1em、西文按 0.58em。
// 用于换算水印盒子的宽高比，避免 Word 的文字水印被 fitshape 拉伸变形。
func watermarkTextWidthEm(s string) float64 {
	total := 0.0
	for _, r := range s {
		switch {
		case r == ' ':
			total += 0.35
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hangul, r),
			unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), r >= 0xFF00:
			total += 1.0
		default:
			total += 0.58
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

// watermarkBox 计算水印文字占位盒的宽高（单位 pt），宽高比按文字实际宽度估算。
func watermarkBox(wm model.DocWatermark) (width, height float64) {
	size := float64(wm.Size)
	width = math.Max(watermarkTextWidthEm(wm.Text)*size, 40)
	height = size * 1.25
	return width, height
}

// ---------------- 数值格式 ----------------

// formatFloat 生成 OOXML/CSS 中使用的紧凑小数文本（最多两位小数，无多余 0）。
func formatFloat(v float64) string {
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// vmlOpacity 生成 VML 透明度文本（Word 惯用 ".14" 而非 "0.14"）。
func vmlOpacity(v float64) string {
	s := strings.TrimPrefix(fmt.Sprintf("%.2f", v), "0")
	if s == "" {
		s = "0"
	}
	return s
}

// clampInt 把 v 限制在 [min, max] 区间内。
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

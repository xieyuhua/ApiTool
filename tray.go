package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"apitool/internal/wailsrt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// startTray 创建系统托盘（Wails v3 原生 API）。
// 必须在 application.Run 之前调用：v3 的托盘创建会被排队到主循环启动时执行。
func (a *App) startTray(app *application.App) {
	if app == nil || app.SystemTray == nil {
		fmt.Println("系统托盘不可用：application 尚未初始化")
		return
	}
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon())
	tray.SetTooltip("ApiTool - 接口调试与文档工具")

	menu := app.NewMenu()
	menu.Add("显示主窗口").OnClick(func(*application.Context) {
		a.WindowShow()
		a.WindowUnminimise()
		a.windowVisible = true
	})
	menu.Add("隐藏主窗口").OnClick(func(*application.Context) {
		a.WindowHide()
		a.windowVisible = false
	})
	menu.AddSeparator()
	menu.Add("剪贴板历史…").OnClick(func(*application.Context) {
		a.toggleClipboardWindow()
	})
	menu.Add("清空剪贴板历史").OnClick(func(*application.Context) {
		a.ClearClipHistory()
	})
	menu.AddSeparator()
	menu.Add("运行全部测试").OnClick(func(*application.Context) {
		// 通知前端触发「运行全部测试」
		wailsrt.RuntimeImpl.Emit("apitool:tray-run-tests", nil)
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		// 主动退出：放行关闭窗口，再结束应用
		a.quitting = true
		wailsrt.RuntimeImpl.Quit()
	})
	tray.SetMenu(menu)
}

// trayIcon 生成一张 ICO 图标（内部图像为 64x64 PNG：蓝色圆角背景 + 白色 "A"）。
// Windows 托盘图标要求 ICO 容器；这里输出 PNG-in-ICO（Vista+ 原生支持）。
func trayIcon() []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: 22, G: 93, B: 255, A: 255}
	fg := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	transparent := color.RGBA{R: 0, G: 0, B: 0, A: 0}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, transparent)
		}
	}
	// 圆角矩形背景（四角内缩 6px 的圆角）
	const pad = 6
	isCorner := func(x, y int) bool {
		cx, cy := x-pad, y-pad
		if cx < 0 && cy < 0 && cx*cx+cy*cy > pad*pad {
			return true
		}
		rx, ry := x-(size-1-pad), y-pad
		if rx > 0 && ry < 0 && rx*rx+ry*ry > pad*pad {
			return true
		}
		lx, ly := x-pad, y-(size-1-pad)
		if lx < 0 && ly > 0 && lx*lx+ly*ly > pad*pad {
			return true
		}
		ux, uy := x-(size-1-pad), y-(size-1-pad)
		if ux > 0 && uy > 0 && ux*ux+uy*uy > pad*pad {
			return true
		}
		return false
	}
	for y := pad; y < size-pad; y++ {
		for x := pad; x < size-pad; x++ {
			if !isCorner(x, y) {
				img.Set(x, y, bg)
			}
		}
	}
	// 白色 "A"：三角形主体
	drawPx := func(x, y int, c color.Color) {
		if x >= 0 && x < size && y >= 0 && y < size {
			img.Set(x, y, c)
		}
	}
	left := func(y int) int { return int(20 + (32-20)*float64(y-16)/float64(48-16)) }
	right := func(y int) int { return int(44 - (44-32)*float64(y-16)/float64(48-16)) }
	for y := 16; y < 48; y++ {
		for x := left(y); x <= right(y); x++ {
			drawPx(x, y, fg)
		}
	}
	// A 的中间横杠（用背景色挖空）
	for y := 33; y <= 37; y++ {
		for x := 24; x <= 40; x++ {
			drawPx(x, y, bg)
		}
	}
	// 编码为 PNG 作为 ICO 内嵌图像
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	pngBytes := pngBuf.Bytes()

	// 组装 ICO 文件：ICONDIR(6) + ICONDIRENTRY(16) + PNG
	var ico bytes.Buffer
	ico.Write([]byte{0, 0})  // Reserved
	ico.Write([]byte{1, 0})  // Type = 1 (icon)
	ico.Write([]byte{1, 0})  // Count = 1
	ico.WriteByte(byte(size)) // Width (0 表示 256，这里 64)
	ico.WriteByte(byte(size)) // Height
	ico.WriteByte(0)          // ColorCount
	ico.WriteByte(0)          // Reserved
	ico.Write([]byte{1, 0})   // Planes
	ico.Write([]byte{32, 0})  // BitCount
	bc := make([]byte, 4)
	binary.LittleEndian.PutUint32(bc, uint32(len(pngBytes)))
	ico.Write(bc) // BytesInRes
	off := make([]byte, 4)
	binary.LittleEndian.PutUint32(off, 6+16)
	ico.Write(off) // ImageOffset
	ico.Write(pngBytes)
	return ico.Bytes()
}

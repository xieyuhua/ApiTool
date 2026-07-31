package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// 启动系统托盘。在应用生命周期内常驻，提供窗口显隐、运行测试与退出能力。
// 必须在 Wails 主循环所在 goroutine 之外调用（通常 go a.startTray()），
// systray 会自行维护其消息循环。
func (a *App) startTray() {
	systray.Run(a.onTrayReady, a.onTrayExit)
}

func (a *App) onTrayReady() {
	// 托盘图标（16x16 单色 PNG，内嵌生成，避免依赖外部资源文件）
	systray.SetIcon(trayIcon())
	systray.SetTitle("ApiTool")
	systray.SetTooltip("ApiTool - 接口调试与文档工具")

	mShow := systray.AddMenuItem("显示主窗口", "显示/恢复 ApiTool 主窗口")
	mHide := systray.AddMenuItem("隐藏主窗口", "将主窗口最小化到托盘")
	systray.AddSeparator()
	mClip := systray.AddMenuItem("剪贴板历史…", "弹出剪贴板历史，点击条目即可复制")
	mClipClear := systray.AddMenuItem("清空剪贴板历史", "清空全部剪贴板历史记录")
	systray.AddSeparator()
	mRun := systray.AddMenuItem("运行全部测试", "执行当前项目全部测试用例")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出 ApiTool")

	// 图标点击行为由各菜单项（显示/隐藏主窗口）承载
	for {
		select {
		case <-mShow.ClickedCh:
			runtime.WindowShow(a.ctx)
			runtime.WindowUnminimise(a.ctx)
			a.windowVisible = true
		case <-mHide.ClickedCh:
			runtime.WindowHide(a.ctx)
			a.windowVisible = false
		case <-mClip.ClickedCh:
			// 切换剪贴板历史浮层窗口（Vue 渲染，支持文本与图片）
			a.toggleClipboardWindow()
		case <-mClipClear.ClickedCh:
			a.ClearClipHistory()
		case <-mRun.ClickedCh:
			// 通知前端触发「运行全部测试」
			runtime.EventsEmit(a.ctx, "apitool:tray-run-tests", nil)
		case <-mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (a *App) onTrayExit() {
	// 托盘退出时关闭应用，确保无残留进程
	a.quitting = true
	runtime.Quit(a.ctx)
}

// trayIcon 生成一张 ICO 图标（内部图像为 64x64 PNG：蓝色圆角背景 + 白色 "A"）。
// 说明：systray v1.2.2 在 Windows 上通过 LoadImageW + LR_LOADFROMFILE 从临时文件加载图标，
// 该临时文件无扩展名，PNG 在部分 Windows 版本下无法被识别而不显示。
// 因此这里直接输出 .ico 容器（PNG-in-ICO，Vista+ 原生支持），确保托盘图标稳定显示。
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
		// 左上
		cx, cy := x-pad, y-pad
		if cx < 0 && cy < 0 && cx*cx+cy*cy > pad*pad {
			return true
		}
		// 右上
		rx, ry := x-(size-1-pad), y-pad
		if rx > 0 && ry < 0 && rx*rx+ry*ry > pad*pad {
			return true
		}
		// 左下
		lx, ly := x-pad, y-(size-1-pad)
		if lx < 0 && ly > 0 && lx*lx+ly*ly > pad*pad {
			return true
		}
		// 右下
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
	ico.Write([]byte{0, 0})          // Reserved
	ico.Write([]byte{1, 0})          // Type = 1 (icon)
	ico.Write([]byte{1, 0})          // Count = 1
	// ICONDIRENTRY
	ico.WriteByte(byte(size))        // Width (0 表示 256，这里 64)
	ico.WriteByte(byte(size))        // Height
	ico.WriteByte(0)                 // ColorCount
	ico.WriteByte(0)                 // Reserved
	ico.Write([]byte{1, 0})          // Planes
	ico.Write([]byte{32, 0})         // BitCount
	bc := make([]byte, 4)
	binary.LittleEndian.PutUint32(bc, uint32(len(pngBytes)))
	ico.Write(bc)                    // BytesInRes
	off := make([]byte, 4)
	binary.LittleEndian.PutUint32(off, 6+16)
	ico.Write(off)                   // ImageOffset
	ico.Write(pngBytes)
	return ico.Bytes()
}

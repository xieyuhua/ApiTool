package main

import (
	"embed"
	"os"

	"apitool/internal/wailsrt"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

const appTitle = "ApiTool - 接口调试与文档工具"

func main() {
	app := NewApp()

	wailsApp := application.New(application.Options{
		Name:        "ApiTool",
		Description: appTitle,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		// 以服务方式绑定 App：v3 的注册名是 main.App.Method（main 包的反射路径
		// 恒为 "main"）。兼容层见 frontend/src/wailsV3Bridge.js。
		Services: []application.Service{
			application.NewService(app),
		},
		Windows: application.WindowsOptions{
			// 关闭最后一个窗口时不退出（改为隐藏到托盘，见 bindWindowEvents）
			DisableQuitOnLastWindowClosed: true,
			// 允许通过 WAILS_DEBUG_PORT 环境变量开启 WebView2 远程调试，
			// 便于自动化验证前端渲染与绑定调用（未设置时不生效）。
			AdditionalBrowserArgs: debugBrowserArgs(),
		},
	})

	// 注入全局运行时，供业务子包（agent/doc/sniff/...）使用
	wailsrt.SetApp(wailsApp)

	// 初始化业务状态：数据目录、存储、Agent、测试引擎、托盘、剪贴板采集、抓包
	app.startup(wailsApp)

	win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            appTitle,
		Width:            1360,
		Height:           860,
		MinWidth:         1080,
		MinHeight:        700,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	wailsrt.SetMainWindow(win)
	bindWindowEvents(app, win)

	wailsApp.OnShutdown(func() { app.shutdown() })

	if err := wailsApp.Run(); err != nil {
		println("Error:", err.Error())
	}
}

// debugBrowserArgs 在设置了 WAILS_DEBUG_PORT 时返回 WebView2 远程调试参数。
// 仅用于开发/自动化验证，生产环境不设置该变量时不附加任何参数。
func debugBrowserArgs() []string {
	port := os.Getenv("WAILS_DEBUG_PORT")
	if port == "" {
		return nil
	}
	return []string{"--remote-debugging-port=" + port}
}

// bindWindowEvents 注册窗口事件：点击关闭时隐藏到托盘而不是退出应用。
func bindWindowEvents(a *App, win *application.WebviewWindow) {
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		// 主动退出（托盘「退出」）时放行关闭
		if a.quitting {
			return
		}
		e.Cancel()
		a.beforeClose()
	})
}

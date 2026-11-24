// Package bus 定义业务子包与桌面运行时之间的解耦接口。
//
// 业务模块（capture/agent/plugins/share/sync/stress/platform 等）
// 不再直接 import 具体的 UI 框架运行时，而是通过 bus.Bus 接口
// 收发前端事件、弹窗、剪贴板与窗口控制，从而实现可独立编译与单测。
//
// 运行时实现见 internal/wailsrt（Wails v3 适配层）。
package bus

import "apitool/internal/wailsrt"

// Bus 是业务子包与桌面运行时之间的抽象。App 在主包中实现该接口，
// 内部转发到 internal/wailsrt。
type Bus interface {
	// Emit 向前端发送事件（数据透传）。
	Emit(event string, data ...interface{})
	// Quit 退出应用。
	Quit()
	// WindowShow / WindowHide / WindowUnminimise / WindowCenter 控制主窗口。
	WindowShow()
	WindowHide()
	WindowUnminimise()
	WindowCenter()
	// WindowSetAlwaysOnTop 设置主窗口是否置顶。
	WindowSetAlwaysOnTop(b bool)
	// BrowserOpenURL 用系统浏览器打开 URL。
	BrowserOpenURL(url string)
	// ClipboardGetText / ClipboardSetText 读写系统剪贴板。
	ClipboardGetText() (string, error)
	ClipboardSetText(text string) error
	// SaveFileDialog 弹出保存文件对话框。
	SaveFileDialog(opts wailsrt.DialogOptions) (string, error)
	// OpenFileDialog 弹出打开文件对话框。
	OpenFileDialog(opts wailsrt.DialogOptions) (string, error)
	// OpenDirectoryDialog 弹出选择目录对话框。
	OpenDirectoryDialog(opts wailsrt.DialogOptions) (string, error)
}

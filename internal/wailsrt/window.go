package wailsrt

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 主窗口引用：v3 的窗口能力挂在 WebviewWindow 上，这里保存主窗口供全局使用。
var (
	winMu   sync.RWMutex
	mainWin *application.WebviewWindow
)

// SetMainWindow 在创建主窗口后调用。
func SetMainWindow(w *application.WebviewWindow) {
	winMu.Lock()
	mainWin = w
	winMu.Unlock()
}

// MainWindow 返回主窗口（可能为 nil）。
func MainWindow() *application.WebviewWindow {
	winMu.RLock()
	defer winMu.RUnlock()
	return mainWin
}

// ---- bus.Bus 的运行时实现 ----

// Runtime 实现 bus.Bus 中与窗口/事件/剪贴板相关的部分。
type Runtime struct{}

// RuntimeImpl 全局实例（无状态，可直接复用）。
var RuntimeImpl = &Runtime{}

// Emit 向前端推送自定义事件。
func (r *Runtime) Emit(event string, data ...interface{}) {
	if a := App(); a != nil {
		a.Event.Emit(event, data...)
	}
}

// Quit 退出应用。
func (r *Runtime) Quit() {
	if a := App(); a != nil {
		a.Quit()
	}
}

// WindowShow 显示主窗口。
func (r *Runtime) WindowShow() {
	if w := MainWindow(); w != nil {
		w.Show()
	}
}

// WindowHide 隐藏主窗口。
func (r *Runtime) WindowHide() {
	if w := MainWindow(); w != nil {
		w.Hide()
	}
}

// WindowUnminimise 恢复主窗口（取消最小化）。
func (r *Runtime) WindowUnminimise() {
	if w := MainWindow(); w != nil {
		w.UnMinimise()
	}
}

// WindowCenter 居中主窗口。
func (r *Runtime) WindowCenter() {
	if w := MainWindow(); w != nil {
		w.Center()
	}
}

// WindowSetAlwaysOnTop 设置主窗口是否置顶。
func (r *Runtime) WindowSetAlwaysOnTop(b bool) {
	if w := MainWindow(); w != nil {
		w.SetAlwaysOnTop(b)
	}
}

// BrowserOpenURL 用系统默认浏览器打开 URL。
func (r *Runtime) BrowserOpenURL(url string) {
	if a := App(); a != nil {
		_ = a.Browser.OpenURL(url)
	}
}

// ClipboardGetText 读取剪贴板文本。
func (r *Runtime) ClipboardGetText() (string, error) {
	a := App()
	if a == nil {
		return "", errRuntimeNotReady
	}
	text, _ := a.Clipboard.Text()
	return text, nil
}

// ClipboardSetText 写入剪贴板文本。
func (r *Runtime) ClipboardSetText(text string) error {
	a := App()
	if a == nil {
		return errRuntimeNotReady
	}
	if !a.Clipboard.SetText(text) {
		return errClipboardSetFailed
	}
	return nil
}

// SaveFileDialog 弹出保存文件对话框。
func (r *Runtime) SaveFileDialog(opts DialogOptions) (string, error) {
	return SaveFileDialog(nil, opts)
}

// OpenFileDialog 弹出打开文件对话框。
func (r *Runtime) OpenFileDialog(opts DialogOptions) (string, error) {
	return OpenFileDialog(nil, opts)
}

// OpenDirectoryDialog 弹出选择目录对话框。
func (r *Runtime) OpenDirectoryDialog(opts DialogOptions) (string, error) {
	return OpenDirectoryDialog(nil, opts)
}

const errClipboardSetFailed runtimeError = "写入剪贴板失败"

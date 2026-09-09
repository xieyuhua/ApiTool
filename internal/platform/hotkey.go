//go:build windows

package platform

// 全局快捷键功能已移除：不再安装任何系统级键盘钩子。
// 剪贴板历史浮层改由系统托盘菜单「剪贴板历史…」触发（见 app.go / tray.go）。

// SetHotkeyHandlers 保留签名以兼容旧调用，现为无操作。
func SetHotkeyHandlers(ctrlB func()) {}

// StartGlobalHotkey 保留签名以兼容旧调用，现为无操作。
func StartGlobalHotkey() {}

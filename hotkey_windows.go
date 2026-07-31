//go:build windows

package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 全局快捷键：在 Windows 系统层面监听键盘（即使主窗口失焦 / 隐藏到托盘也能触发），
// 连续按两次 Ctrl（默认 500ms 内）即调出剪贴板历史浮层（Vue 渲染，支持文本与图片）。
// 写死，不依赖设置。

const (
	whKeyboardLL = 13
	wmKeydown    = 0x0100
	vkControl    = 0xA2 // VK_CONTROL / VK_LCONTROL
	vkRControl   = 0xA3 // VK_RCONTROL

	doubleCtrlWindow = 500 * time.Millisecond // 两次 Ctrl 按下的最大间隔
)

type kbdLLHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	user32      = windows.NewLazySystemDLL("user32.dll")
	procSetHook = user32.NewProc("SetWindowsHookExW")
	procGetMsg  = user32.NewProc("GetMessageW")
	procNextHook = user32.NewProc("CallNextHookEx")

	hotkeyHook    windows.Handle
	hotkeyApp     *App // 钩子回调使用的 App 实例（NewCallback 不支持闭包，故用包级变量）
	lastCtrlTime  time.Time
)

// startGlobalHotkey 安装全局键盘钩子（应在独立 goroutine 中运行）。
func (a *App) startGlobalHotkey() {
	hotkeyApp = a
	// WH_KEYBOARD_LL 是低级钩子，MSDN 要求 dwModule 必须传 NULL(0)，
	// 传模块句柄会导致钩子不触发。
	h, _, _ := procSetHook.Call(uintptr(whKeyboardLL), uintptr(windows.NewCallback(lowLevelKeyboardProc)), 0, 0)
	hotkeyHook = windows.Handle(h)
	if hotkeyHook == 0 {
		return
	}
	// 消息泵：维持钩子线程。GetMessage 阻塞直到线程收到消息。
	for {
		if r, _, _ := procGetMsg.Call(0, 0, 0, 0); int32(r) <= 0 {
			break
		}
	}
}

// lowLevelKeyboardProc 是全局键盘钩子回调（必须为普通函数，不能用闭包，
// 否则 windows.NewCallback 无法正确捕获上下文，钩子不会触发）。
func lowLevelKeyboardProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		kh := (*kbdLLHookStruct)(unsafe.Pointer(lParam))
		if wParam == wmKeydown && (kh.VkCode == vkControl || kh.VkCode == vkRControl) {
			now := time.Now()
			if now.Sub(lastCtrlTime) <= doubleCtrlWindow {
				// 连续两次 Ctrl：调出剪贴板历史浮层
				lastCtrlTime = time.Time{} // 复位，避免三次连按立即再触发
				go hotkeyApp.toggleClipboardWindow()
				return 1
			}
			lastCtrlTime = now
		}
	}
	r, _, _ := procNextHook.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

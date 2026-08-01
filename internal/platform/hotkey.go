//go:build windows

package platform

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 全局快捷键：在 Windows 系统层面监听键盘（即使主窗口失焦 / 隐藏到托盘也能触发）。
//   - 连续按两次 Ctrl（默认 500ms 内）→ 打开主窗体
//   - Ctrl+B → 打开剪贴板历史浮层
//
// 写死，不依赖设置。

const (
	whKeyboardLL = 13
	wmKeydown    = 0x0100
	wmKeyup      = 0x0101

	vkControl  = 0xA2 // VK_CONTROL / VK_LCONTROL
	vkRControl = 0xA3 // VK_RCONTROL
	vkB        = 0x42 // B

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

	hotkeyHook      windows.Handle
	lastCtrlTime    time.Time
	ctrlDown        bool
	onDoubleCtrl    func() // 由 App 注入：打开主窗体
	onCtrlB         func() // 由 App 注入：打开剪贴板历史浮层
)

// SetHotkeyHandlers 注册全局热键回调（App 在 startup 时调用，注入自身方法）。
func SetHotkeyHandlers(doubleCtrl, ctrlB func()) {
	onDoubleCtrl = doubleCtrl
	onCtrlB = ctrlB
}

// StartGlobalHotkey 安装全局键盘钩子（应在独立 goroutine 中运行）。
func StartGlobalHotkey() {
	h, _, _ := procSetHook.Call(uintptr(whKeyboardLL), uintptr(windows.NewCallback(lowLevelKeyboardProc)), 0, 0)
	hotkeyHook = windows.Handle(h)
	if hotkeyHook == 0 {
		return
	}
	for {
		if r, _, _ := procGetMsg.Call(0, 0, 0, 0); int32(r) <= 0 {
			break
		}
	}
}

// lowLevelKeyboardProc 是全局键盘钩子回调（必须为普通函数，不能用闭包）。
func lowLevelKeyboardProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		kh := (*kbdLLHookStruct)(unsafe.Pointer(lParam))
		vk := kh.VkCode
		down := wParam == wmKeydown
		up := wParam == wmKeyup

		switch {
		case down && (vk == vkControl || vk == vkRControl):
			ctrlDown = true
			now := time.Now()
			if now.Sub(lastCtrlTime) <= doubleCtrlWindow {
				lastCtrlTime = time.Time{} // 复位，避免三次连按立即再触发
				ctrlDown = false
				if onDoubleCtrl != nil {
					go onDoubleCtrl()
				}
				return 1
			}
			lastCtrlTime = now

		case up && (vk == vkControl || vk == vkRControl):
			ctrlDown = false

		case down && vk == vkB && ctrlDown:
			if onCtrlB != nil {
				go onCtrlB()
			}
			return 1
		}
	}
	r, _, _ := procNextHook.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

//go:build windows

package main

import (
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// 全局快捷键：在 Windows 系统层面监听组合键（即使主窗口未聚焦也能触发），
// 匹配后通过 Wails 事件通知前端弹出/收起剪贴板历史。
// 默认组合：Ctrl+Shift+V 与 Ctrl+`（反引号）。
// 注意：WebView2 内部的 keydown 仅在窗口聚焦时生效，无法做到真正「全局」，
// 因此这里使用低级键盘钩子 WH_KEYBOARD_LL 实现系统级捕获。

const (
	whKeyboardLL = 13
	wmKeydown    = 0x0100
	wmSyskeydown = 0x0104
	wmKeyup      = 0x0101
	wmSyskeyup   = 0x0105
)

type kbdLLHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	procSetHook       = user32.NewProc("SetWindowsHookExW")
	procUnhook        = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHook  = user32.NewProc("CallNextHookEx")
	procGetMessage    = user32.NewProc("GetMessageW")
	procTranslateMsg  = user32.NewProc("TranslateMessage")
	procDispatchMsg   = user32.NewProc("DispatchMessageW")
)

var (
	hotkeyHook    windows.Handle
	hotkeyMu      sync.Mutex
	stateCtrl     bool
	stateShift    bool
	stateV        bool
	stateBacktick bool
)

// startGlobalHotkey 安装全局键盘钩子（应在独立 goroutine 中运行）。
func (a *App) startGlobalHotkey() {
	// WH_KEYBOARD_LL 是低级钩子，MSDN 明确要求 dwModule 必须传 NULL(0)，
	// 传模块句柄会导致钩子安装失败/不触发，这是此前快捷键无效的根因。
	hook, _, _ := procSetHook.Call(
		uintptr(whKeyboardLL),
		uintptr(windows.NewCallback(lowLevelKeyboardProc(a))),
		0,
		0,
	)
	hotkeyHook = windows.Handle(hook)
	if hotkeyHook == 0 {
		return
	}
	// 消息循环：保持钩子线程可响应
	var msg struct {
		Hwnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
		_       [2]uintptr // 补齐 SIZE_T 对齐
	}
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) == -1 || int32(ret) == 0 {
			break
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// lowLevelKeyboardProc 返回钩子回调函数。
func lowLevelKeyboardProc(a *App) func(int32, uintptr, uintptr) uintptr {
	return func(nCode int32, wParam uintptr, lParam uintptr) uintptr {
		if nCode >= 0 {
			kh := (*kbdLLHookStruct)(unsafe.Pointer(lParam))
			vk := kh.VkCode
			down := wParam == wmKeydown || wParam == wmSyskeydown

			switch vk {
			case 0xA2, 0xA3: // VK_CONTROL / VK_LCONTROL / VK_RCONTROL
				stateCtrl = down
			case 0x10: // VK_SHIFT
				stateShift = down
			case 0x56: // V
				stateV = down
			case 0xC0, 0xDE: // VK_OEM_3 (反引号/波浪号 `)
				stateBacktick = down
			}

			hotkeyMu.Lock()
			// 窗口可能已最小化/隐藏到托盘，先恢复窗口确保弹窗可见
			showWindow := func() {
				runtime.WindowShow(a.ctx)
				runtime.WindowUnminimise(a.ctx)
				a.windowVisible = true
			}
			// Ctrl+Shift+V
			if stateCtrl && stateShift && stateV {
				stateV = false // 防止重复触发，等待再次按下
				hotkeyMu.Unlock()
				showWindow()
				runtime.EventsEmit(a.ctx, "apitool:toggle-clipboard", nil)
				return 1
			}
			// Ctrl+`
			if stateCtrl && stateBacktick {
				stateBacktick = false
				hotkeyMu.Unlock()
				showWindow()
				runtime.EventsEmit(a.ctx, "apitool:toggle-clipboard", nil)
				return 1
			}
			hotkeyMu.Unlock()
		}
		ret, _, _ := procCallNextHook.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}
}

// stopGlobalHotkey 卸载钩子。
func (a *App) stopGlobalHotkey() {
	if hotkeyHook != 0 {
		procUnhook.Call(uintptr(hotkeyHook))
		hotkeyHook = 0
	}
}

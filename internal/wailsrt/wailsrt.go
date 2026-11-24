// Package wailsrt 把业务代码与具体的 Wails 运行时解耦。
//
// 迁移到 Wails v3 后，窗口 / 事件 / 对话框 / 剪贴板等能力都挂在 application.App
// 的各个 Manager 上，而本项目的业务子包（agent/capture/doc/sniff/stress/testing/plugins）
// 通过 bus.Bus 与少量包级函数获取这些能力。本包既是 bus.Bus 的默认实现，
// 也为 doc 等直接使用运行时（需要 context.Context）的包提供与框架无关的入口。
package wailsrt

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	mu  sync.RWMutex
	app *application.App
)

// SetApp 在 main 中创建 application.App 后调用，注入全局实例。
func SetApp(a *application.App) {
	mu.Lock()
	app = a
	mu.Unlock()
}

// App 返回全局 application.App（可能为 nil，表示尚未初始化）。
func App() *application.App {
	mu.RLock()
	defer mu.RUnlock()
	return app
}

// DialogOptions 与 Wails v2 的 runtime.SaveDialogOptions / OpenDialogOptions
// 字段保持一致，避免业务代码因为迁移而改写大量调用点。
type DialogOptions struct {
	DefaultFilename     string
	Title               string
	DefaultDirectory    string
	Filters             []FileFilter
	ShowHiddenFiles     bool
	CanCreateDirectories bool
	ResolvesAliases     bool
}

// FileFilter 与 Wails v2 的 runtime.FileFilter 字段一致。
type FileFilter struct {
	DisplayName string
	Pattern     string
}

func toFilters(in []FileFilter) []application.FileFilter {
	if len(in) == 0 {
		return nil
	}
	out := make([]application.FileFilter, 0, len(in))
	for _, f := range in {
		out = append(out, application.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern})
	}
	return out
}

// SaveFileDialog 弹出保存文件对话框，返回用户选择的路径。
func SaveFileDialog(_ context.Context, opts DialogOptions) (string, error) {
	a := App()
	if a == nil {
		return "", errRuntimeNotReady
	}
	d := a.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:               opts.Title,
		Filename:            opts.DefaultFilename,
		Directory:           opts.DefaultDirectory,
		Filters:             toFilters(opts.Filters),
		CanCreateDirectories: opts.CanCreateDirectories,
		ShowHiddenFiles:     opts.ShowHiddenFiles,
	})
	return d.PromptForSingleSelection()
}

// OpenFileDialog 弹出打开文件对话框，返回用户选择的路径。
func OpenFileDialog(_ context.Context, opts DialogOptions) (string, error) {
	a := App()
	if a == nil {
		return "", errRuntimeNotReady
	}
	d := a.Dialog.OpenFile().
		SetTitle(opts.Title).
		SetDirectory(opts.DefaultDirectory)
	for _, f := range toFilters(opts.Filters) {
		d = d.AddFilter(f.DisplayName, f.Pattern)
	}
	if opts.ShowHiddenFiles {
		d = d.ShowHiddenFiles(true)
	}
	if opts.ResolvesAliases {
		d = d.ResolvesAliases(true)
	}
	if opts.CanCreateDirectories {
		d = d.CanCreateDirectories(true)
	}
	return d.PromptForSingleSelection()
}

// OpenDirectoryDialog 弹出选择目录对话框，返回用户选择的路径。
func OpenDirectoryDialog(_ context.Context, opts DialogOptions) (string, error) {
	a := App()
	if a == nil {
		return "", errRuntimeNotReady
	}
	d := a.Dialog.OpenFile().
		SetTitle(opts.Title).
		SetDirectory(opts.DefaultDirectory).
		CanChooseDirectories(true).
		CanChooseFiles(false)
	if opts.ShowHiddenFiles {
		d = d.ShowHiddenFiles(true)
	}
	return d.PromptForSingleSelection()
}

// BrowserOpenURL 用系统默认浏览器打开 URL（供 doc 包等直接调用）。
func BrowserOpenURL(_ context.Context, url string) {
	RuntimeImpl.BrowserOpenURL(url)
}

// ClipboardSetText 写入剪贴板文本（供 doc 包等直接调用）。
func ClipboardSetText(_ context.Context, text string) error {
	return RuntimeImpl.ClipboardSetText(text)
}

type runtimeError string

func (e runtimeError) Error() string { return string(e) }

const errRuntimeNotReady runtimeError = "Wails 运行时未就绪"

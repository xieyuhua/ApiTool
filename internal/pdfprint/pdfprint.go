// Package pdfprint 把 HTML 交给本机 Chromium 内核浏览器（Edge / Chrome）无头打印为 PDF。
//
// 设计取舍：
//   - 不引入体积庞大的前端 PDF 库，也不需要联网；
//   - 无头打印能完整保留 CSS 分页（@page）、表格与矢量文字，水印等固定定位元素会重复绘制到每页；
//   - 本机没有 Chromium 内核浏览器时返回明确错误，由调用方引导用户改用「导出 HTML 后手动打印」。
//
// agent 会话导出与文档中心导出共用本包，避免各自维护一套浏览器查找与打印参数。
package pdfprint

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
)

// printMu 串行化无头打印：避免同时拉起多个浏览器进程。
var printMu sync.Mutex

// 默认参数
const (
	defaultVirtualTimeBudgetMS = 6000
	defaultTempPrefix          = "apitool-pdf"
	errorTailRunes             = 300
)

// Options 打印参数（零值即默认值）。
type Options struct {
	// VirtualTimeBudgetMS 给页面脚本/字体加载预留的渲染时间（毫秒）；含 mermaid 等远程图表时可调大。
	VirtualTimeBudgetMS int
	// TempPrefix 临时 HTML 文件所在目录的前缀，便于在系统临时目录中辨识来源。
	TempPrefix string
}

func (o Options) budget() int {
	if o.VirtualTimeBudgetMS > 0 {
		return o.VirtualTimeBudgetMS
	}
	return defaultVirtualTimeBudgetMS
}

func (o Options) tempPrefix() string {
	if o.TempPrefix != "" {
		return o.TempPrefix
	}
	return defaultTempPrefix
}

// FromHTML 把一段 HTML 文本打印为 pdfPath。
func FromHTML(htmlContent, pdfPath string, opt Options) error {
	tmp, err := os.MkdirTemp("", opt.tempPrefix())
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "page.html")
	if err := os.WriteFile(src, []byte(htmlContent), 0o644); err != nil {
		return err
	}
	return FromHTMLFile(src, pdfPath, opt)
}

// FromHTMLFile 把已存在的本地 HTML 文件打印为 pdfPath。
//
// 每次打印都使用独立的临时 user-data-dir：
//   - 避免与本机正在运行的 Edge / Chrome 共用默认配置目录（可能被占用或污染用户配置）；
//   - 也避免多次导出之间残留缓存互相影响。
//
// 打印过程串行执行：无头打印每次都要冷启动一个浏览器进程（约 1-2 秒、内存占用较高），
// 并发既没有收益，也容易让低配机器同时拉起多个浏览器导致卡顿。
func FromHTMLFile(src, pdfPath string, opt Options) error {
	browser, err := FindBrowser()
	if err != nil {
		return err
	}
	profile, err := os.MkdirTemp("", opt.tempPrefix()+"-profile")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)

	printMu.Lock()
	defer printMu.Unlock()

	if err := print(browser, src, pdfPath, profile, opt.budget(), true); err != nil {
		// 旧版本浏览器可能不认 --no-pdf-header-footer，去掉后再试一次
		if err2 := print(browser, src, pdfPath, profile, opt.budget(), false); err2 != nil {
			return err
		}
	}
	return nil
}

// print 执行一次无头打印；noHeader 控制是否隐藏浏览器默认页眉页脚。
func print(browser, src, pdfPath, profileDir string, budgetMS int, noHeader bool) error {
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-extensions",
		"--hide-scrollbars",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
		"--run-all-compositor-stages-before-draw",
		"--user-data-dir=" + profileDir,
		fmt.Sprintf("--virtual-time-budget=%d", budgetMS),
	}
	if noHeader {
		args = append(args, "--no-pdf-header-footer")
	}
	args = append(args, "--print-to-pdf="+pdfPath, fileURL(src))
	out, err := exec.Command(browser, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("调用浏览器生成 PDF 失败: %s", tailText(string(out)))
	}
	if fi, statErr := os.Stat(pdfPath); statErr != nil || fi.Size() == 0 {
		return fmt.Errorf("浏览器未生成 PDF 文件: %s", tailText(string(out)))
	}
	return nil
}

// FindBrowser 查找本机可用的 Chromium 内核浏览器（Edge / Chrome / Chromium）。
func FindBrowser() (string, error) {
	for _, p := range candidatePaths() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	for _, name := range []string{"msedge", "chrome", "chromium", "google-chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 Chrome / Edge 浏览器，无法直接生成 PDF；请改用「导出 HTML」，再在浏览器中打印为 PDF")
}

// candidatePaths 按平台给出常见的 Chromium 内核浏览器安装路径。
func candidatePaths() []string {
	switch goruntime.GOOS {
	case "windows":
		envPaths := []struct{ env, rel string }{
			{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
			{"LOCALAPPDATA", `Microsoft\Edge\Application\msedge.exe`},
			{"LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`},
		}
		paths := make([]string, 0, len(envPaths))
		for _, p := range envPaths {
			if base := os.Getenv(p.env); base != "" {
				paths = append(paths, filepath.Join(base, p.rel))
			}
		}
		return paths
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		var paths []string
		for _, name := range []string{"microsoft-edge", "microsoft-edge-stable", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if p, err := exec.LookPath(name); err == nil {
				paths = append(paths, p)
			}
		}
		return paths
	}
}

// fileURL 把本地路径转为 file:// URL（Windows 下盘符需转为 /C:/ 形式）。
func fileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

// tailText 截取文本末尾若干字符（按 rune 截断，避免中文乱码）。
func tailText(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= errorTailRunes {
		return s
	}
	return "..." + string(r[len(r)-errorTailRunes:])
}

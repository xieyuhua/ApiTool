package pdfprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionsDefaults(t *testing.T) {
	var o Options
	if o.budget() != defaultVirtualTimeBudgetMS {
		t.Fatalf("默认预算 = %d, want %d", o.budget(), defaultVirtualTimeBudgetMS)
	}
	if o.tempPrefix() != defaultTempPrefix {
		t.Fatalf("默认临时目录前缀 = %q, want %q", o.tempPrefix(), defaultTempPrefix)
	}
	custom := Options{VirtualTimeBudgetMS: 15000, TempPrefix: "apitool-agent-export"}
	if custom.budget() != 15000 || custom.tempPrefix() != "apitool-agent-export" {
		t.Fatal("自定义参数未生效")
	}
}

func TestFileURL(t *testing.T) {
	got := fileURL(filepath.Join("tmp", "a b.html"))
	if len(got) < 8 || got[:7] != "file://" {
		t.Fatalf("fileURL = %q, 应以 file:// 开头", got)
	}
	if !strings.Contains(got, "%20") {
		t.Fatalf("fileURL = %q, 空格应转义为 %%20", got)
	}
}

func TestTailText(t *testing.T) {
	long := ""
	for i := 0; i < errorTailRunes+50; i++ {
		long += "字"
	}
	got := tailText(long)
	if len([]rune(got)) != errorTailRunes+3 { // "..." + 尾部 errorTailRunes 个字符
		t.Fatalf("tailText 长度 = %d, want %d", len([]rune(got)), errorTailRunes+3)
	}
	if tailText("短文本") != "短文本" {
		t.Fatal("tailText 不应截断短文本")
	}
}

// TestFromHTML 仅在装了 Chromium 内核浏览器时真正执行打印（否则跳过），
// 用于回归「HTML → PDF」链路是否可用。
func TestFromHTML(t *testing.T) {
	browser, err := FindBrowser()
	if err != nil {
		t.Skipf("本机无 Chromium 内核浏览器，跳过打印测试：%v", err)
	}
	t.Logf("使用浏览器：%s", browser)

	out := filepath.Join(t.TempDir(), "sample.pdf")
	html := `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">
<style>@page{size:A4;margin:12mm} body{font-family:"Microsoft YaHei"}</style></head>
<body><h1>导出链路自检</h1><p>Hello PDF</p></body></html>`
	if err := FromHTML(html, out, Options{VirtualTimeBudgetMS: 1500, TempPrefix: "apitool-pdf-test"}); err != nil {
		t.Fatalf("FromHTML 失败: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读取 PDF 失败: %v", err)
	}
	if len(b) < 1024 || string(b[:5]) != "%PDF-" {
		t.Fatalf("生成结果不是有效 PDF（%d 字节）", len(b))
	}
}

// TestFromHTMLConcurrent 验证并发导出（用户连点/批量导出）不会互相踩踏：
// 打印过程串行化 + 每次独立 user-data-dir，两份产物都应是有效 PDF。
func TestFromHTMLConcurrent(t *testing.T) {
	if _, err := FindBrowser(); err != nil {
		t.Skipf("本机无 Chromium 内核浏览器，跳过并发打印测试：%v", err)
	}
	dir := t.TempDir()
	html := `<!DOCTYPE html><html><head><meta charset="utf-8"></head><body><h1>并发导出</h1></body></html>`
	outs := []string{filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf")}

	errc := make(chan error, len(outs))
	for _, out := range outs {
		go func(target string) {
			errc <- FromHTML(html, target, Options{VirtualTimeBudgetMS: 800, TempPrefix: "apitool-pdf-test"})
		}(out)
	}
	for i := range outs {
		if err := <-errc; err != nil {
			t.Fatalf("第 %d 次并发导出失败: %v", i+1, err)
		}
	}
	for _, out := range outs {
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", out, err)
		}
		if len(b) < 512 || string(b[:5]) != "%PDF-" {
			t.Fatalf("%s 不是有效 PDF（%d 字节）", out, len(b))
		}
	}
}

package doc

// 测试报告的 Word（.docx）渲染：复用接口文档的 docx 基建
// （docxBuilder 段落/标题/表格/代码块 + packDOCX 打包 + 页眉 VML 水印）。

import (
	"fmt"
	"sort"
	"strings"

	"apitool/internal/model"
)

// BuildTestReportDOCX 把测试报告渲染为 .docx 字节流，按配置写入页眉水印。
func BuildTestReportDOCX(r model.TestReport, wm model.DocWatermark) ([]byte, error) {
	return BuildTestReportDOCXWithCover(r, wm, model.DocCover{})
}

// BuildTestReportDOCXWithCover 同上，并可选择为报告加上「作者封面」页。
func BuildTestReportDOCXWithCover(r model.TestReport, wm model.DocWatermark, cover model.DocCover) ([]byte, error) {
	b := &docxBuilder{}
	if cover.Enabled {
		if strings.TrimSpace(cover.Title) == "" {
			cover.Title = "接口测试报告"
		}
		b.cover(cover)
		b.pageBreak()
	}
	b.para("DocTitle", docxRun{Text: "接口测试报告"})

	meta := fmt.Sprintf("计划：%s · 生成时间：%s", r.PlanName, r.CreatedAt)
	if r.EnvName != "" {
		meta += " · 环境：" + r.EnvName
	}
	b.para("DocMeta", docxRun{Text: meta})

	// 统计概览
	b.table([]string{"用例总数", "通过", "失败", "总耗时(ms)"}, [][]docxCell{{
		{Text: fmt.Sprint(r.Total)}, {Text: fmt.Sprint(r.Passed)}, {Text: fmt.Sprint(r.Failed)},
		{Text: fmt.Sprint(r.DurationMs)},
	}}, []int{2400, 2400, 2400, 2438})

	// AI 分析摘要
	if r.Summary != "" {
		b.heading(1, "AI 分析摘要")
		b.text("", r.Summary)
	}

	// 用例结果总表
	b.heading(1, "用例结果")
	rows := make([][]docxCell, 0, len(r.Results))
	for _, res := range r.Results {
		label := "✗ 失败"
		if res.Passed {
			label = "✓ 通过"
		}
		rows = append(rows, []docxCell{
			{Text: res.CaseName},
			{Text: res.Category},
			{Text: fmt.Sprint(res.Status)},
			{Text: fmt.Sprintf("%d ms", res.DurationMs)},
			{Text: label},
		})
	}
	b.table([]string{"用例", "分类", "状态码", "耗时", "结果"}, rows, []int{2800, 1800, 1200, 1400, 2438})

	// 请求 / 响应明细
	if len(r.Results) > 0 {
		b.heading(1, "请求与响应明细")
	}
	for i, res := range r.Results {
		if i > 0 {
			b.pageBreak()
		}
		b.heading(2, res.CaseName)
		if res.Error != "" {
			b.para("", docxRun{Text: "请求错误：" + res.Error, Bold: true})
		}

		b.heading(3, "请求")
		b.para("DocURL", docxRun{Text: upperOrDash(res.RequestMethod) + "  " + res.RequestURL, Mono: true})
		b.kvTable("请求头", res.RequestHeaders)
		b.kvTable("查询参数", res.RequestQuery)
		if res.RequestBodyType == "form" {
			b.formTable("表单字段", res.RequestFormItems)
		} else if res.RequestBody != "" {
			b.text("", "请求体（"+orDash(res.RequestBodyType)+"）：")
			b.code(res.RequestBody)
		}

		b.heading(3, "响应")
		b.text("", fmt.Sprintf("状态码：%d %s · 耗时：%d ms · 大小：%d 字节",
			res.Status, res.StatusText, res.DurationMs, res.Size))
		b.kvTable("响应头", headerKVs(res.ResponseHeaders))
		if res.ResponseBody != "" {
			b.code(res.ResponseBody)
		}

		b.heading(3, "断言")
		if len(res.AssertionResults) == 0 {
			b.text("", "无断言（按 HTTP 2xx 判定）")
		} else {
			for _, ar := range res.AssertionResults {
				mark := "✓"
				if !ar.Passed {
					mark = "✗"
				}
				b.text("", fmt.Sprintf("%s %s —— %s", mark, ar.Description, ar.Detail))
			}
		}
	}

	return packDOCX("接口测试报告", b.body.String(), docxWatermarkMarkup(wm))
}

// headerKVs 把响应头 map 转为按名称排序的 KV 列表（让导出结果稳定可复现）。
func headerKVs(headers map[string]string) []model.KV {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]model.KV, 0, len(keys))
	for _, k := range keys {
		out = append(out, model.KV{Enabled: true, Key: k, Value: headers[k]})
	}
	return out
}

func upperOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return strings.ToUpper(s)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

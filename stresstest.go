package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// StressTarget 压测目标（来自测试用例或接口的请求定义）
type StressTarget struct {
	Name        string `json:"name"`
	Method      string `json:"method"`
	URL         string `json:"url"`
	Headers     []KV   `json:"headers"`
	Query       []KV   `json:"query"`
	BodyType    string `json:"bodyType"`
	Body        string `json:"body"`
	FormItems   []KV   `json:"formItems"`
	ContentType string `json:"contentType"`
}

// StressConfig 压测配置
type StressConfig struct {
	EnvID       string `json:"envId"`       // 运行环境（用于 {{变量}} 替换）
	Concurrency int    `json:"concurrency"` // 并发数（同时发起的请求数）
	Requests    int    `json:"requests"`    // 每个目标的请求次数
	TimeoutSec  int    `json:"timeoutSec"`
}

// StressResult 单个目标的压测结果
type StressResult struct {
	Name       string         `json:"name"`
	Method     string         `json:"method"`
	URL        string         `json:"url"`
	Total      int            `json:"total"`
	Success    int            `json:"success"`
	Failed     int            `json:"failed"`
	StatusDist map[string]int `json:"statusDist"` // 状态码分布，如 "200": 980
	ErrorDist  map[string]int `json:"errorDist"`  // 错误分布，如 "请求失败: ...": 5
	MinMs      int64          `json:"minMs"`
	MaxMs      int64          `json:"maxMs"`
	AvgMs      int64          `json:"avgMs"`
	P50        int64          `json:"p50"`
	P90        int64          `json:"p90"`
	P95        int64          `json:"p95"`
	P99        int64          `json:"p99"`
}

// StressReport 压测总报告
type StressReport struct {
	Total      int            `json:"total"`      // 总请求数
	Success    int            `json:"success"`    // 成功数（2xx/3xx 视为成功）
	Failed     int            `json:"failed"`     // 失败数
	DurationMs int64          `json:"durationMs"` // 总耗时（墙钟）
	RPS        float64        `json:"rps"`        // 吞吐：总请求数 / 总耗时（秒）
	Results    []StressResult `json:"results"`    // 各目标明细
}

// RunStressTest 对给定目标发起并发压测，返回含延迟分布与吞吐的报告。
// 通过运行时事件 "apitool:stress-progress" 推送进度 {done,total}。
func (a *App) RunStressTest(targets []StressTarget, config StressConfig) (StressReport, error) {
	report := StressReport{}
	if len(targets) == 0 {
		return report, fmt.Errorf("请至少选择一个压测目标")
	}
	reqs := config.Requests
	if reqs <= 0 {
		reqs = 100
	}
	if reqs > 100000 {
		reqs = 100000
	}
	conc := config.Concurrency
	if conc <= 0 {
		conc = 10
	}
	if conc > 500 {
		conc = 500
	}
	timeout := config.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}

	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return report, fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]

	env := []KV{}
	if config.EnvID != "" {
		for _, e := range proj.Environments {
			if e.ID == config.EnvID {
				env = enabledEnvVars(e.Vars)
				break
			}
		}
	}

	// 构建任务队列：每个目标复制 reqs 份
	type job struct{ ti int }
	jobs := make([]job, 0, len(targets)*reqs)
	for ti := range targets {
		for i := 0; i < reqs; i++ {
			jobs = append(jobs, job{ti: ti})
		}
	}
	total := len(jobs)

	type agg struct {
		total      int
		success    int
		failed     int
		statusDist map[string]int
		errorDist  map[string]int
		durations  []int64
	}
	aggs := make([]*agg, len(targets))
	for i := range aggs {
		aggs[i] = &agg{statusDist: map[string]int{}, errorDist: map[string]int{}}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, conc)
	var progMu sync.Mutex
	done := 0
	lastEmit := time.Now()
	start := time.Now()

	for _, jb := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			t := targets[j.ti]
			spec := RequestSpec{
				Method:      t.Method,
				URL:         t.URL,
				Headers:     t.Headers,
				Query:       t.Query,
				BodyType:    t.BodyType,
				Body:        t.Body,
				FormItems:   t.FormItems,
				TimeoutSec:  timeout,
				Env:         env,
				ContentType: t.ContentType,
			}
			mergeCommon(&spec, proj.Common)
			resp := a.SendRequest(spec)

			progMu.Lock()
			ag := aggs[j.ti]
			ag.total++
			if resp.Error != "" {
				ag.failed++
				ag.errorDist[resp.Error]++
			} else {
				ag.success++
				ag.statusDist[strconv.Itoa(resp.Status)]++
				ag.durations = append(ag.durations, resp.DurationMs)
			}
			done++
			now := time.Now()
			if now.Sub(lastEmit) > 300*time.Millisecond || done == total {
				runtime.EventsEmit(a.ctx, "apitool:stress-progress", map[string]interface{}{
					"done":  done,
					"total": total,
				})
				lastEmit = now
			}
			progMu.Unlock()
		}(jb)
	}
	wg.Wait()

	wallMs := time.Since(start).Milliseconds()
	report.Total = total
	report.DurationMs = wallMs
	if wallMs > 0 {
		report.RPS = float64(total) / (float64(wallMs) / 1000.0)
	}
	results := make([]StressResult, len(targets))
	for i, t := range targets {
		ag := aggs[i]
		r := StressResult{
			Name:       t.Name,
			Method:     t.Method,
			URL:        t.URL,
			Total:      ag.total,
			Success:    ag.success,
			Failed:     ag.failed,
			StatusDist: ag.statusDist,
			ErrorDist:  ag.errorDist,
		}
		if len(ag.durations) > 0 {
			ds := append([]int64(nil), ag.durations...)
			sort.Slice(ds, func(x, y int) bool { return ds[x] < ds[y] })
			var sum int64
			for _, v := range ds {
				sum += v
			}
			r.MinMs = ds[0]
			r.MaxMs = ds[len(ds)-1]
			r.AvgMs = sum / int64(len(ds))
			r.P50 = percentile(ds, 50)
			r.P90 = percentile(ds, 90)
			r.P95 = percentile(ds, 95)
			r.P99 = percentile(ds, 99)
		}
		results[i] = r
		report.Success += ag.success
		report.Failed += ag.failed
	}
	report.Results = results
	return report, nil
}

// percentile 在已排序的切片上取第 p 百分位（最近秩法）
func percentile(sorted []int64, p int) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(float64(p) / 100.0 * float64(n))
	if idx >= n {
		idx = n - 1
	}
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// ---------------- 压测报告导出 ----------------

func statusDistText(m map[string]int) string {
	if len(m) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, "  ")
}

func stressMarkdown(r StressReport) string {
	var sb strings.Builder
	sb.WriteString("# 压力测试报告\n\n")
	fmt.Fprintf(&sb, "- 生成时间：%s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "- 总请求：%d，成功：%d，失败：%d\n", r.Total, r.Success, r.Failed)
	fmt.Fprintf(&sb, "- 吞吐（RPS）：%.1f\n", r.RPS)
	fmt.Fprintf(&sb, "- 总耗时：%d ms\n\n", r.DurationMs)
	sb.WriteString("| 目标 | 成功率 | 最小(ms) | 平均(ms) | P95 | P99 | 失败 | 状态码分布 |\n|---|---|---|---|---|---|---|---|\n")
	for _, res := range r.Results {
		rate := 0.0
		if res.Total > 0 {
			rate = float64(res.Success) / float64(res.Total) * 100
		}
		sb.WriteString(fmt.Sprintf("| %s | %.1f%% | %d | %d | %d | %d | %d | %s |\n",
			mdEscape(res.Name), rate, res.MinMs, res.AvgMs, res.P95, res.P99, res.Failed,
			mdEscape(statusDistText(res.StatusDist))))
	}
	return sb.String()
}

func stressHTML(r StressReport) string {
	var stats strings.Builder
	fmt.Fprintf(&stats, `<div class="stats">
<div class="stat tot"><div class="n">%d</div><div>总请求</div></div>
<div class="stat ok"><div class="n">%d</div><div>成功</div></div>
<div class="stat fail"><div class="n">%d</div><div>失败</div></div>
<div class="stat"><div class="n">%.1f</div><div>吞吐 RPS</div></div>
<div class="stat"><div class="n">%d ms</div><div>总耗时</div></div></div>`,
		r.Total, r.Success, r.Failed, r.RPS, r.DurationMs)

	var rows strings.Builder
	for _, res := range r.Results {
		rate := 0.0
		if res.Total > 0 {
			rate = float64(res.Success) / float64(res.Total) * 100
		}
		cls := "pass"
		if res.Failed > 0 {
			cls = "fail"
		}
		rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td class="%s">%.1f%%</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%s</td></tr>`,
			htmlEscape(res.Name), cls, rate, res.MinMs, res.AvgMs, res.P95, res.P99, res.Failed,
			htmlEscape(statusDistText(res.StatusDist))))
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>压力测试报告</title><style>%s</style></head>
<body><div class="wrap">
<h1>压力测试报告</h1>
<div class="meta">生成时间：%s</div>
%s
<div class="card"><h2>目标明细</h2>
<table><thead><tr><th>目标</th><th>成功率</th><th>最小(ms)</th><th>平均(ms)</th><th>P95</th><th>P99</th><th>失败</th><th>状态码分布</th></tr></thead><tbody>%s</tbody></table></div>
</div></body></html>`,
		reportCSS, time.Now().Format("2006-01-02 15:04:05"), stats.String(), rows.String())
}

// ExportStressReport 将压测报告（JSON）导出为 Markdown / HTML 文件，返回保存路径
func (a *App) ExportStressReport(reportJSON string, format string) (string, error) {
	var r StressReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	var content, ext, filter string
	switch format {
	case "html":
		content = stressHTML(r)
		ext, filter = ".html", "HTML (*.html)|*.html"
	case "markdown", "":
		content = stressMarkdown(r)
		ext, filter = ".md", "Markdown (*.md)|*.md"
	default:
		return "", fmt.Errorf("不支持的格式: %s", format)
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出压测报告",
		DefaultFilename: "stress-report-" + time.Now().Format("20060102-150405") + ext,
		Filters: []runtime.FileFilter{
			{DisplayName: filter, Pattern: "*" + ext},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if len(content) > 20<<20 {
		content = content[:20<<20]
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------------- AI 生成测试用例 ----------------

// buildApiBrief 将接口信息压缩为适合喂给 AI 的精简结构（含字段示例与响应结构）
func buildApiBrief(api ApiInfo, common CommonParams, envKeys []string) string {
	brief := map[string]interface{}{
		"name":        api.Name,
		"description": api.Description,
		"method":      api.Method,
		"url":         api.URL,
		"query":       enabledKVs(api.Query),
		"headers":     enabledKVs(api.Headers),
		"reqFields":   api.ReqFields,
		"respFields":  api.RespFields,
		"body":        api.Body,
		// 公共参数：执行时自动附加到所有请求，接口同名覆盖公共
		"commonHeaders": enabledKVs(common.Headers),
		"commonQuery":   enabledKVs(common.Query),
		// 可用环境变量名（执行时按 {{变量名}} 替换，值为机密不提供）
		"availableEnvVars": envKeys,
	}
	b, _ := json.Marshal(brief)
	return string(b)
}

// genCasesForApi 针对单个接口调用 AI 生成测试用例
func genCasesForApi(s Settings, api ApiInfo, common CommonParams, envKeys []string) ([]TestCase, error) {
	brief := buildApiBrief(api, common, envKeys)
	system := `你是一名资深 API 测试专家。根据提供的接口信息，生成覆盖全面的自动化测试用例。
每个用例必须是完整可独立执行的（包含 method/url/headers/query/bodyType/body 与断言 assertions），
请求参数要真实可跑（优先使用字段示例值，缺失时给出合理默认值）。
断言用于校验响应是否符合预期。
注意：接口信息中的 "commonHeaders" / "commonQuery" 是项目级公共参数，执行时会自动附加到所有请求，
你无需在用例中重复它们；若需覆盖，请显式设置同名参数。请求地址与参数均支持 {{变量名}} 占位，
执行时按当前环境变量替换（可用变量见 "availableEnvVars"）。`
	user := fmt.Sprintf(`接口信息（JSON）：
%s

请生成测试用例，并只返回一个 JSON 对象，结构如下（不要输出任何额外说明文字）：
{
  "cases": [
    {
      "name": "用例名称",
      "category": "正常流程 | 参数边界 | 异常场景 | 权限安全",
      "description": "用例简述",
      "method": "POST",
      "url": "接口地址（可含 {{变量}}）",
      "headers": [{"key":"Content-Type","value":"application/json"}],
      "query": [{"key":"page","value":"1"}],
      "bodyType": "json",
      "body": "请求体原始字符串，json 类型时放 JSON 文本",
      "assertions": [
        {"type":"status","target":"","operator":"eq","expected":"200"},
        {"type":"json","target":"$.code","operator":"eq","expected":"0"},
        {"type":"bodyContains","target":"","operator":"contains","expected":"success"}
      ]
    }
  ]
}

要求：
1. 至少包含 1 个「正常流程」用例，并尽量补充「参数边界」「异常场景」「权限安全」用例，总计 4~8 个。
2. assertions 的 type 取值：
   - status：响应状态码
   - json：按 JSONPath 取响应字段（target 指定路径，如 $.code、$.data.list[0].id）
   - bodyContains：响应体包含文本
   - header：响应头（target 指定头名）
   - duration：响应耗时（单位毫秒，operator 用 gt/lt/gte/lte）
   - contentType：响应 Content-Type（如 application/json）
   - cookie：响应 Set-Cookie（target 为空，expected 为 cookie 名或 名=值 片段）
   - regex：响应体正则匹配（expected 为正则，eq/contains 表示必须匹配，ne 表示必须不匹配）
   - size：响应体字节大小（operator 用 gt/lt/gte/lte）
3. operator 取值：eq / ne / gt / gte / lt / lte / contains / exists / isTrue / isFalse。
4. json 类型用 target 指定 JSONPath；header/cookie/contentType 的 target 用法见上。
5. 正常用例务必断言 status eq 200（或接口实际成功码），并尽量用 json 断言校验关键业务字段；涉及鉴权/下载/大响应时可用 contentType、cookie、size、regex 等类型。`, brief)

	raw, err := aiChat(s, system, user)
	if err != nil {
		return nil, err
	}
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("AI 未返回有效的 JSON 结果")
	}
	var parsed struct {
		Cases []struct {
			Name        string      `json:"name"`
			Category    string      `json:"category"`
			Description string      `json:"description"`
			Method      string      `json:"method"`
			URL         string      `json:"url"`
			Headers     []KV        `json:"headers"`
			Query       []KV        `json:"query"`
			BodyType    string      `json:"bodyType"`
			Body        string      `json:"body"`
			Assertions  []Assertion `json:"assertions"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("AI 用例结果解析失败: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	out := make([]TestCase, 0, len(parsed.Cases))
	for _, c := range parsed.Cases {
		cat := c.Category
		if cat == "" {
			cat = "正常流程"
		}
		// 断言默认启用
		for i := range c.Assertions {
			c.Assertions[i].Enabled = true
		}
		out = append(out, TestCase{
			ID:          genID(),
			ApiID:       api.ID,
			ApiName:     api.Name,
			Category:    cat,
			Name:        c.Name,
			Description: c.Description,
			Method:      strings.ToUpper(c.Method),
			URL:         c.URL,
			Headers:     c.Headers,
			Query:       c.Query,
			BodyType:    c.BodyType,
			Body:        c.Body,
			Assertions:  c.Assertions,
			Enabled:     true,
			CreatedAt:   now,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("AI 未生成任何用例")
	}
	return out, nil
}

// GenerateTestCases 为指定接口生成测试用例（基于现有接口或已导入的 OpenAPI 接口）
func (a *App) GenerateTestCases(apiID string) ([]TestCase, error) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return nil, fmt.Errorf("没有可用的项目")
	}
	if strings.TrimSpace(data.Settings.AIKey) == "" {
		return nil, fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	var api *ApiInfo
	for i := range data.Projects[idx].Apis {
		if data.Projects[idx].Apis[i].ID == apiID {
			api = &data.Projects[idx].Apis[i]
			break
		}
	}
	if api == nil {
		return nil, fmt.Errorf("未找到指定的接口")
	}
	envKeys := activeEnvKeys(data, idx)
	return genCasesForApi(data.Settings, *api, data.Projects[idx].Common, envKeys)
}

// GenerateTestCasesForApis 批量生成（可先导入 OpenAPI，再对多个接口生成）
func (a *App) GenerateTestCasesForApis(apiIDs []string) ([]TestCase, error) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return nil, fmt.Errorf("没有可用的项目")
	}
	if strings.TrimSpace(data.Settings.AIKey) == "" {
		return nil, fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	idSet := map[string]bool{}
	for _, id := range apiIDs {
		idSet[id] = true
	}
	var out []TestCase
	common := data.Projects[idx].Common
	envKeys := activeEnvKeys(data, idx)
	for i := range data.Projects[idx].Apis {
		api := data.Projects[idx].Apis[i]
		if !idSet[api.ID] {
			continue
		}
		cases, err := genCasesForApi(data.Settings, api, common, envKeys)
		if err != nil {
			return out, err
		}
		out = append(out, cases...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有可生成用例的接口")
	}
	return out, nil
}

// ---------------- AI 生成测试用例（异步队列 + 进度） ----------------

// 生成任务队列：前端提交任务后立即返回 jobId，后台 worker 顺序消费并推送进度事件。
var (
	genJobCh      = make(chan genJobReq, 64)
	genWorkerOnce sync.Once
)

type genJobReq struct {
	jobID  string
	apiIDs []string
	projID string
}

// GenerateTestCasesAsync 将生成任务放入异步队列，前端通过运行时事件接收进度与结果。
// 返回任务 ID（jobId），前端据此过滤事件。
func (a *App) GenerateTestCasesAsync(apiIDs []string) (string, error) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return "", fmt.Errorf("没有可用的项目")
	}
	if strings.TrimSpace(data.Settings.AIKey) == "" {
		return "", fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	if len(apiIDs) == 0 {
		return "", fmt.Errorf("请至少选择一个接口")
	}
	genWorkerOnce.Do(func() { go a.genWorker() })
	jobID := genID()
	genJobCh <- genJobReq{jobID: jobID, apiIDs: apiIDs, projID: data.CurrentProjectID}
	return jobID, nil
}

func (a *App) genWorker() {
	for job := range genJobCh {
		a.runGenJob(job)
	}
}

func (a *App) runGenJob(job genJobReq) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		a.emitGenError(job.jobID, "没有可用的项目")
		return
	}
	// 定位任务所属项目（若已切换则用当前项目兜底）
	projIdx := idx
	for i, p := range data.Projects {
		if p.ID == job.projID {
			projIdx = i
			break
		}
	}
	proj := data.Projects[projIdx]
	common := proj.Common
	envKeys := activeEnvKeys(data, projIdx)

	total := len(job.apiIDs)
	done := 0
	allCases := []TestCase{}
	for _, id := range job.apiIDs {
		var api *ApiInfo
		for i := range proj.Apis {
			if proj.Apis[i].ID == id {
				api = &proj.Apis[i]
				break
			}
		}
		if api == nil {
			done++
			a.emitGenProgress(job.jobID, total, done, "", "skipped")
			continue
		}
		a.emitGenProgress(job.jobID, total, done, api.Name, "generating")
		cases, err := genCasesForApi(data.Settings, *api, common, envKeys)
		done++
		if err != nil {
			a.emitGenProgress(job.jobID, total, done, api.Name, "error:"+err.Error())
			continue
		}
		allCases = append(allCases, cases...)
		a.emitGenProgress(job.jobID, total, done, api.Name, "ok")
	}
	runtime.EventsEmit(a.ctx, "apitool:gen-done", map[string]interface{}{
		"jobId": job.jobID,
		"total": total,
		"cases": allCases,
	})
}

func (a *App) emitGenProgress(jobID string, total, done int, name, phase string) {
	runtime.EventsEmit(a.ctx, "apitool:gen-progress", map[string]interface{}{
		"jobId": jobID,
		"total": total,
		"done":  done,
		"name":  name,
		"phase": phase,
	})
}

func (a *App) emitGenError(jobID string, msg string) {
	runtime.EventsEmit(a.ctx, "apitool:gen-error", map[string]interface{}{
		"jobId": jobID,
		"error": msg,
	})
}

// ---------------- 执行引擎 ----------------

func enabledEnvVars(vars []EnvVar) []KV {
	out := []KV{}
	for _, v := range vars {
		if v.Enabled && v.Key != "" {
			out = append(out, KV{Key: v.Key, Value: v.Value, Enabled: true})
		}
	}
	return out
}

// activeEnvKeys 返回当前项目激活环境下的环境变量名（不包含值，避免泄露机密）
func activeEnvKeys(data AppData, idx int) []string {
	proj := data.Projects[idx]
	if proj.ActiveEnvID == "" {
		return nil
	}
	for _, e := range proj.Environments {
		if e.ID == proj.ActiveEnvID {
			keys := []string{}
			for _, v := range e.Vars {
				if v.Enabled && v.Key != "" {
					keys = append(keys, v.Key)
				}
			}
			return keys
		}
	}
	return nil
}

// mergeCommon 将项目公共参数合并进请求规格（用例/接口自身同名参数优先覆盖公共）
func mergeCommon(spec *RequestSpec, common CommonParams) {
	hm := map[string]KV{}
	for _, h := range common.Headers {
		if h.Enabled && h.Key != "" {
			hm[strings.ToLower(h.Key)] = h
		}
	}
	for _, h := range spec.Headers {
		if h.Enabled && h.Key != "" {
			hm[strings.ToLower(h.Key)] = h
		}
	}
	spec.Headers = []KV{}
	for _, v := range hm {
		spec.Headers = append(spec.Headers, v)
	}
	qm := map[string]KV{}
	for _, q := range common.Query {
		if q.Enabled && q.Key != "" {
			qm[strings.ToLower(q.Key)] = q
		}
	}
	for _, q := range spec.Query {
		if q.Enabled && q.Key != "" {
			qm[strings.ToLower(q.Key)] = q
		}
	}
	spec.Query = []KV{}
	for _, v := range qm {
		spec.Query = append(spec.Query, v)
	}
}

// jsonPathValue 从响应体 JSON 中按简化 JSONPath 取值（支持 a.b.c 与 a.b[0].c）
func jsonPathValue(body string, path string) (interface{}, error) {
	var data interface{}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, fmt.Errorf("响应体为空")
	}
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return nil, fmt.Errorf("响应体不是合法 JSON")
	}
	p := strings.TrimSpace(path)
	p = strings.TrimPrefix(p, "$")
	p = strings.TrimPrefix(p, ".")
	p = strings.TrimPrefix(p, ".")
	if p == "" {
		return data, nil
	}
	re := regexp.MustCompile(`([^.\[\]]+)|\[(-?\d+)\]`)
	cur := data
	for _, m := range re.FindAllStringSubmatch(p, -1) {
		if m[1] != "" {
			key := m[1]
			obj, ok := cur.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("路径 %s 处不是对象", key)
			}
			v, exists := obj[key]
			if !exists {
				return nil, fmt.Errorf("字段 %s 不存在", key)
			}
			cur = v
		} else if m[2] != "" {
			idx, _ := strconv.Atoi(m[2])
			arr, ok := cur.([]interface{})
			if !ok {
				return nil, fmt.Errorf("路径处不是数组")
			}
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("数组索引越界 %d", idx)
			}
			cur = arr[idx]
		}
	}
	return cur, nil
}

// compareValues 按操作符比较实际值与期望值
func compareValues(actual, expected, op string) (bool, error) {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	switch op {
	case "contains":
		return strings.Contains(actual, expected), nil
	case "exists":
		return true, nil
	case "isTrue":
		return strings.EqualFold(actual, "true") || actual == "1", nil
	case "isFalse":
		return strings.EqualFold(actual, "false") || actual == "0", nil
	}
	af, aerr := strconv.ParseFloat(actual, 64)
	ef, eerr := strconv.ParseFloat(expected, 64)
	numeric := aerr == nil && eerr == nil
	switch op {
	case "eq":
		if numeric {
			return af == ef, nil
		}
		return actual == expected, nil
	case "ne":
		if numeric {
			return af != ef, nil
		}
		return actual != expected, nil
	case "gt":
		if !numeric {
			return false, fmt.Errorf("非数值无法比较")
		}
		return af > ef, nil
	case "gte":
		if !numeric {
			return false, fmt.Errorf("非数值无法比较")
		}
		return af >= ef, nil
	case "lt":
		if !numeric {
			return false, fmt.Errorf("非数值无法比较")
		}
		return af < ef, nil
	case "lte":
		if !numeric {
			return false, fmt.Errorf("非数值无法比较")
		}
		return af <= ef, nil
	default:
		return false, fmt.Errorf("未知操作符 %s", op)
	}
}

// assertionLabel 返回断言类型的可读名（用于 UI 与提示）
func assertionLabel(t string) string {
	switch t {
	case "status":
		return "状态码"
	case "json":
		return "JSON 字段"
	case "bodyContains":
		return "响应体包含"
	case "header":
		return "响应头"
	case "duration":
		return "响应耗时"
	case "contentType":
		return "Content-Type"
	case "cookie":
		return "Cookie"
	case "regex":
		return "正则匹配"
	case "size":
		return "响应体积"
	default:
		return t
	}
}

// compareSimple 用 compareValues 比较实际值/期望值，复用操作符语义
func compareSimple(actual string, as Assertion, desc string) AssertionResult {
	ar := AssertionResult{Description: desc}
	ok, err := compareValues(actual, as.Expected, as.Operator)
	if err != nil {
		ar.Passed = false
		ar.Detail = "比较失败：" + err.Error()
		return ar
	}
	ar.Passed = ok
	ar.Detail = fmt.Sprintf("实际值=%q，期望值=%q", actual, as.Expected)
	return ar
}

// evaluateAssertion 评估单条断言，返回结果
// 支持的断言类型：status / json / bodyContains / header / duration / contentType / cookie / regex / size
func evaluateAssertion(resp ResponseData, as Assertion) AssertionResult {
	desc := assertionLabel(as.Type)
	if as.Target != "" {
		desc += " " + as.Target
	}
	desc += " " + as.Operator + " " + as.Expected

	switch as.Type {
	case "status":
		return compareSimple(strconv.Itoa(resp.Status), as, desc)
	case "duration":
		return compareSimple(strconv.FormatInt(resp.DurationMs, 10), as, desc)
	case "size":
		return compareSimple(strconv.FormatInt(resp.Size, 10), as, desc)
	case "header":
		actual := ""
		for k, v := range resp.Headers {
			if strings.EqualFold(k, as.Target) {
				actual = v
				break
			}
		}
		return compareSimple(actual, as, desc)
	case "contentType":
		return compareSimple(resp.Headers["Content-Type"], as, desc)
	case "cookie":
		return compareSimple(resp.Headers["Set-Cookie"], as, desc)
	case "bodyContains":
		return compareSimple(resp.Body, as, desc)
	case "regex":
		re, err := regexp.Compile(as.Expected)
		if err != nil {
			return AssertionResult{Description: desc, Passed: false, Detail: "正则编译失败：" + err.Error()}
		}
		matched := re.MatchString(resp.Body)
		// eq/contains/isTrue 表示「必须匹配」；ne/isFalse 表示「必须不匹配」
		ok := matched
		switch as.Operator {
		case "ne", "isFalse":
			ok = !matched
		}
		detail := "正则未匹配"
		if matched {
			detail = "正则匹配成功"
		}
		if as.Operator == "ne" || as.Operator == "isFalse" {
			detail += "（要求不匹配）"
		} else {
			detail += "（要求匹配）"
		}
		return AssertionResult{Description: desc, Passed: ok, Detail: detail}
	case "json":
		v, err := jsonPathValue(resp.Body, as.Target)
		if err != nil {
			return AssertionResult{Description: desc, Passed: false, Detail: "取值失败：" + err.Error()}
		}
		return compareSimple(fmt.Sprintf("%v", v), as, desc)
	default:
		return AssertionResult{Description: desc, Passed: false, Detail: "不支持的断言类型：" + as.Type}
	}
}

// runCase 执行单个用例并返回结果
func (a *App) runCase(c TestCase, env []KV, common CommonParams, timeout int) TestResult {
	res := TestResult{
		CaseID:     c.ID,
		CaseName:   c.Name,
		Category:   c.Category,
		DurationMs: 0,
	}
	spec := RequestSpec{
		Method:      c.Method,
		URL:         c.URL,
		Headers:     c.Headers,
		Query:       c.Query,
		BodyType:    c.BodyType,
		Body:        c.Body,
		FormItems:   c.FormItems,
		TimeoutSec:  timeout,
		Env:         env,
		ContentType: c.ContentType,
	}
	// 合并项目公共参数（用例同名优先），公共参数中的 {{变量}} 也会按环境变量替换
	mergeCommon(&spec, common)
	resp := a.SendRequest(spec)
	res.Status = resp.Status
	res.DurationMs = resp.DurationMs
	res.ResponseBody = resp.Body
	if resp.Error != "" {
		res.Error = resp.Error
		res.Passed = false
		return res
	}
	passed := true
	hasAssertion := false
	for _, as := range c.Assertions {
		if !as.Enabled {
			continue
		}
		hasAssertion = true
		ar := evaluateAssertion(resp, as)
		res.AssertionResults = append(res.AssertionResults, ar)
		if !ar.Passed {
			passed = false
		}
	}
	if !hasAssertion {
		// 无断言时，仅以 HTTP 2xx 视为通过
		passed = resp.Status >= 200 && resp.Status < 300
	}
	res.Passed = passed
	return res
}

// RunTestCases 执行指定用例（可跨计划），支持并发执行（并发数 concurrency<=0 时退化为串行）。
// 返回测试报告（不持久化，由前端负责保存）。结果按入参用例顺序返回。
func (a *App) RunTestCases(caseIDs []string, envID string, concurrency int) (TestReport, error) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return TestReport{}, fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]

	var env []KV
	if envID != "" {
		for _, e := range proj.Environments {
			if e.ID == envID {
				env = enabledEnvVars(e.Vars)
				break
			}
		}
	}

	caseMap := map[string]TestCase{}
	for _, c := range proj.TestCases {
		caseMap[c.ID] = c
	}

	// 按入参顺序收集待执行用例
	order := make([]TestCase, 0, len(caseIDs))
	for _, id := range caseIDs {
		c, ok := caseMap[id]
		if !ok || !c.Enabled {
			continue
		}
		order = append(order, c)
	}

	report := TestReport{
		ID:        genID(),
		PlanID:    "",
		PlanName:  "手动执行",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	if len(order) == 0 {
		return report, nil
	}

	results := make([]TestResult, len(order))
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(order) {
		concurrency = len(order)
	}

	start := time.Now()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, c := range order {
		wg.Add(1)
		sem <- struct{}{} // 获取并发额度
		go func(idx int, tc TestCase) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = a.runCase(tc, env, proj.Common, data.Settings.TimeoutSec)
		}(i, c)
	}
	wg.Wait()
	report.Results = results
	report.DurationMs = time.Since(start).Milliseconds()
	report.Total = len(results)
	for _, r := range results {
		if r.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report, nil
}

// RunTestPlan 执行指定测试计划（并发数取计划配置的 Concurrency）
func (a *App) RunTestPlan(planID string) (TestReport, error) {
	data := a.readData()
	idx := activeProjectIndex(data)
	if idx < 0 {
		return TestReport{}, fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]
	var plan *TestPlan
	for i := range proj.TestPlans {
		if proj.TestPlans[i].ID == planID {
			plan = &proj.TestPlans[i]
			break
		}
	}
	if plan == nil {
		return TestReport{}, fmt.Errorf("未找到指定的测试计划")
	}
	conc := plan.Concurrency
	if conc <= 0 {
		conc = 1
	}
	report, err := a.RunTestCases(plan.CaseIDs, plan.EnvID, conc)
	if err != nil {
		return report, err
	}
	report.PlanID = plan.ID
	report.PlanName = plan.Name
	return report, nil
}

// ---------------- AI 分析报告 ----------------

// GenerateReportSummary 根据测试报告（JSON）调用 AI 生成中文分析摘要
func (a *App) GenerateReportSummary(reportJSON string) (string, error) {
	data := a.readData()
	if strings.TrimSpace(data.Settings.AIKey) == "" {
		return "", fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	system := `你是一名资深测试分析师。根据提供的接口测试报告（JSON），用简体中文输出一份测试分析摘要。
要求：分点清晰，聚焦风险与改进建议，避免复述原始数据。`
	user := fmt.Sprintf(`测试报告（JSON）：
%s

请基于以上报告输出分析摘要，建议包含：
1. 总体评价（通过率、稳定性）。
2. 主要风险点与失败原因归类。
3. 改进与回归建议。
不要使用 JSON，直接输出 Markdown 文本。`, reportJSON)

	return aiChat(data.Settings, system, user)
}

// ---------------- 报告导出 ----------------

func reportMarkdown(r TestReport) string {
	var sb strings.Builder
	sb.WriteString("# 接口测试报告\n\n")
	fmt.Fprintf(&sb, "- 计划：%s\n", r.PlanName)
	fmt.Fprintf(&sb, "- 生成时间：%s\n", r.CreatedAt)
	fmt.Fprintf(&sb, "- 用例总数：%d，通过：%d，失败：%d\n", r.Total, r.Passed, r.Failed)
	fmt.Fprintf(&sb, "- 总耗时：%d ms\n\n", r.DurationMs)

	if r.Summary != "" {
		sb.WriteString("## AI 分析摘要\n\n")
		sb.WriteString(r.Summary + "\n\n")
	}

	sb.WriteString("## 用例结果\n\n")
	sb.WriteString("| 用例 | 分类 | 状态 | 耗时 | 结果 |\n|---|---|---|---|---|\n")
	for _, res := range r.Results {
		status := "失败"
		if res.Passed {
			status = "通过"
		}
		errMsg := res.Error
		if errMsg != "" {
			errMsg = "（" + errMsg + "）"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %d | %d ms | %s%s |\n",
			mdEscape(res.CaseName), mdEscape(res.Category), res.Status, res.DurationMs, status, mdEscape(errMsg)))
	}

	sb.WriteString("\n## 断言明细\n\n")
	for _, res := range r.Results {
		sb.WriteString(fmt.Sprintf("### %s\n", mdEscape(res.CaseName)))
		if res.Error != "" {
			sb.WriteString("- 请求错误：" + mdEscape(res.Error) + "\n")
		}
		if len(res.AssertionResults) == 0 {
			sb.WriteString("- 无断言\n")
		}
		for _, ar := range res.AssertionResults {
			mark := "✓"
			if !ar.Passed {
				mark = "✗"
			}
			sb.WriteString(fmt.Sprintf("- %s %s —— %s\n", mark, mdEscape(ar.Description), mdEscape(ar.Detail)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

const reportCSS = `
:root{color-scheme:light}
*{box-sizing:border-box}body{margin:0;font-family:"Segoe UI","Microsoft YaHei",sans-serif;color:#1f2329;background:#f7f8fa}
.wrap{max-width:1100px;margin:0 auto;padding:32px 28px}
h1{font-size:26px;margin:0 0 6px}
.meta{color:#86909c;font-size:13px;margin-bottom:18px}
.stats{display:flex;gap:14px;flex-wrap:wrap;margin:18px 0}
.stat{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:14px 20px;min-width:120px}
.stat .n{font-size:24px;font-weight:700}
.stat.ok .n{color:#00b42a}.stat.fail .n{color:#f53f3f}.stat.tot .n{color:#165dff}
.card{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:18px 22px;margin:16px 0}
.card h2{margin:0 0 12px;font-size:18px}
.summary{white-space:pre-wrap;line-height:1.8;font-size:14px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{border:1px solid #e5e6eb;padding:8px 12px;text-align:left;word-break:break-all}
th{background:#f7f8fa;font-weight:600}
.pass{color:#00b42a;font-weight:600}.fail{color:#f53f3f;font-weight:600}
.tag{display:inline-block;font-size:11px;padding:1px 8px;border-radius:10px;background:#f2f3f5;color:#4e5969;margin-right:6px}
.assert{font-size:12px;padding:3px 0;border-bottom:1px dashed #eee}
.assert .ok{color:#00b42a}.assert .no{color:#f53f3f}
`

func reportHTML(r TestReport) string {
	var stats strings.Builder
	fmt.Fprintf(&stats, `<div class="stats">
<div class="stat tot"><div class="n">%d</div><div>用例总数</div></div>
<div class="stat ok"><div class="n">%d</div><div>通过</div></div>
<div class="stat fail"><div class="n">%d</div><div>失败</div></div>
<div class="stat"><div class="n">%d ms</div><div>总耗时</div></div></div>`,
		r.Total, r.Passed, r.Failed, r.DurationMs)

	var summary strings.Builder
	if r.Summary != "" {
		summary.WriteString(`<div class="card"><h2>AI 分析摘要</h2><div class="summary">` + htmlEscape(r.Summary) + `</div></div>`)
	}

	var rows strings.Builder
	for _, res := range r.Results {
		cls := "pass"
		label := "通过"
		if !res.Passed {
			cls = "fail"
			label = "失败"
		}
		errMsg := ""
		if res.Error != "" {
			errMsg = ` <span style="color:#f53f3f">（` + htmlEscape(res.Error) + `）</span>`
		}
		rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%d</td><td>%d ms</td><td class="%s">%s%s</td></tr>`,
			htmlEscape(res.CaseName), htmlEscape(res.Category), res.Status, res.DurationMs, cls, label, errMsg))
	}

	var details strings.Builder
	for _, res := range r.Results {
		details.WriteString(fmt.Sprintf(`<div class="card"><h2>%s</h2>`, htmlEscape(res.CaseName)))
		if res.Error != "" {
			details.WriteString(`<div class="assert"><span class="no">✗</span> 请求错误：` + htmlEscape(res.Error) + `</div>`)
		}
		if len(res.AssertionResults) == 0 {
			details.WriteString(`<div class="assert">无断言</div>`)
		}
		for _, ar := range res.AssertionResults {
			mark := `<span class="ok">✓</span>`
			if !ar.Passed {
				mark = `<span class="no">✗</span>`
			}
			details.WriteString(fmt.Sprintf(`<div class="assert">%s %s —— %s</div>`,
				mark, htmlEscape(ar.Description), htmlEscape(ar.Detail)))
		}
		details.WriteString(`</div>`)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>接口测试报告 - %s</title><style>%s</style></head>
<body><div class="wrap">
<h1>接口测试报告</h1>
<div class="meta">计划：%s · 生成时间：%s</div>
%s
%s
<div class="card"><h2>用例结果</h2>
<table><thead><tr><th>用例</th><th>分类</th><th>状态码</th><th>耗时</th><th>结果</th></tr></thead><tbody>%s</tbody></table></div>
%s
</div></body></html>`,
		htmlEscape(r.PlanName), reportCSS, htmlEscape(r.PlanName), htmlEscape(r.CreatedAt),
		stats.String(), summary.String(), rows.String(), details.String())
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

// BuildReportHTMLContent 将测试报告（JSON）渲染为独立 HTML 源码，供文档中心预览/导出/分享。
func (a *App) BuildReportHTMLContent(reportJSON string) (string, error) {
	var r TestReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	return reportHTML(r), nil
}

// ExportTestReport 将测试报告导出为 Markdown / HTML 文件，返回保存路径
func (a *App) ExportTestReport(reportJSON string, format string) (string, error) {
	var r TestReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	var content, ext, filter string
	switch format {
	case "html":
		content = reportHTML(r)
		ext, filter = ".html", "HTML (*.html)|*.html"
	case "markdown", "":
		content = reportMarkdown(r)
		ext, filter = ".md", "Markdown (*.md)|*.md"
	default:
		return "", fmt.Errorf("不支持的格式: %s", format)
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出测试报告",
		DefaultFilename: "test-report-" + sanitizeFilename(r.PlanName) + ext,
		Filters: []runtime.FileFilter{
			{DisplayName: filter, Pattern: "*" + ext},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	// 大文件截断保护
	if len(content) > 20<<20 {
		content = content[:20<<20]
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

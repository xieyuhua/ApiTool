// Package testing 提供接口测试用例的生成、异步生成队列、执行引擎与报告导出能力。
// 通过 Host 接口与上层 App 解耦，App 只需实现数据存储、请求发送、事件推送与对话框等宿主能力。
package testing

import (
	"apitool/internal/ai"
	"apitool/internal/doc"
	"apitool/internal/model"
	"apitool/internal/store"
	"apitool/internal/util"

	"context"
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

// Host 是 Engine 所需的宿主能力（由 main 包 *App 实现）。
type Host interface {
	// ReadData 返回当前全部应用数据。
	ReadData() model.AppData
	// SaveData 持久化全部应用数据。
	SaveData(model.AppData) error
	// SendRequest 执行 HTTP 请求，返回响应数据。
	SendRequest(model.RequestSpec) model.ResponseData
	// Emit 向前端发送运行时事件。
	Emit(event string, data ...interface{})
	// SaveFileDialog 弹出保存文件对话框，返回所选路径。
	SaveFileDialog(opts runtime.SaveDialogOptions) (string, error)
	// AppVersion 返回客户端版本号。
	AppVersion() string
}

// Engine 承载测试用例生成与执行逻辑，避免 main 包过度膨胀。
type Engine struct {
	host Host
	ctx  context.Context
}

// NewEngine 创建测试引擎实例。
func NewEngine(host Host, ctx context.Context) *Engine {
	return &Engine{host: host, ctx: ctx}
}

// ---------------- AI 生成测试用例 ----------------

// buildApiBrief 将接口信息压缩为适合喂给 AI 的精简结构（含字段示例与响应结构）
func buildApiBrief(api model.ApiInfo, common model.CommonParams, envKeys []string) string {
	brief := map[string]interface{}{
		"name":        api.Name,
		"description": api.Description,
		"method":      api.Method,
		"url":         api.URL,
		"query":       doc.EnabledKVs(api.Query),
		"headers":     doc.EnabledKVs(api.Headers),
		"reqFields":   api.ReqFields,
		"respFields":  api.RespFields,
		"body":        api.Body,
		// 公共参数：执行时自动附加到所有请求，接口同名覆盖公共
		"commonHeaders": doc.EnabledKVs(common.Headers),
		"commonQuery":   doc.EnabledKVs(common.Query),
		// 可用环境变量名（执行时按 {{变量名}} 替换，值为机密不提供）
		"availableEnvVars": envKeys,
	}
	b, _ := json.Marshal(brief)
	return string(b)
}

// dirNameOf 按目录 ID 查找目录名称（用于测试用例的目录归属展示）
func dirNameOf(dirs []model.Directory, id string) string {
	for _, d := range dirs {
		if d.ID == id {
			return d.Name
		}
	}
	return ""
}

func genCasesForApi(s model.Settings, api model.ApiInfo, common model.CommonParams, envKeys []string) ([]model.TestCase, error) {
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

	raw, err := ai.Chat(s, system, user)
	if err != nil {
		return nil, err
	}
	jsonStr := ai.ExtractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("AI 未返回有效的 JSON 结果")
	}
	var parsed struct {
		Cases []struct {
			Name        string `json:"name"`
			Category    string `json:"category"`
			Description string `json:"description"`
			Method      string `json:"method"`
			URL         string `json:"url"`
			Headers     []model.KV `json:"headers"`
			Query       []model.KV `json:"query"`
			BodyType    string `json:"bodyType"`
			Body        string `json:"body"`
			Assertions  []model.Assertion `json:"assertions"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("AI 用例结果解析失败: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	out := make([]model.TestCase, 0, len(parsed.Cases))
	for _, c := range parsed.Cases {
		cat := c.Category
		if cat == "" {
			cat = "正常流程"
		}
		// 断言默认启用
		for i := range c.Assertions {
			c.Assertions[i].Enabled = true
		}
		out = append(out, model.TestCase{
			ID:          util.GenID(),
			ApiID:       api.ID,
			ApiName:     api.Name,
			DirID:       api.DirID,
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
			Source:      "ai",
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("AI 未生成任何用例")
	}
	return out, nil
}

// GenerateTestCases 为指定接口生成测试用例（基于现有接口或已导入的 OpenAPI 接口）
func (e *Engine) GenerateTestCases(apiID string) ([]model.TestCase, error) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return nil, fmt.Errorf("没有可用的项目")
	}
	if strings.TrimSpace(data.Settings.AIKey) == "" {
		return nil, fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	var api *model.ApiInfo
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
	cases, err := genCasesForApi(data.Settings, *api, data.Projects[idx].Common, envKeys)
	if err != nil {
		return nil, err
	}
	for i := range cases {
		cases[i].DirName = dirNameOf(data.Projects[idx].Dirs, api.DirID)
	}
	return cases, nil
}

// GenerateTestCasesForApis 批量生成（可先导入 OpenAPI，再对多个接口生成）
func (e *Engine) GenerateTestCasesForApis(apiIDs []string) ([]model.TestCase, error) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
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
	var out []model.TestCase
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
		for j := range cases {
			cases[j].DirName = dirNameOf(data.Projects[idx].Dirs, api.DirID)
		}
		out = append(out, cases...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有可生成用例的接口")
	}
	return out, nil
}

// apiToTestCase 将已有接口定义直接转换为可执行测试用例（无需 AI，用于快速自动化测试 / 压测）
func apiToTestCase(api model.ApiInfo) model.TestCase {
	tc := model.TestCase{
		ID:          util.GenID(),
		ApiID:       api.ID,
		ApiName:     api.Name,
		DirID:       api.DirID,
		Category:    "正常流程",
		Name:        util.FirstNonEmpty(api.Name, api.URL),
		Description: api.Description,
		Method:      api.Method,
		URL:         api.URL,
		Headers:     api.Headers,
		Query:       api.Query,
		BodyType:    api.BodyType,
		Body:        api.Body,
		FormItems:   api.FormItems,
		ContentType: api.ContentType,
		Assertions: []model.Assertion{
			{Type: "status", Target: "", Operator: "eq", Expected: "200", Enabled: true},
		},
		Enabled:   true,
		CreatedAt: time.Now().Format(time.RFC3339),
		Source:    "api",
	}
	return tc
}

// ImportApisAsTestCases 将指定接口导入为测试用例，复用其请求定义（地址/参数/请求体）
func (e *Engine) ImportApisAsTestCases(apiIDs []string) (int, error) {
	if len(apiIDs) == 0 {
		return 0, fmt.Errorf("请至少选择一个接口")
	}
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	idSet := map[string]bool{}
	for _, id := range apiIDs {
		idSet[id] = true
	}
	count := 0
	for i := range data.Projects[idx].Apis {
		api := data.Projects[idx].Apis[i]
		if !idSet[api.ID] {
			continue
		}
		tc := apiToTestCase(api)
		tc.DirName = dirNameOf(data.Projects[idx].Dirs, api.DirID)
		data.Projects[idx].TestCases = append(data.Projects[idx].TestCases, tc)
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("未找到所选接口")
	}
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := e.host.SaveData(data); err != nil {
		return 0, err
	}
	return count, nil
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
func (e *Engine) GenerateTestCasesAsync(apiIDs []string) (string, error) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return "", fmt.Errorf("没有可用的项目")
	}
	if strings.TrimSpace(data.Settings.AIKey) == "" {
		return "", fmt.Errorf("请先在「设置」中配置 AI API Key")
	}
	if len(apiIDs) == 0 {
		return "", fmt.Errorf("请至少选择一个接口")
	}
	genWorkerOnce.Do(func() { go e.genWorker() })
	jobID := util.GenID()
	genJobCh <- genJobReq{jobID: jobID, apiIDs: apiIDs, projID: data.CurrentProjectID}
	return jobID, nil
}

func (e *Engine) genWorker() {
	for job := range genJobCh {
		e.runGenJob(job)
	}
}

func (e *Engine) runGenJob(job genJobReq) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		e.emitGenError(job.jobID, "没有可用的项目")
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
	allCases := []model.TestCase{}
	for _, id := range job.apiIDs {
		var api *model.ApiInfo
		for i := range proj.Apis {
			if proj.Apis[i].ID == id {
				api = &proj.Apis[i]
				break
			}
		}
		if api == nil {
			done++
			e.emitGenProgress(job.jobID, total, done, "", "skipped")
			continue
		}
		e.emitGenProgress(job.jobID, total, done, api.Name, "generating")
		cases, err := genCasesForApi(data.Settings, *api, common, envKeys)
		done++
		if err != nil {
			e.emitGenProgress(job.jobID, total, done, api.Name, "error:"+err.Error())
			continue
		}
		allCases = append(allCases, cases...)
		for k := range allCases {
			allCases[k].DirName = dirNameOf(proj.Dirs, allCases[k].DirID)
		}
		e.emitGenProgress(job.jobID, total, done, api.Name, "ok")
	}
	e.host.Emit("apitool:gen-done", map[string]interface{}{
		"jobId": job.jobID,
		"total": total,
		"cases": allCases,
	})
}

func (e *Engine) emitGenProgress(jobID string, total, done int, name, phase string) {
	e.host.Emit("apitool:gen-progress", map[string]interface{}{
		"jobId": jobID,
		"total": total,
		"done":  done,
		"name":  name,
		"phase": phase,
	})
}

func (e *Engine) emitGenError(jobID string, msg string) {
	e.host.Emit("apitool:gen-error", map[string]interface{}{
		"jobId": jobID,
		"error": msg,
	})
}

// ---------------- 执行引擎 ----------------

// activeEnvKeys 返回当前项目激活环境下的环境变量名（不包含值，避免泄露机密）
func activeEnvKeys(data model.AppData, idx int) []string {
	proj := data.Projects[idx]
	if proj.ActiveEnvID == "" {
		return nil
	}
	for _, en := range proj.Environments {
		if en.ID == proj.ActiveEnvID {
			keys := []string{}
			for _, v := range en.Vars {
				if v.Enabled && v.Key != "" {
					keys = append(keys, v.Key)
				}
			}
			return keys
		}
	}
	return nil
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
func compareSimple(actual string, as model.Assertion, desc string) model.AssertionResult {
	ar := model.AssertionResult{Description: desc}
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
func evaluateAssertion(resp model.ResponseData, as model.Assertion) model.AssertionResult {
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
			return model.AssertionResult{Description: desc, Passed: false, Detail: "正则编译失败：" + err.Error()}
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
		return model.AssertionResult{Description: desc, Passed: ok, Detail: detail}
	case "json":
		v, err := jsonPathValue(resp.Body, as.Target)
		if err != nil {
			return model.AssertionResult{Description: desc, Passed: false, Detail: "取值失败：" + err.Error()}
		}
		return compareSimple(fmt.Sprintf("%v", v), as, desc)
	default:
		return model.AssertionResult{Description: desc, Passed: false, Detail: "不支持的断言类型：" + as.Type}
	}
}

// runCase 执行单个用例并返回结果
func (e *Engine) runCase(c model.TestCase, env []model.KV, common model.CommonParams, timeout int) model.TestResult {
	res := model.TestResult{
		CaseID:     c.ID,
		CaseName:   c.Name,
		Category:   c.Category,
		DurationMs: 0,
	}
	spec := model.RequestSpec{
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
	util.MergeCommon(&spec, common)
	resp := e.host.SendRequest(spec)
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

// DeleteTestCases 按 ID 批量删除当前项目的测试用例
func (e *Engine) DeleteTestCases(caseIDs []string) (int, error) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return 0, fmt.Errorf("没有可用的项目")
	}
	idSet := map[string]bool{}
	for _, id := range caseIDs {
		idSet[id] = true
	}
	kept := make([]model.TestCase, 0)
	removed := 0
	for _, c := range data.Projects[idx].TestCases {
		if idSet[c.ID] {
			removed++
			continue
		}
		kept = append(kept, c)
	}
	data.Projects[idx].TestCases = kept
	data.Projects[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if err := e.host.SaveData(data); err != nil {
		return 0, err
	}
	return removed, nil
}

// RunTestCases 执行指定用例（可跨计划），支持并发执行（并发数 concurrency<=0 时退化为串行）。
// 返回测试报告（不持久化，由前端负责保存）。结果按入参用例顺序返回。
func (e *Engine) RunTestCases(caseIDs []string, envID string, concurrency int) (model.TestReport, error) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return model.TestReport{}, fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]

	var env []model.KV
	if envID != "" {
		for _, en := range proj.Environments {
			if en.ID == envID {
				env = util.EnabledEnvVars(en.Vars)
				break
			}
		}
	}

	caseMap := map[string]model.TestCase{}
	for _, c := range proj.TestCases {
		caseMap[c.ID] = c
	}

	// 按入参顺序收集待执行用例
	order := make([]model.TestCase, 0, len(caseIDs))
	for _, id := range caseIDs {
		c, ok := caseMap[id]
		if !ok || !c.Enabled {
			continue
		}
		order = append(order, c)
	}

	report := model.TestReport{
		ID:        util.GenID(),
		PlanID:    "",
		PlanName:  "手动执行",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	if len(order) == 0 {
		return report, nil
	}

	results := make([]model.TestResult, len(order))
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
		go func(idx int, tc model.TestCase) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = e.runCase(tc, env, proj.Common, data.Settings.TimeoutSec)
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
func (e *Engine) RunTestPlan(planID string) (model.TestReport, error) {
	data := e.host.ReadData()
	idx := store.ActiveProjectIndex(data)
	if idx < 0 {
		return model.TestReport{}, fmt.Errorf("没有可用的项目")
	}
	proj := data.Projects[idx]
	var plan *model.TestPlan
	for i := range proj.TestPlans {
		if proj.TestPlans[i].ID == planID {
			plan = &proj.TestPlans[i]
			break
		}
	}
	if plan == nil {
		return model.TestReport{}, fmt.Errorf("未找到指定的测试计划")
	}
	conc := plan.Concurrency
	if conc <= 0 {
		conc = 1
	}
	report, err := e.RunTestCases(plan.CaseIDs, plan.EnvID, conc)
	if err != nil {
		return report, err
	}
	report.PlanID = plan.ID
	report.PlanName = plan.Name
	return report, nil
}

// ---------------- AI 分析报告 ----------------

// GenerateReportSummary 根据测试报告（JSON）调用 AI 生成中文分析摘要
func (e *Engine) GenerateReportSummary(reportJSON string) (string, error) {
	data := e.host.ReadData()
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

	return ai.Chat(data.Settings, system, user)
}

// ---------------- 报告导出 ----------------

// BuildReportHTMLContent 将测试报告（JSON）渲染为独立 HTML 源码，供文档中心预览/导出/分享。
func (e *Engine) BuildReportHTMLContent(reportJSON string) (string, error) {
	var r model.TestReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	return doc.BuildTestReportHTML(r), nil
}

// ExportTestReport 将测试报告导出为 Markdown / HTML 文件，返回保存路径
func (e *Engine) ExportTestReport(reportJSON string, format string) (string, error) {
	var r model.TestReport
	if err := json.Unmarshal([]byte(reportJSON), &r); err != nil {
		return "", fmt.Errorf("报告解析失败: %v", err)
	}
	var content, ext, filter string
	switch format {
	case "html":
		content = doc.BuildTestReportHTML(r)
		ext, filter = ".html", "HTML (*.html)|*.html"
	case "markdown", "":
		content = doc.BuildTestReportMarkdown(r)
		ext, filter = ".md", "Markdown (*.md)|*.md"
	default:
		return "", fmt.Errorf("不支持的格式: %s", format)
	}
	path, err := e.host.SaveFileDialog(runtime.SaveDialogOptions{
		Title:           "导出测试报告",
		DefaultFilename: "test-report-" + doc.SanitizeFilename(r.PlanName) + ext,
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

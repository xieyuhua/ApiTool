package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var envVarRe = regexp.MustCompile(`{{\s*([\w.\-]+)\s*}}`)

// buildEnvMap 由环境变量列表构建映射（仅启用项）
func buildEnvMap(env []KV) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if kv.Enabled && kv.Key != "" {
			m[kv.Key] = kv.Value
		}
	}
	return m
}

// applyEnv 将字符串中的 {{var}} 替换为环境变量值
func applyEnv(s string, env map[string]string) string {
	if len(env) == 0 {
		return s
	}
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarRe.FindStringSubmatch(match)[1]
		if v, ok := env[name]; ok {
			return v
		}
		return match
	})
}

// SendRequest 执行 HTTP 请求
func (a *App) SendRequest(spec RequestSpec) ResponseData {
	start := time.Now()
	fail := func(msg string) ResponseData {
		return ResponseData{Error: msg, DurationMs: time.Since(start).Milliseconds()}
	}

	env := buildEnvMap(spec.Env)
	rawURL := applyEnv(strings.TrimSpace(spec.URL), env)
	if rawURL == "" {
		return fail("请求地址不能为空")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fail("地址解析失败: " + err.Error())
	}
	q := u.Query()
	for _, kv := range spec.Query {
		if kv.Enabled && kv.Key != "" {
			q.Add(kv.Key, applyEnv(kv.Value, env))
		}
	}
	u.RawQuery = q.Encode()

	method := strings.ToUpper(spec.Method)
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	contentType := ""
	switch spec.BodyType {
	case "json":
		if strings.TrimSpace(spec.Body) != "" {
			bodyReader = strings.NewReader(applyEnv(spec.Body, env))
			contentType = "application/json"
		}
	case "form":
		form := url.Values{}
		for _, kv := range spec.FormItems {
			if kv.Enabled && kv.Key != "" {
				form.Add(kv.Key, applyEnv(kv.Value, env))
			}
		}
		bodyReader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	case "text":
		if spec.Body != "" {
			bodyReader = strings.NewReader(applyEnv(spec.Body, env))
			contentType = "text/plain"
		}
	}

	req, err := http.NewRequest(method, u.String(), bodyReader)
	if err != nil {
		return fail("构建请求失败: " + err.Error())
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if spec.ContentType != "" {
		req.Header.Set("Content-Type", spec.ContentType)
	}
	for _, kv := range spec.Headers {
		if kv.Enabled && kv.Key != "" {
			req.Header.Set(applyEnv(kv.Key, env), applyEnv(kv.Value, env))
		}
	}

	timeout := spec.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fail("请求失败: " + err.Error())
	}
	defer resp.Body.Close()

	const maxBody = 20 << 20 // 20MB
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fail("读取响应失败: " + err.Error())
	}

	headers := map[string]string{}
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}

	body := string(bodyBytes)
	isJSON := false
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var js interface{}
		if json.Unmarshal([]byte(trimmed), &js) == nil {
			isJSON = true
			if pretty, err := json.MarshalIndent(js, "", "  "); err == nil {
				body = string(pretty)
			}
		}
	}

	return ResponseData{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    headers,
		Body:       body,
		DurationMs: time.Since(start).Milliseconds(),
		Size:       int64(len(bodyBytes)),
		IsJSON:     isJSON,
	}
}

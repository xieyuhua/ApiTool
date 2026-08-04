// Package httpx 提供 HTTP 请求执行与 {{var}} 变量替换，独立于 Wails 运行时。
package httpx

import (
	"apitool/internal/model"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var envVarRe = regexp.MustCompile(`{{\s*([\w.\-]+)\s*}}`)

// buildEnvMap 由环境变量列表构建映射（仅启用项）
func buildEnvMap(env []model.KV) map[string]string {
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
func SendRequest(spec model.RequestSpec) model.ResponseData {
	start := time.Now()
	fail := func(msg string) model.ResponseData {
		return model.ResponseData{Error: msg, DurationMs: time.Since(start).Milliseconds()}
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
		// 若存在文件类型字段，则使用 multipart/form-data；否则使用 urlencoded
		hasFile := false
		for _, kv := range spec.FormItems {
			if kv.Enabled && kv.Key != "" && kv.Type == model.FormTypeFile {
				hasFile = true
				break
			}
		}
		if !hasFile {
			form := url.Values{}
			for _, kv := range spec.FormItems {
				if kv.Enabled && kv.Key != "" {
					form.Add(kv.Key, applyEnv(kv.Value, env))
				}
			}
			bodyReader = strings.NewReader(form.Encode())
			contentType = "application/x-www-form-urlencoded"
		} else {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			for _, kv := range spec.FormItems {
				if !kv.Enabled || kv.Key == "" {
					continue
				}
				if kv.Type == model.FormTypeFile {
					path := applyEnv(kv.Value, env)
					if path == "" {
						continue
					}
					fd, ferr := os.Open(path)
					if ferr != nil {
						return fail("打开上传文件失败: " + ferr.Error())
					}
					// 以原始文件名作为 part 文件名
					_, name := filepath.Split(path)
					part, perr := w.CreateFormFile(kv.Key, name)
					if perr != nil {
						fd.Close()
						return fail("创建文件表单失败: " + perr.Error())
					}
					if _, cerr := io.Copy(part, fd); cerr != nil {
						fd.Close()
						return fail("读取上传文件失败: " + cerr.Error())
					}
					fd.Close()
				} else {
					if ferr := w.WriteField(kv.Key, applyEnv(kv.Value, env)); ferr != nil {
						return fail("写入表单字段失败: " + ferr.Error())
					}
				}
			}
			if cerr := w.Close(); cerr != nil {
				return fail("关闭表单失败: " + cerr.Error())
			}
			bodyReader = &buf
			contentType = w.FormDataContentType()
		}
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

	return model.ResponseData{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    headers,
		Body:       body,
		DurationMs: time.Since(start).Milliseconds(),
		Size:       int64(len(bodyBytes)),
		IsJSON:     isJSON,
	}
}

// Package serverclient 桌面端访问 ApiTool 服务端的 Go HTTP 客户端。
//
// 职责：登录/刷新令牌、/rpc 反射调用、凭据持久化（0600 文件）。
// DualStore（dualstore.go）用它实现「未登录→本地 SQLite；已登录→服务端」
// 的双模式存储。网页端不需要本包（它直接走浏览器 fetch）。
package serverclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Client 服务端客户端。
type Client struct {
	baseURL string
	http    *http.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	userID       string
	username     string
	// onChange 登录态变化回调（登录/登出/令牌轮换后触发，可空）。
	onChange func(userID, username string)
}

// New 创建客户端。baseURL 形如 http://127.0.0.1:8080。
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// SetOnChange 注册登录态变化回调。
func (c *Client) SetOnChange(fn func(userID, username string)) { c.onChange = fn }

// BaseURL 返回服务端地址。
func (c *Client) BaseURL() string { return c.baseURL }

// UserID 当前登录用户（未登录为空）。
func (c *Client) UserID() string { return c.userID }

// Username 当前登录用户名（未登录为空）。
func (c *Client) Username() string { return c.username }

// LoggedIn 是否已登录。
func (c *Client) LoggedIn() bool { return c.accessToken != "" }

// ---- 凭据持久化（path 为凭据文件路径，服务端地址前缀）----

// SaveCredentials 持久化凭据（0600，目录按需创建）。
func (c *Client) SaveCredentials(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if path == "" {
		return nil
	}
	b, _ := json.Marshal(map[string]string{
		"baseURL":      c.baseURL,
		"accessToken":  c.accessToken,
		"refreshToken": c.refreshToken,
		"userID":       c.userID,
		"username":     c.username,
	})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// RestoreCredentials 从文件恢复凭据（返回是否恢复出有效登录态）。
func (c *Client) RestoreCredentials(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	if m["baseURL"] != "" {
		c.baseURL = strings.TrimRight(m["baseURL"], "/")
	}
	c.accessToken = m["accessToken"]
	c.refreshToken = m["refreshToken"]
	c.userID = m["userID"]
	c.username = m["username"]
	if c.accessToken == "" || c.userID == "" {
		return false
	}
	return true
}

// ClearCredentials 清空内存凭据并删除文件。
func (c *Client) ClearCredentials(path string) {
	c.mu.Lock()
	c.accessToken = ""
	c.refreshToken = ""
	c.userID = ""
	c.username = ""
	c.mu.Unlock()
	if path != "" {
		_ = os.Remove(path)
	}
}

// ---- 鉴权 ----

type authResp struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	User         struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	Projects []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Role    string `json:"role"`
		Version int64  `json:"version"`
	} `json:"projects"`
	// 错误响应
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (r *authResp) errIf() error {
	if r.Error != "" || (!r.OK && r.AccessToken == "" && r.Error == "") {
		if r.Error != "" {
			return fmt.Errorf("%s", r.Error)
		}
	}
	return nil
}

// Login 登录（成功后触发 onChange）。
func (c *Client) Login(username, password string) error {
	var resp authResp
	if err := c.post("/api/login", map[string]string{"username": username, "password": password}, &resp, false); err != nil {
		return err
	}
	if err := resp.errIf(); err != nil {
		return err
	}
	if resp.AccessToken == "" {
		return fmt.Errorf("登录失败：服务端未返回令牌")
	}
	c.mu.Lock()
	c.accessToken = resp.AccessToken
	c.refreshToken = resp.RefreshToken
	c.userID = resp.User.ID
	c.username = resp.User.Username
	onChange := c.onChange
	c.mu.Unlock()
	if onChange != nil {
		onChange(resp.User.ID, resp.User.Username)
	}
	return nil
}

// Register 注册并登录。
func (c *Client) Register(username, password string) error {
	var resp authResp
	if err := c.post("/api/register", map[string]string{"username": username, "password": password}, &resp, false); err != nil {
		return err
	}
	if resp.AccessToken == "" {
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		return fmt.Errorf("注册失败")
	}
	c.mu.Lock()
	c.accessToken = resp.AccessToken
	c.refreshToken = resp.RefreshToken
	c.userID = resp.User.ID
	c.username = resp.User.Username
	onChange := c.onChange
	c.mu.Unlock()
	if onChange != nil {
		onChange(resp.User.ID, resp.User.Username)
	}
	return nil
}

// Logout 登出（尽力吊销 refresh）。
func (c *Client) Logout() {
	c.mu.Lock()
	rt := c.refreshToken
	c.mu.Unlock()
	if rt != "" {
		_ = c.post("/api/logout", map[string]string{"refreshToken": rt}, nil, false)
	}
}

// refresh 用 refreshToken 轮换 accessToken。
func (c *Client) refresh() error {
	c.mu.Lock()
	rt := c.refreshToken
	c.mu.Unlock()
	if rt == "" {
		return fmt.Errorf("无刷新令牌")
	}
	var resp authResp
	if err := c.post("/api/token/refresh", map[string]string{"refreshToken": rt}, &resp, false); err != nil {
		return err
	}
	if resp.AccessToken == "" {
		return fmt.Errorf("刷新失败")
	}
	c.mu.Lock()
	c.accessToken = resp.AccessToken
	c.refreshToken = resp.RefreshToken
	onChange := c.onChange
	uid, uname := c.userID, c.username
	c.mu.Unlock()
	if onChange != nil {
		onChange(uid, uname)
	}
	return nil
}

// ---- 通用请求 ----

// post 发送 JSON POST。withAuth 为 true 时带 accessToken 并在 401 时刷新重试一次。
func (c *Client) post(path string, body interface{}, out interface{}, withAuth bool) error {
	do := func() (*http.Response, error) {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if withAuth {
			c.mu.Lock()
			tok := c.accessToken
			c.mu.Unlock()
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
		}
		return c.http.Do(req)
	}

	for attempt := 0; attempt < 2; attempt++ {
		res, err := do()
		if err != nil {
			return fmt.Errorf("连接服务端失败: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
		_ = res.Body.Close()

		// 401 → 刷新后重试一次
		if res.StatusCode == http.StatusUnauthorized && withAuth && attempt == 0 {
			if rerr := c.refresh(); rerr != nil {
				return fmt.Errorf("登录已过期（%v），请重新登录", rerr)
			}
			continue
		}

		// 解析：/rpc 返回 {ok,result}；/api 直接顶层或 {ok,result}
		var envelope struct {
			OK     bool            `json:"ok"`
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(raw, &envelope); err == nil && (envelope.Result != nil || envelope.Error != "") {
			if !envelope.OK && envelope.Error != "" {
				return fmt.Errorf("%s", envelope.Error)
			}
			if out != nil && envelope.Result != nil {
				if err := json.Unmarshal(envelope.Result, out); err != nil {
					return fmt.Errorf("响应解析失败: %w", err)
				}
			}
			return nil
		}
		// 顶层结构（auth 响应）
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("响应解析失败: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("请求失败")
}

// CallRPC 调用服务端 /rpc/{Method}（与前端桥同一协议）。
func (c *Client) CallRPC(method string, args []json.RawMessage, out interface{}) error {
	return c.post("/rpc/"+method, args, out, true)
}

// Health 探测服务端。
func (c *Client) Health() (version string, err error) {
	res, err := c.http.Get(c.baseURL + "/api/health")
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	var h struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return "", err
	}
	if !h.OK {
		return "", fmt.Errorf("服务端健康检查失败")
	}
	return h.Version, nil
}

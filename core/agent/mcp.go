package agent

// 基础层：MCP 客户端（stdio / http 两种传输）。
// 负责连接、列举工具、调用工具；不参与 Agent 循环编排（那在 engine 层）。

// ============================ MCP 客户端 ============================

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"apitool/internal/ai"
	"apitool/internal/util"
)

// MCPTool MCP 工具定义。
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Server      string          `json:"server"` // 归属服务器 ID（前端用）
	ServerName  string          `json:"serverName"`
}

// jsonRPCReq / jsonRPCResp JSON-RPC 2.0 报文。
type jsonRPCReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}
type jsonRPCResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mcpUserContext 传给 MCP 的登录用户上下文（用于权限区分）。
func (m *Manager) MCPUserContext(cfg AgentConfig, users []AgentUser) map[string]interface{} {
	for _, u := range users {
		if u.ID == cfg.CurrentUserID {
			return map[string]interface{}{
				"userId":   u.ID,
				"userName": u.Name,
				"token":    u.Token,
				"roles":    u.Roles,
			}
		}
	}
	return nil
}

// callMCPStdio 通过 stdio 传输调用一次 MCP（initialize -> 指定 method）。
// 为简化实现，每次调用启动一次进程，完成后退出。
func (m *Manager) callMCPStdio(srv MCPServer, method string, params interface{}) (json.RawMessage, error) {
	if srv.Command == "" {
		return nil, fmt.Errorf("MCP[%s] 未配置命令", srv.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP 进程失败: %w", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	reader := bufio.NewReader(stdout)
	writeReq := func(req jsonRPCReq) error {
		b, _ := json.Marshal(req)
		b = append(b, '\n')
		_, err := stdin.Write(b)
		return err
	}
	readResp := func(wantID int) (json.RawMessage, error) {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var resp jsonRPCResp
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue // 忽略非 JSON-RPC 行（部分 server 会打印日志）
			}
			if resp.ID != wantID {
				continue
			}
			if resp.Error != nil {
				return nil, fmt.Errorf("MCP 错误 %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

	// initialize
	if err := writeReq(jsonRPCReq{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "apitool-agent", "version": ai.AppVersion},
	}}); err != nil {
		return nil, err
	}
	if _, err := readResp(1); err != nil {
		return nil, err
	}
	// initialized 通知
	_ = writeReq(jsonRPCReq{JSONRPC: "2.0", ID: 0, Method: "notifications/initialized"})

	// 真正的方法调用
	if err := writeReq(jsonRPCReq{JSONRPC: "2.0", ID: 2, Method: method, Params: params}); err != nil {
		return nil, err
	}
	return readResp(2)
}

// callMCPHTTP 通过 HTTP（JSON-RPC over POST）调用远程 MCP。
func (m *Manager) callMCPHTTP(srv MCPServer, method string, params interface{}, userCtx map[string]interface{}) (json.RawMessage, error) {
	if srv.URL == "" {
		return nil, fmt.Errorf("MCP[%s] 未配置 URL", srv.Name)
	}
	reqBody, _ := json.Marshal(jsonRPCReq{JSONRPC: "2.0", ID: 2, Method: method, Params: params})
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range srv.Headers {
		req.Header.Set(k, v)
	}
	// 传入登录用户标识，供服务端区分权限
	if userCtx != nil {
		if uid, ok := userCtx["userId"].(string); ok && uid != "" {
			req.Header.Set("X-User-Id", uid)
		}
		if tok, ok := userCtx["token"].(string); ok && tok != "" {
			req.Header.Set("X-User-Token", tok)
		}
		if b, err := json.Marshal(userCtx); err == nil {
			req.Header.Set("X-User-Context", string(b))
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(body))
	}
	var rpc jsonRPCResp
	if err := json.Unmarshal(body, &rpc); err != nil {
		return nil, fmt.Errorf("解析 MCP 响应失败: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("MCP 错误 %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	return rpc.Result, nil
}

// listMCPTools 列出单个服务器的工具。
func (m *Manager) ListMCPTools(srv MCPServer, userCtx map[string]interface{}) ([]MCPTool, error) {
	var raw json.RawMessage
	var err error
	if srv.Transport == "http" {
		raw, err = m.callMCPHTTP(srv, "tools/list", map[string]interface{}{}, userCtx)
	} else {
		raw, err = m.callMCPStdio(srv, "tools/list", map[string]interface{}{})
	}
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	for i := range parsed.Tools {
		parsed.Tools[i].Server = srv.ID
		parsed.Tools[i].ServerName = srv.Name
	}
	return parsed.Tools, nil
}

// ListAllMCPTools 列出所有启用服务器的工具（前端预览/调试用）。
func (m *Manager) ListAllMCPTools() ([]MCPTool, error) {
	d := m.ReadAgentData()
	userCtx := m.MCPUserContext(d.Config, d.Users)
	var all []MCPTool
	for _, srv := range d.Servers {
		if !srv.Enabled {
			continue
		}
		tools, err := m.ListMCPTools(srv, userCtx)
		if err != nil {
			m.AppendLog(AgentLog{Level: "error", Category: "mcp", Title: "列出工具失败: " + srv.Name, Detail: err.Error()})
			continue
		}
		all = append(all, tools...)
	}
	return all, nil
}

// TestMCPServer 测试单个 MCP 服务器连通性，返回工具列表。
func (m *Manager) TestMCPServer(srv MCPServer) ([]MCPTool, error) {
	d := m.ReadAgentData()
	userCtx := m.MCPUserContext(d.Config, d.Users)
	return m.ListMCPTools(srv, userCtx)
}

// callMCPTool 调用某工具，注入登录用户上下文（供权限区分）。
func (m *Manager) CallMCPTool(srv MCPServer, tool string, arguments map[string]interface{}, userCtx map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      tool,
		"arguments": arguments,
	}
	// 同时通过 _meta 注入用户上下文（MCP 规范允许携带 _meta）
	if userCtx != nil {
		params["_meta"] = map[string]interface{}{"user": userCtx}
	}
	start := time.Now()
	var raw json.RawMessage
	var err error
	if srv.Transport == "http" {
		raw, err = m.callMCPHTTP(srv, "tools/call", params, userCtx)
	} else {
		raw, err = m.callMCPStdio(srv, "tools/call", params)
	}
	dur := time.Since(start).Milliseconds()
	if err != nil {
		m.AppendLog(AgentLog{Level: "error", Category: "mcp", Title: fmt.Sprintf("调用工具失败: %s@%s", tool, srv.Name), Detail: err.Error(), DurationMs: dur})
		return "", err
	}
	// 解析 content 文本
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(raw, &result)
	var sb strings.Builder
	for _, c := range result.Content {
		if c.Text != "" {
			sb.WriteString(c.Text)
			sb.WriteString("\n")
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		out = string(raw)
	}
	m.AppendLog(AgentLog{Level: "tool", Category: "mcp", Title: fmt.Sprintf("调用工具: %s@%s", tool, srv.Name), Detail: "参数: " + toJSON(arguments) + "\n结果: " + util.Truncate(out, 4000), DurationMs: dur})
	return out, nil
}

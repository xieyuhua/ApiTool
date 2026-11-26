# AI Agent 独立服务（agentd）

把 AI Agent 能力做成一个**可独立部署的单二进制服务**：一个进程、一份数据、
一个访问令牌，自带网页界面。与 ApiTool 桌面端、主服务端**互不依赖**。

> 业务核心位于 [`core/agent`](../core/agent)，复制自 `internal/agent`
> （来源提交：`4bd9bdd`）并去掉了桌面依赖（`wailsrt` 文件对话框、
> `internal/plugins` 插件管理、`bus.Bus` 全量接口）。
> 两份代码**各自演进**：这里修复的问题不会自动回流到 `internal/agent`。

---

## 快速开始

### 1. 构建

```bash
# 仓库根目录
./build-agent.sh              # 全部 6 个平台
./build-agent.sh linux        # 指定平台（linux|darwin|windows）
```

脚本会先构建前端、复制到 `agent/webui/dist`，再交叉编译到 `dist/`。

### 2. 运行

```bash
./dist/agentd-linux-amd64 -addr :8090 -data ./agent-data -token <随机串>
```

启动后会打印带令牌的访问链接：

```
ApiTool Agent 独立服务已启动：:8090（db=sqlite）
访问地址：http://127.0.0.1:8090/?token=xxx
```

浏览器打开该链接即可使用（令牌会写入 Cookie 并从地址栏抹掉）。

> **必须设置令牌**：Agent 具备文件读写与执行命令能力。未设置 `-token` 时
> 服务只允许监听 `127.0.0.1`，绑定其它地址会直接拒绝启动。

### 3. Docker

```bash
./build-agent.sh linux
docker build -f agent/Dockerfile -t apitool-agent .
docker run -d --name agent -p 8090:8090 \
  -e AGENT_TOKEN=$(openssl rand -hex 16) \
  -v $PWD/agent-data:/data \
  apitool-agent
```

---

## 配置

命令行参数（也可用同名环境变量，见括号）：

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-addr` | `:8090` | 监听地址（`AGENT_ADDR`） |
| `-data` | `./agent-data` | 数据目录（`AGENT_DATA`） |
| `-db` | `sqlite` | `sqlite` 或 `mysql`（`AGENT_DB`） |
| `-dsn` | 空 | MySQL 连接串（`AGENT_DSN`） |
| `-token` | 空 | 访问令牌（`AGENT_TOKEN`） |
| `-token-ttl` | `720h` | 令牌 Cookie 有效期 |

---

## HTTP API

除 `/api/health` 外全部需要令牌（`X-Agent-Token` 请求头、`agent_token`
Cookie 或 `?token=` 参数三选一）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 健康检查（无需令牌） |
| GET | `/api/agent/data` | 全量数据（会话/配置/技能/MCP/日志） |
| GET | `/api/agent/bootstrap` | 最小启动数据（AI 配置 + 数据库连接） |
| PUT | `/api/agent/config` | 保存运行配置 |
| PUT | `/api/agent/skills` | 保存技能 |
| PUT | `/api/agent/mcpservers` | 保存 MCP 服务器 |
| PUT | `/api/agent/users` | 保存用户列表 |
| POST | `/api/agent/sessions` | 新建会话 |
| POST | `/api/agent/sessions/{id}/switch` | 切换会话 |
| PATCH | `/api/agent/sessions/{id}` | 重命名 |
| DELETE | `/api/agent/sessions/{id}` | 删除 |
| POST | `/api/agent/messages/clear` | 清空消息 |
| GET | `/api/agent/sessions/{id}/html` | 会话 HTML 源码 |
| GET | `/api/agent/sessions/{id}/export?format=html\|pdf` | 导出下载 |
| POST | `/api/agent/run` | 执行对话 |
| POST | `/api/agent/polish` | 文本润色 |
| GET | `/api/agent/tools` | 内置工具列表 |
| GET | `/api/agent/mcp/tools` | 全部 MCP 工具 |
| POST | `/api/agent/mcp/test` | 测试 MCP 服务器 |
| GET | `/api/agent/logs` | 查询日志 |
| GET | `/api/agent/logs/facets` | 日志筛选项 |
| GET | `/api/agent/logs/{id}` | 日志详情 |
| DELETE | `/api/agent/logs` | 清空日志 |
| GET | `/api/agent/events` | SSE 事件流 |

统一响应：`{"ok":true,"result":...}` / `{"ok":false,"error":"..."}`。

SSE 事件名：`agent:delta`、`agent:step`、`agent:thinking`、`agent:plan`、
`agent:usage`、`agent:done`、`agent:loop-start`、`agent:polish-start`、
`agent:sessions-changed`、`agent:log`。

### curl 示例

```bash
TOKEN=xxx
curl -H "X-Agent-Token: $TOKEN" http://127.0.0.1:8090/api/agent/data
curl -H "X-Agent-Token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"input":"你好","sessionId":""}' \
  http://127.0.0.1:8090/api/agent/run
```

---

## AI 接口配置

首次使用需在页面「设置」里填写 AI 接口地址、Key 与模型，或直接写进数据：

```bash
curl -H "X-Agent-Token: $TOKEN" -H 'Content-Type: application/json' \
  -X PUT http://127.0.0.1:8090/api/agent/config \
  -d '{"mode":"react","maxLoops":8}'
```

`RunAgent` 请求里未显式传 `baseUrl/apiKey/model` 时，服务端回退到已保存的
全局设置（`/api/agent/bootstrap` 返回的 `settings`）。

---

## 与桌面端/主服务端的差异

| 能力 | 桌面端 | 主服务端 | 独立 agentd |
|---|---|---|---|
| 数据存储 | 本地 SQLite | 多用户 MySQL/SQLite | 单租户 SQLite/MySQL |
| 鉴权 | 无（本机） | 账号 + JWT | 单令牌 |
| 文件对话框导出 | ✓ | ✗（下载） | ✗（下载） |
| 局域网访问开关 | ✓ | — | 不需要（本身就是网页） |
| 内置工具（文件/命令/DB） | ✓ | ✓（可关） | ✓ |

---

## 安全提醒

- 内置工具包含 `run_command`（执行本机命令）、`read_file`/`write_file`、
  `db_query`。**不要把服务直接暴露到公网**；如需公网访问，请置于反向代理后
  并保留令牌鉴权。
- 令牌一旦泄露等同于服务器命令执行权限，请用 `openssl rand -hex 16` 生成，
  并定期轮换（更换后旧链接立即失效）。

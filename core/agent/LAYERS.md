# core/agent 分层设计

独立 Agent 服务（`agent/`）的业务核心。本文说明四层职责、依赖方向与改动落点。

## 一、四层结构

```
core/agent/                基础层（包 agent）：类型 + 数据 + 配置 + 日志 + MCP
├── types.go               AgentData / AgentConfig / AgentSession / AgentMsg / AgentStep / AgentLog
│                          MCPServer / MCPTool / ToolFlags / DBSyncedTable / RunAgentArgs / RunAgentResult
├── builtin_meta.go        BuiltinToolMeta（内置工具的静态元信息）
├── host.go                Host / Emitter 接口、Manager、对外访问器
├── data.go                ReadAgentData / WriteAgentData / UpdateData（含技能迁移、孤儿快照清理）
├── config.go              配置 / 技能 / MCP 服务器 / 用户的保存
├── session.go             会话 CRUD + 变更广播
├── logs.go                日志写入 / 查询 / 筛选项 / 清空
├── mcp.go                 MCP 客户端（stdio / http，列举与调用工具）
└── update.go              子包所需的窄接口：Runtime / Context / DataReader

core/agent/tools/          工具层（包 tools）：内置工具 + 数据库查询 + 表格导出
├── toolbox.go             Toolbox 定义、Collect / Exec / SyncedDBs 入口
├── tools.go               15 个内置工具 + SQL 已同步表校验
├── db.go                  数据库只读查询（MySQL / PostgreSQL / Oracle 方言适配）
├── support.go             连接池（按连接指纹复用，90s 过期）
└── table.go               表格解析与 xlsx / csv / html / md 导出

core/agent/engine/         引擎层（包 engine）：对话编排 + LLM 调用
├── engine_types.go        Engine 定义（持有 Runtime + Toolbox）
├── engine.go              RunAgent（ReAct / Plan 循环）、PolishText
├── llm.go                 非流式 / 流式 LLM 调用、空闲超时、请求日志
├── stream.go              流式增量过滤（bodyFilter，避免半截标签外泄）
├── prompt.go              系统提示词构建
└── parse.go               工具调用解析（标准标签 / 半闭合 / 裸 JSON / DSML）

core/agent/io/             导出层（包 io）：会话导出
├── exporter.go            Exporter 定义（只依赖 DataReader）
└── export.go              会话 HTML / PDF 生成、Markdown 渲染
```

## 二、依赖方向（单向，无环）

```
                    ┌──────────────────────┐
                    │  agent（基础层）      │  ← 不 import 任何子包
                    │  类型 / 数据 / 日志   │
                    └──────────┬───────────┘
                               │ 被所有子包 import
          ┌────────────────────┼────────────────────┐
          │                    │                    │
    ┌─────▼─────┐      ┌───────▼───────┐     ┌──────▼──────┐
    │  tools    │      │    engine     │     │     io      │
    │ 工具层    │◄─────│    引擎层      │     │   导出层    │
    └───────────┘      └───────────────┘     └─────────────┘
                               │
                    ┌──────────▼───────────┐
                    │   agent/（服务层）    │  ← 组合根：装配四层 + HTTP/SSE
                    └──────────────────────┘
```

**硬约束**：`core/agent` 绝不能 import `engine` / `tools` / `io`。
一旦违反就会形成 import 环（子包必须 import 基础层拿类型），
Go 编译器会直接拒绝，但更要避免「为了图方便在这里加个 engine 调用」的念头。

## 三、每层允许 / 禁止

| 层 | 允许 | 禁止 |
|---|---|---|
| `agent` | 读写 AgentData、会话/配置/日志 CRUD、MCP 连接、对外访问器 | import 任何子包；调用 LLM；执行内置工具；渲染 HTML |
| `tools` | 执行内置工具、只读数据库查询、写导出文件 | 调 LLM、读写 AgentData（只能读）、推送事件、编排对话 |
| `engine` | 编排对话、调 LLM、解析工具调用、推送事件、读写会话数据（经 `Runtime`） | 直接持有存储实现、直接执行内置工具（必须经 `Toolbox`）、渲染导出 HTML |
| `io` | 读快照、渲染 HTML/PDF、生成 Markdown | 修改任何数据、调 LLM、执行工具 |
| `agent/`（服务） | 装配、HTTP 路由、鉴权、SSE 广播、静态资源 | 放业务逻辑（应在对应层） |

## 四、新增能力该放哪一层

| 要做的事 | 落点 |
|---|---|
| 新增一个内置工具 | `tools/tools.go` 实现 + `builtin_meta.go` 注册元信息（两者必须同步，有测试校验） |
| 新增一种导出格式（如 docx） | `io/export.go` 加分支；`agent/api_export.go` 的 `format` 透传即可 |
| 新增一个 HTTP 接口 | `agent/api_*.go`；业务逻辑下沉到对应层 |
| 新增 Agent 配置项 | `types.go` 的 `AgentConfig` + `config.go` 的校验；前端在 `AgentSettings.vue` 补表单项 |
| 新增会话字段 | `types.go` 的 `AgentSession`；`data.go` 的读路径补默认值 |
| 新增 LLM 供应商适配 | `engine/llm.go`（请求构造与流式解析） |
| 新增工具调用格式兼容 | `engine/parse.go` 加规则；务必在 `parse_test.go` 补用例 |
| 新增数据存储后端 | 实现 `storeapi.Store`（`internal/store` 已有 SQLite/MySQL/JSON 三种） |

## 五、数据库表结构同步与语义注释

这组能力横跨 `types` / `data` / `tools` 三层，单独说明。

### 数据存在哪

| 字段 | 位置 | 内容 |
|---|---|---|
| `DBSchemas` | `AgentConfig`（meta.agent 列） | 表结构快照，key = `connId\|database\|table`（全小写） |
| `DBSemantics` | `AgentConfig` | 用户手写的表/字段语义，key 含 `column` 段表示字段级 |
| `DBLastDB` | `AgentConfig` | 每连接上次选中的库，界面恢复用 |
| `ActiveDBConn` | `AgentConfig` | 当前激活连接（工具未指定 connId 时回退到它） |

### 三层语义来源

1. **数据库自带注释** — 同步时取自 `information_schema`，存在 `DBSyncedColumn.Comment`；
2. **用户维护语义** — 存在 `DBSemantics`，承载业务口径（如「状态：1 待审 2 通过」）；
3. **表级语义** — `DBSemantics` 中 key 不含 `column` 段的项，说明整张表的业务含义。

三者都会注入提示词。数据库没写注释时，第 2 层就是模型唯一的线索。

### 为什么是「离线快照」而不是实时读库

- 生产库常常不允许随时连；
- 同步是用户显式动作，快照即「用户认可的结构范围」，模型不会看到意外出现的表；
- 断网也能问答。

代价是快照会过期 —— 表结构变了要重新同步。

### 关键校验

`validateSyncedTables`（`tools/tools.go`）在 `db_query` 执行前，
用正则提取 SQL 中 `FROM`/`JOIN` 后的表名，与已同步集合比对。
不在集合内的表直接拒绝，并列出可用表 —— 模型下一轮即可自我修正。
系统视图（`information_schema` / `pg_catalog` / `pg_toast`）放行。

### 孤儿清理

删除数据库连接不会级联清除快照，因此 `pruneOrphanDBAnalysis`（`data.go`）
在每次读数据时剔除指向已删除连接的快照/语义/记忆库，并校准 `ActiveDBConn`。
不做这一步的话，模型会拿到已失效的 connId，报错信息还会指向错误的数据库类型。

## 六、与 `internal/agent` 的关系

| | `internal/agent` | `core/agent` |
|---|---|---|
| 使用方 | 桌面端、主服务端 | 独立 Agent 服务（`agent/`） |
| 依赖 | `bus.Bus`、`wailsrt`（文件对话框）、`internal/plugins` | 无桌面依赖（本地 `Emitter` + 复制的查询链路） |
| 结构 | 7 个平铺文件、单一 Manager | 4 层分包 |
| 演进 | 独立 | 独立 |

**两份代码各自演进，互不同步。** 在 `internal/agent` 修的问题不会自动出现在
`core/agent`，反之亦然。同步时需按本分层结构手工对照。

## 七、测试

| 层 | 测试 | 依赖 |
|---|---|---|
| `agent` | `manager_test.go` | 真实 `internal/store`（临时目录 SQLite） |
| `tools` | `tools_test.go` | stub `Context`，无需存储 |
| `engine` | `parse_test.go` | 纯函数，无需任何依赖 |
| `io` | 无（靠 e2e 覆盖） | — |

分层带来的直接好处：`tools` 与 `engine` 的测试不再需要真实存储，
只需注入最小 stub。

```bash
go test ./core/...
```

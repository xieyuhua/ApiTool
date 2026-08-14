# ApiTool 架构与开发指南

本文面向**开发者 / 贡献者**，说明代码组织、模块依赖、关键约定与构建方式。
面向最终用户的功能说明见 [`README.md`](./README.md)；历史重构记录见 [`REFACTOR_PLAN.md`](./REFACTOR_PLAN.md)。

---

## 一、技术栈

| 层 | 技术 |
| --- | --- |
| 桌面外壳 | [Wails v2](https://wails.io/)（Go + webview，前端以 `window.go` 注入绑定） |
| 后端 | 纯 Go（模块名 `apitool`，Go 1.26） |
| 前端 | Vue 3 + Vite（`frontend/`） |
| 本地存储 | SQLite（modernc.org/sqlite，纯 Go 无 CGO）/ 可选 MySQL / 旧版 JSON 回退 |
| 抓包 | go-mitmproxy（HTTP/HTTPS/WS/SSE/gRPC/GraphQL 解密与解码） |
| 云服务器 | 标准 `net/http`，可独立部署（`./server/cmd`） |

依赖均锁定在 `go.mod`，构建默认使用 vendor（`-mod=vendor`）。

---

## 二、分层与目录

```
apitool/
├── main.go          # 进程入口：解析子命令、初始化 App、启动 Wails/独立服务
├── app.go           # App：Wails 绑定聚合根，嵌入各业务模块，暴露给前端的方法
├── tray.go          # 系统托盘菜单（剪贴板历史入口等）
├── tools.go         # 子命令注册（build/server/cli 等）
├── wails.json       # Wails 构建配置（前端产物、绑定生成）
├── internal/        # 业务逻辑（与 UI 框架解耦，可独立测试/复用）
├── server/          # 可独立部署的云同步 / 分享服务器（./server/cmd）
└── frontend/        # Vue 前端（wailsjs/ 为自动生成的绑定桩）
```

### `internal/` 子包职责

| 包 | 职责 | 关键依赖 |
| --- | --- | --- |
| `model` | 全局数据模型（`AppData`、项目/接口/环境/用例/计划/报告/设置/插件/剪贴板） | — |
| `store` | **存储门面**：SQLite/MySQL/JSON 三后端切换、旧 JSON 自动导入、读写加锁 | `model`, `store/db` |
| `store/db` | DB 抽象、`schema` DDL、SQLite 与 MySQL 实现、往返测试 | — |
| `bus` | 事件总线，把后端事件转发给前端（解耦后端与 UI 刷新） | `model` |
| `agent` | AI Agent 编排：多会话、MCP（stdio/http）、内置工具、Token 统计 | `ai`, `bus`, `store`, `model` |
| `ai` | AI 底层调用（`Chat`/`ChatRaw`/字段描述生成），以 `Host` 接口解耦宿主 | `model` |
| `testing` | 测试引擎：用例生成、执行、报告导出 | `model`, `httpx`, `store` |
| `tool` | 通用工具（Hash/HMAC/对称加解密），由 App 嵌入提升给 Wails 前端 | `crypto` |
| `doc` | 接口文档生成（md/html/word/openapi）与导入（OpenAPI/Swagger/Postman） | `model`, `jsonutil` |
| `share` | 本地分享服务（独立 HTML / 在线链接，localhost 托管） | `model`, `store`, `bus` |
| `sync` | 桌面端内置同步服务（账号、项目、分享文档） | `model`, `store` |
| `capture` | 浏览器扩展回传的流量捕获（服务端兜底过滤、去重） | `model`, `store`, `bus` |
| `plugins` | 插件数据库连接（MySQL/Redis/PG 等） | `model` |
| `httpx` | HTTP 客户端封装（发送、代理、超时、拦截） | — |
| `stress` | 压力测试 | `httpx`, `model` |
| `crypto` | 加解密工具 | — |
| `sniff` | 网络抓包（MITM）：代理、协议识别、gRPC/protobuf 解码、系统代理与 CA | `model`, `bus` |
| `platform` | 平台相关能力（剪贴板、PNG 读取、文件对话框等，含 Windows CGo） | — |
| `jsonutil` | JSON 解析 / 字段树构建 | — |
| `util` | 通用工具（`GenID`、`Token`、字符串、环境变量等） | — |

> 依赖方向原则：上层包（agent/doc/share/...）依赖 `model` 与 `store`，但 `model`/`store`/`util` 不反向依赖业务包，避免循环依赖。`bus` 仅做事件转发，不含业务逻辑。

---

## 三、运行时形态

程序有两种运行形态，由 `main.go` 的子命令决定：

1. **桌面应用（默认）**：`wails build` / `wails dev` 启动，`App` 作为 Wails 绑定根，
   前端通过 `window.go.<pkg>.<Method>` 调用 Go 方法，后端经 `bus` 向前端推送事件。
2. **云同步 / 分享服务器**：`go build -o apitool-server ./server/cmd` 后独立运行，
   不依赖前端，对外暴露 `/api/*` 与 `/s/*` 路由（见 README「部署云分享服务器」）。

---

## 四、关键设计约定

### 4.1 存储与并发
- `store.Store` 用 `sync.RWMutex` 保护后端：`GetData()` 走读锁（多协程并发读），
  `SaveData()`/`initBackend` 走写锁。**读多写少**，RWMutex 避免读路径串行。
- 后端 `db.DB.Read/Write` 内部维护原生 map（`AppData` 含切片/映射），**任何并发读写都必须加锁**，
  否则触发 `concurrent map read and map write` panic。
- 后端不可用时（文件损坏/连接失败）自动回退 JSON 文件（`data.json`），保证不崩溃。

### 4.2 包级单例状态（capture / share）
- `internal/capture` 与 `internal/share` 以**包级单例 + `sync.Mutex`** 形式运行（同进程仅一个服务实例）。
- 所有状态字段（`captureSrv`/`captureMu`/`capturedList`/`capturedIdx`/`captureToken`、
  `shareMu`/`shareSrv`/`shareItems`/...）的读写**必须持有对应锁**，禁止锁外直接访问。
- 调用点集中在 `app.go`，签名稳定，前端绑定不受内部组织变化影响。

### 4.3 ID / Token 生成
- 统一收口到 `util`：`util.GenID()`（UUID v4）用于业务 ID，`util.Token()`（32 字符 hex，128bit 熵）
  用于对外服务鉴权（如 capture 的访问 token）。
- 禁止在业务包内重复实现 `crypto/rand` + `hex` 逻辑；Agent 会话 ID 用 `util.GenID()` 前缀化。

### 4.4 事件总线
- 后端重要变更（抓包新增、分享状态、同步结果等）经 `bus` 推送，前端订阅刷新，**不**在业务包内直接操作前端状态。

### 4.5 配置来源
- Agent 配置（`internal/agent/agent.json`）由 `agent.Manager` 管理；`configs.yaml` 可覆盖模型/Provider 等默认值，
  读取时合并，避免「静态表 vs 动态文件」双重真相。

---

## 五、构建与开发

### 环境
- Go 1.26+（与 `go.mod` 一致；旧文档写的 1.21 已过时）
- Node.js 18+，Wails v2 CLI：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### 常用命令
```bash
# 桌面应用
wails build            # 产物 build/bin/apitool(.exe)
wails dev              # 热更新 + Vite，可访问 http://localhost:34115 调试前端

# 云服务器
go build -o build/bin/apitool-server ./server/cmd
./apitool-server -addr :8080 -data apitool-server-data

# 校验
go build -mod=vendor ./...
go vet  ./...
```

### 绑定生成
前端 `frontend/wailsjs/` 由 Wails 从 `app.go` 中 `App` 的公开方法自动生成；
修改 `App` 暴露的方法后须重新 `wails build`/`wails dev` 以刷新桩代码。

---

## 六、贡献约定

- **新增业务包**：放 `internal/<name>/`，保持单向依赖（不反向依赖 `app`、不依赖其他业务包的内部状态）。
- **存储访问**：统一经 `store.Store`，不要直接操作 `db` 后端；并发读用 `GetData()`（已加读锁）。
- **对外 ID/Token**：一律用 `util.GenID()` / `util.Token()`，勿重复造轮子。
- **包级状态**：若引入新的全局可变状态，必须用 `sync.Mutex`/`RWMutex` 保护，并在 `var` 块注释加锁契约。
- **事件通知前端**：经 `bus`，不要在前端组件里轮询后端。
- **破坏性改动**：涉及 `App` 公开方法签名的，需同步确认前端绑定与 README。

---

## 七、已知技术债（记录备查）

`go vet ./...` 存在 5 处历史警告，修改变更涉及 CGo / Windows API，风险较高，暂未处理：

| 位置 | 类型 | 说明 |
| --- | --- | --- |
| `internal/platform/clipboard.go:189,227,384` | `unsafe.Pointer` 误用 | Windows 剪贴板 CGo 代码 |
| `internal/platform/hotkey.go:74` | `unsafe.Pointer` 误用 | Windows 低级键盘钩子 |
| `internal/plugins/plugins.go:1034` | 非常量格式串 | `PrintfLine` 调用 |

> 上述均为既有问题，与业务逻辑无直接关联；如需修复须单独评估 CGo 兼容性。

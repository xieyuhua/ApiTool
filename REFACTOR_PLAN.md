# Go 代码目录整理方案（internal 分包重构）

> **状态：已完成（2026-08-01）。** 本文件为方案记录，最终落地架构见末尾「实际落地」。
>
> 目标：把原平铺在根的 `package main` 业务文件，按职责拆分为 Go 规范的 `internal/*` 子包，
> 消除"改一处忘一处"的散落问题。所有模块原本都通过 `App` 直接访问存储与运行时，
> 重构后通过 `Host` 接口 + 嵌入（`*agent.Manager` / `*testing.Engine`）解耦。

## 一、核心约束（务必先读）

- 所有文件目前都是 `package main`，互相引用大量未导出符号。
- `App` 结构体定义在 `app.go`，其字段 `ctx / dataFile / syncDir / captureToken / windowVisible /
  clipWinVisible / quitting / mu / agentMu` 被 10+ 个文件直接裸访问。
- `readData()` / `SaveData()` 是 `App` 的方法，被 7+ 个文件调用：`testing.go`(9处)、`capture.go`(2)、
  `share.go`、`stresstest.go`、`import.go`、`ai.go`、`clipboard_windows.go`(2)、`app.go` 内部。
- `runtime.EventsEmit(a.ctx, ...)` 等系统 API 调用点分布在：`tray.go`、`testing.go`、`stresstest.go`、
  `plugins.go`、`clipboard_windows.go`、`capture.go`、`app.go`、`agent_run.go`、`agent.go`。

## 二、目标目录结构

```
apitool/
├── main.go                  # 入口：仅装配 App 与 Wails，不再含业务逻辑
├── app.go                   # App 生命周期 + 持有 store/bus 实例 + Wails 绑定转发
├── tray.go                  # 系统托盘（App 方法，依赖 runtime，留 main 包）
├── stresstest.go            # 压测（依赖 store + bus，可迁入 internal/stress）
├── internal/
│   ├── model/               # ★ 已可无损迁入：models.go（纯导出类型，零依赖）
│   ├── crypto/              # ★ 已完成：tools.go 的加解密（纯标准库）
│   ├── jsonutil/            # jsonparse.go 的顺序 JSON 解码（被 capture/import/export 用）
│   ├── store/               # 数据读写层（解耦 dataFile/mu，核心基础设施 ①）
│   ├── bus/                 # 事件/系统 API 接口（解耦 a.ctx，核心基础设施 ②）
│   ├── agent/               # agent.go + agent_run.go + builtin_tools.go + ai.go + testing.go
│   ├── plugins/             # plugins.go + dbclient.go + sftpclient.go
│   ├── httpx/               # httpclient.go（请求执行 + 变量替换）
│   ├── doc/                 # export.go + import.go（文档导入导出，依赖 jsonutil/model）
│   ├── capture/             # capture.go + jsonparse.go（请求捕获服务）
│   ├── share/               # share.go（文档分享 HTTP 服务）
│   ├── sync/                # syncserver.go（内置同步服务，已用 server 包）
│   └── platform/            # clipboard_windows.go + hotkey_windows.go（Windows 专用）
├── server/                  # 已独立包，不动
├── frontend/ vendor/ build/ # 不动
```

> 说明：`model` 与 `crypto` 已完成。其余模块因依赖 `App` 的内部状态，必须在
> `store` + `bus` 两个基础设施就绪后才能迁入。

## 三、两个核心基础设施（第一阶段，必须先做）

### 1. `internal/store` —— 解耦数据读写
把以下从 `app.go` 抽出为独立包（不再依赖 `App`）：
- `Store` 结构体，持有 `dataFile string` + `mu sync.Mutex`。
- `NewStore(dataFile string) *Store`
- `Read() model.AppData`（原 `readData`，含 `defaultData`/`migrateLegacy`/`activeProjectIndex` 一并迁入）
- `Write(data model.AppData) error`（原 `SaveData`）
- `DataFilePath() string`
- `Dir()` 方法返回 `filepath.Dir(dataFile)`，供图片路径拼接（替代各处的 `filepath.Dir(a.dataFile)`）。

`App` 改为持有 `store *store.Store`，`a.readData()` 转发为 `a.store.Read()`，`a.SaveData(d)` 转发为
`a.store.Write(d)`。这样 7+ 个调用方**签名不变**，无需逐个改。

### 2. `internal/bus` —— 解耦运行时事件/系统 API
定义接口（避免子包直接 `import wails runtime` 并持有 `ctx`）：
```go
package bus
type Bus interface {
    Emit(event string, data ...interface{})
    SaveFileDialog(opts runtime.SaveDialogOptions) (string, error)
    OpenDirectoryDialog(opts runtime.OpenDialogOptions) (string, error)
    ClipboardGetText() (string, error)
    ClipboardSetText(text string) error
    BrowserOpenURL(url string)
    WindowShow() / WindowHide() / WindowSetAlwaysOnTop(b bool)
}
```
`App` 实现 `bus.Bus`（内部用 `a.ctx` 调 `runtime`）。各业务子包的函数签名改为接收 `bus.Bus`
参数（如 `agent.Run(cfg, b bus.Bus, s *store.Store)`），不再依赖 `App`。

> 改造量：① 为 `App` 加 `bus.Bus` 实现（约 10 个方法）；② 把 9 处 `runtime.xxx(a.ctx,...)`
> 改为 `b.xxx(...)`；③ `App` 的各 `func (a *App) Xxx()` 方法体转发到子包函数并传入 `a`(实现 Bus) 与 `a.store`。

## 四、分阶段实施步骤（每步 `go build -mod=vendor ./...` 验证）

| 阶段 | 动作 | 风险 |
|---|---|---|
| 0（已完成） | `models.go` → `internal/model`；`tools.go` → `internal/crypto` | 零 |
| 1 | 建 `internal/store`，抽出存储层；`App` 持有 `store.Store` 并转发 | 中（改 app.go + 验证全部 readData/SaveData 调用方仍编译） |
| 2 | 建 `internal/bus`，定义接口；`App` 实现并替换 9 处 `runtime.xxx(a.ctx)` | 中 |
| 3 | `jsonparse.go` → `internal/jsonutil` | 低 |
| 4 | `capture.go`+`jsonparse` → `internal/capture`（用 store+bus） | 高（文件大、依赖多） |
| 5 | `agent.go`+`agent_run.go`+`builtin_tools.go`+`ai.go`+`testing.go` → `internal/agent` | 高 |
| 6 | `plugins.go`+`dbclient.go`+`sftpclient.go` → `internal/plugins` | 高 |
| 7 | `httpclient.go` → `internal/httpx`；`export.go`+`import.go` → `internal/doc` | 中 |
| 8 | `share.go` → `internal/share`；`syncserver.go` → `internal/sync` | 中 |
| 9 | `clipboard_windows.go`+`hotkey_windows.go` → `internal/platform` | 中 |
| 10 | `stresstest.go` → `internal/stress`；`main.go` 精简为纯装配 | 中 |

## 五、关键注意

- **`App` 保留在 `package main`**，作为 Wails 绑定入口（Wails `Bind` 必须绑定 `*App`）。
  各业务方法 `func (a *App) Xxx()` 变为"薄转发层"，调用 `internal/*` 包的函数并注入 `a`(Bus)+`a.store`。
  前端 JS 调用的函数名/签名**完全不变**，无需改前端。
- **`internal/model` 会被几乎所有子包 import**，是公共叶子包，无循环依赖。
- `vendor/` 模式：移动文件后务必用 `go build -mod=vendor ./...` 验证，必要时应重跑 `go mod vendor`。
- `main.go` 的 `//go:embed all:frontend/dist` 路径相对 `main.go` 自身，移动 `main.go` 才需调整，本方案不移动它。
- 既有 vet 警告（`builtin_tools.go:382`、`plugins.go:1025`）非本次引入，不处理。

## 六、收益

- 新增/修改工具、Agent、插件逻辑时，定位到对应 `internal/` 子包即可，单一维护点。
- 业务模块可独立单测（不依赖 Wails 运行时）。
- 符合 Go 官方项目布局规范。

## 七、实际落地（与方案差异说明）

方案原拟采用「薄转发层」（App 方法体转发到子包函数并注入 a+store）。实际为降低改动面、
保持前端 JS 绑定签名不变，采用了**嵌入子包结构体**的方式：

- `App` 嵌入 `*agent.Manager` 与 `*testing.Engine`，自动提升其导出方法供 Wails 绑定。
- `agent.Manager` 通过 `Host` 接口（`Store`/`ReadData`/`SaveData`/`SendRequest`/`AppVersion`）
  与 `bus.Bus` 解耦；`testing.Engine` 通过 `Host` 接口
  （`ReadData`/`SaveData`/`SendRequest`/`Emit`/`SaveFileDialog`/`AppVersion`）解耦。
- `App` 已实现上述全部 Host 方法，无需额外薄转发。

### 模块落地对照

| 原根文件 | 目标包 | 状态 |
|---|---|---|
| `models.go` | `internal/model` | ✅ |
| `tools.go`(加解密) | `internal/crypto` | ✅（根 `tools.go` 仅留 `Tool*` 绑定转发） |
| `jsonparse.go` | `internal/jsonutil` | ✅ |
| 存储层 | `internal/store` | ✅ |
| 事件/系统 API | `internal/bus` | ✅ |
| `capture.go` | `internal/capture` | ✅ |
| `agent.go`+`agent_run.go`+`builtin_tools.go` | `internal/agent` | ✅ |
| `ai.go`(共用 LLM) | `internal/ai` | ✅ |
| `testing.go` | `internal/testing` | ✅ |
| `plugins.go`+`dbclient.go`+`sftpclient.go` | `internal/plugins` | ✅ |
| `httpclient.go` | `internal/httpx` | ✅ |
| `export.go`+`import.go` | `internal/doc` | ✅ |
| `share.go` | `internal/share` | ✅ |
| `syncserver.go` | `internal/sync` | ✅ |
| `clipboard_windows.go`+`hotkey_windows.go` | `internal/platform` | ✅ |
| `stresstest.go` | `internal/stress` | ✅ |

### 根目录保留（main 包，绑定入口层）

- `main.go`：Wails 装配入口。
- `app.go`：App 生命周期、store/bus 持有、Host 接口实现、嵌入子包。
- `tray.go`：系统托盘（依赖 runtime，留 main）。
- `ai.go`：`GenerateDescriptions`（App 方法，调用 OpenAI）。
- `tools.go`：`Tool*` 加解密绑定转发（委托 `internal/crypto`）。

> 通过 `go build -mod=vendor ./...` 验证编译通过，前端 wailsjs 绑定签名保持不变。

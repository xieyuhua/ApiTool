# ApiTool 2.0 服务端部署指南

ApiTool 2.0 由「服务端 + 三端客户端」组成：

- **服务端**：单二进制，内嵌网页端，所有业务数据存于此（SQLite / MySQL）
- **桌面端**：Wails 应用，可在「设置 → 服务端同步」登录服务端（本地留镜像，离线可用）
- **网页端**：浏览器访问服务端即可使用完整功能
- **App 端**：手机浏览器「添加到主屏幕」安装 PWA

---

## 1. 构建

```bash
# ① 构建前端（服务端与桌面端共用）
cd frontend && npm install && npm run build && cd ..

# ② 服务端（单二进制，内嵌网页端资源）
go build -tags production -trimpath -ldflags="-w -s" -o apitool-server.exe .

# ③ 桌面端（可选）
go build -tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o apitool.exe .

# ④ 更新可执行文件后需重新构建前端并重新 go build，前端资源才会更新（go:embed）
```

> 也可以用 `bash build-v3.sh`（桌面端）/ `Taskfile.yml`。

---

## 2. 启动服务端

### 2.1 个人/小团队（SQLite，零依赖）

```bash
./apitool-server.exe -addr :8080 -data ./data
```

数据文件为 `./data/server.db`（SQLite），JWT 密钥首次启动自动生成到 `./data/jwt_secret.txt`。

### 2.2 团队（MySQL）

```bash
./apitool-server.exe -addr :8080 -data ./data \
  -db mysql \
  -dsn "user:pass@tcp(127.0.0.1:3306)/apitool?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"
```

- 数据库需提前创建：`CREATE DATABASE apitool CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;`
- 建表由服务端自动完成（MySQL 与 SQLite 共用同一套 schema，主键列自动用 VARCHAR(64)）

### 2.3 全部参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-addr` | `:8080` | 监听地址 |
| `-data` | `apitool-server-data` | 数据目录（SQLite 文件/导出文件） |
| `-db` | `sqlite` | `sqlite` 或 `mysql` |
| `-dsn` | 空 | 数据库连接串 |
| `-jwt-secret` | 自动生成 | JWT 密钥（30 天有效期内可配置固定值） |
| `-allow-register` | `true` | **公网部署建议设为 false**，账号由管理员创建 |
| `-enable-plugins` | `false` | 是否允许网页端调用插件（DB/SSH/SFTP，见§5 风险） |
| `-enable-agent` | `true` | 是否允许网页端运行 AI Agent |
| `-session-ttl` | `30m` | 用户会话空闲淘汰时间 |
| `-rate-limit` | `20` | 每 IP 每秒请求数（0=关闭） |
| `-rate-burst` | `40` | 每 IP 突发上限 |
| `-login-max-fails` | `5` | 连续登录失败上限（0=不锁定） |
| `-login-lock-for` | `10m` | 登录失败锁定时长 |
| `-max-body-mb` | `32` | 请求体上限（MB） |
| `-agent-daily-quota` | `200` | 每用户每日 Agent 调用配额（0=不限） |

---

## 3. 反向代理与 HTTPS

生产环境建议放在 Nginx/Caddy 之后并启用 TLS。

**关键点**：`/events` 是 SSE 长连接，必须关闭代理缓冲且放宽超时。

```nginx
server {
    listen 443 ssl http2;
    server_name apitool.example.com;
    ssl_certificate     /etc/ssl/apitool.crt;
    ssl_certificate_key /etc/ssl/apitool.key;

    client_max_body_size 40m;          # 与 -max-body-mb 匹配

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # SSE（实时事件流）：关闭缓冲、长超时
    location /events {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

Caddy 版本：

```
apitool.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1     # SSE 必需
    }
}
```

> 服务端通过 `X-Forwarded-For` / `X-Real-IP` 识别客户端 IP 做限流，代理必须透传这两个头。

---

## 4. 数据与备份

| 内容 | 位置 |
|---|---|
| 业务数据（项目/接口/文档/测试/设置） | `-data` 目录下的 `server.db` 或 MySQL |
| 用户导出的文件 | `-data/exports/` |
| JWT 密钥 | `-data/jwt_secret.txt`（**务必备份，丢失会导致所有用户需重新登录**） |

- SQLite 备份：停止服务后复制 `server.db`，或用 `sqlite3 server.db ".backup backup.db"`。
- MySQL 备份：`mysqldump`。
- 升级程序前建议先备份。

---

## 5. 安全注意事项

1. **注册开关**：公网部署设 `-allow-register=false`，通过管理员在库中创建账号（或用内网先注册再开放）。
2. **插件 SSRF 风险**：`-enable-plugins=true` 后，网页/App 端用户可让服务端去连接**任意地址**的数据库/SSH/SFTP。仅在可信内网使用；公网务必保持 `false`。
3. **Agent 成本**：AI 调用使用用户自带的 API Key，成本归属清晰；仍建议用 `-agent-daily-quota` 限制滥用。
4. **限流**：默认每 IP 20 rps、登录失败 5 次锁 10 分钟；高并发场景可调。
5. **密码**：scrypt 加盐哈希；刷新令牌只存 SHA-256 哈希，泄漏数据库无法还原。
6. **权限**：项目级角色（admin/editor/viewer）；viewer 的写请求服务端一律拒绝（403）。
7. **JWT 密钥**：可用 `-jwt-secret` 显式指定（多实例部署时必须一致）。

---

## 6. 从 1.x 迁移

- 1.x 桌面端数据在本地 SQLite。桌面端登录服务端后，首端保存会把当前数据推到服务端。
- 旧 `server/server.go` 的本地分享服务保留，不受影响。
- 旧的 `users.json` / `projects/<user>/*.json` 可通过 `server/store` 的迁移逻辑一次性导入（如需请提 issue）。

---

## 7. 健康检查

```bash
curl http://127.0.0.1:8080/api/health
# {"ok":true,"sessions":2,"subscribers":1,"version":"2.0.0"}
```

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 开发命令

### EdgeOne Makers 集成开发
```bash
# 本地开发（前端 + Cloud Functions 联调，推荐）
edgeone makers dev
```

### 后端 Go 开发（本地测试）
```bash

# 运行单个测试（如 handler 测试）
go test ./internal/handler -run TestXXX

# 构建二进制（Cloud Functions 模式）
go build -o cloudreve-eo ./cloud-functions/api.go
```

### 测试运行
- 前端：无内置测试脚本，可手动运行 `npm run build` 检查类型。
- 后端：`go test ./internal/...` 覆盖所有 _test.go 文件。

## 项目架构概述

### 整体结构
此项目是 **Cloudreve-EO** 的简化云盘平台，面向 EdgeOne Makers 部署。采用**前后端一体**模式：前端 React SPA + Go Gin 后端作为 EdgeOne Cloud Functions 运行，无需独立后端服务。

- **前端**：React 19 + TypeScript + Vite + Ant Design + React Router。
  - 静态资源在 `src/` 中。
  - 页面在 `src/pages/`（UserGroups.tsx、Files.tsx 等）。
  - API 调用在 `src/api/`（axios 调用后端 `/api` 路径）。
  - 样式在 `src/`。

- **后端**：Go + Gin + GORM。
  - 入口在 `cloud-functions/api.go`（Gin 路由，基础路径 `/api`）。
  - 业务逻辑分层：
    - **handler/**：处理 HTTP 请求（如 `group.go`、`file.go`、`auth.go`）。
    - **model/**：GORM 数据模型（如 `user.go`、`file.go`、`user_group.go`）。
    - **service/**：业务服务（如 `auth_service.go`、`file_service.go`）。
    - **storage/**：对象存储适配（S3、EdgeOne Blob 等）。
    - **persist/**：数据库持久化（SQLite / Postgres，支持 EdgeOne Blob、S3、GitHub 等后端）。
    - **middleware/**：中间件（如 auth、accesslog）。
    - **internal/** 其他：snowflake ID、日志等。

- **存储**：S3 兼容对象存储（AWS SDK）、EdgeOne Blob（@edgeone/pages-blob）、GitHub 文件后端。
- **数据库**：默认 SQLite（`cloudreve.db`），支持 Postgres；使用 `DB_PERSIST` 选项持久化。
- **部署**：EdgeOne Makers Cloud Functions（`cloud-functions/` 目录产物），前端 Vite 构建到 `dist/`。

### 核心流程
1. 前端 React 页面调用 `/api/xxx`（需 Bearer Token JWT）。
2. Gin 路由在 `cloud-functions/api.go` 分发到 `internal/handler/`。
3. 存储使用预签名 URL 直达对象存储（不经后端中转）。
4. 认证：JWT + 注册/登录。

## 日志规范（强制）

所有后端日志**必须**统一使用 `internal/logx` 包输出 EdgeOne 单行 JSON 格式，禁止任何其他输出方式。

- **原因**：EdgeOne Makers 云函数平台按行采集 stdout/stderr，日志夹在平台 START/END RequestId 之间，控制台按关键字检索。非 JSON 的彩色/纯文本日志无法被有效检索。
- **格式**：`slog.NewJSONHandler` 单行 JSON 输出到 stdout，例如：
  ```
  {"time":"2026-08-04T10:00:00Z","level":"INFO","module":"persist","msg":"已从 edgeone-blob 恢复数据库","bytes":1048576}
  ```
- **级别**：由环境变量 `LOG_LEVEL` 控制，默认 info（debug / info / warn / error）。

### 使用方式

```go
import "github.com/cloudreve-eo/cloudreve-eo/internal/logx"

logx.Info(logx.ModuleStorage, "已加载存储策略", "count", n)      // INFO
logx.Warn(logx.ModuleDB, "慢 SQL", "elapsed_ms", ms)             // WARN
logx.Error(logx.ModuleApp, "启动失败", logx.Err(err))            // ERROR，error 必须用 logx.Err 包装
logx.With(logx.ModuleDB).Debug("SQL", "sql", sql)                // 带属性的 logger
```

### 规则

1. **禁止**直接使用 `fmt.Println` / `fmt.Printf` / `log.*` / `slog.*` / `panic` 输出日志。
2. 每条日志必须带模块名（`logx.ModuleXXX` 常量），新增模块时在 `logx.go` 中登记常量。
3. error 必须通过 `logx.Err(err)` 作为属性传入，不要拼进 msg 字符串。
4. HTTP 请求访问日志统一由 `middleware.AccessLog()` 输出（5xx→ERROR，4xx→WARN，其余 INFO，跳过 404）。
5. panic 恢复使用 `middleware.Recovery()`（JSON 记录堆栈），不要用 `gin.Recovery()`。
6. GORM 日志已通过 `model.gormLogger` 接入 logx（慢 SQL/错误），不要在别处重复打印。
7. gin 必须保持 `gin.ReleaseMode`（禁止 debug 模式路由表输出）。
8. `cloud-functions/api.go` 入口必须在最前面调用 `logx.Setup(os.Getenv("LOG_LEVEL"))`，保证 config 错误也能落盘。

### 关键文件
- `cloud-functions/api.go`：Gin 启动，注册路由。
- `internal/handler/group.go` 等：具体业务 handler。
- `internal/model/`：ORM 模型。
- `src/App.tsx`、`src/pages/*.tsx`：前端路由和组件。
- `vite.config.ts`、`tsconfig.*.json`：前端构建配置。
- `package.json`、`go.mod`：依赖管理。

此架构支持快速本地开发（`edgeone makers dev`）和 EdgeOne 部署。未来扩展应保持分层，避免直接跨层调用。

## Git 操作规则
- 未经用户明确允许（例如用户直接说 "push to github" 或 "推送到 GitHub"），**不得**执行任何 Git 推送、push、git push、git commit --amend 或类似操作。
- 任何涉及 Git 的操作必须先经用户确认或明确授权。

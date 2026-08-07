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
# 运行所有测试
go test ./...

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

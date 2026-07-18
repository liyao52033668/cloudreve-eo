# Cloudreve-EO

参考 [Cloudreve](https://github.com/cloudreve/Cloudreve) 的简化版云盘，面向 [EdgeOne Makers](https://edgeone.ai/) 部署。

- **前端**：React + Vite SPA（静态资源）
- **后端**：Go + Gin，作为 EdgeOne Cloud Functions 运行
- **文件传输**：预签名 URL 直连对象存储，不经后端中转

本项目**不是**独立的 Go 服务 + 独立前端开发模型；本地联调与线上部署都走 **EdgeOne Makers CLI**。

## 功能

- 用户注册 / 登录（JWT）
- 文件上传、下载、删除
- 文件夹创建、重命名、移动
- 文件列表与目录导航
- 文件分享（短链 + 可选提取码 + 过期时间）
- 存储策略：S3 兼容存储（MinIO / COS / R2 等；当前默认且可用）
- 用户配额与已用空间展示

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM、JWT（EdgeOne Cloud Functions） |
| 前端 | React 19、TypeScript、Vite、Ant Design、Axios |
| 数据库 | SQLite（默认）/ PostgreSQL |
| 存储 | S3 兼容对象存储（当前实现）；EdgeOne Blob 见下文说明 |
| 运行时 / 部署 | EdgeOne Makers（`edgeone makers`） |

## 环境要求

- **Node.js** 18+
- **npm**
- **Go** 1.22+（Cloud Functions 本地编译 / 运行需要）
- **EdgeOne CLI**（`edgeone`）
- 可选：MinIO 或其他 S3 兼容存储（本地对象存储）
- 可选：PostgreSQL（生产数据库）

## 快速开始

### 1. 安装依赖与 CLI

```bash
# 项目依赖
npm install

# 全局安装 EdgeOne CLI（也可使用 npx edgeone）
npm install -g edgeone

# 确认安装
edgeone -v
```

### 2. 登录 EdgeOne

```bash
# 中国站
edgeone login --site china

# 或国际站
edgeone login --site global

# 查看登录状态
edgeone whoami
```

### 3. 关联远程项目并同步环境变量

```bash
edgeone makers link
edgeone makers env pull
```

`env pull` 会把远程环境变量拉到本地 `.env`（仓库已忽略该文件）。

### 4. 配置应用环境变量

仅基础设施可走环境变量；**JWT 主密钥**与 **S3 存储策略**一律在前端管理，不从环境变量引导。

```bash
# 可选（均有默认值）
edgeone makers env set DB_DRIVER sqlite
edgeone makers env set DB_DSN cloudreve.db

# 查看 / 同步到本地
edgeone makers env ls
edgeone makers env pull
```

也可直接编辑本地 `.env` 做开发调试；线上生效仍建议用 `edgeone makers env set`。

**首次使用（库为空）**：注册首个账号（自动成为管理员）→ **参数设置** 查看/轮转 JWT 与注册开关 → **存储策略** 添加互相独立的 S3 兼容策略并配置各策略的每用户默认配额 → 即可上传。

### 5. 本地开发

```bash
edgeone makers dev
# 默认访问：http://127.0.0.1:8088/
```

该命令启动 EdgeOne 本地开发运行时（前端 + Cloud Functions 联调）。  
前端与函数同端口，**无需**再拆「独立 Go 后端 + Vite 代理」。

> **重要（官方文档）**  
> `edgeone makers dev` 会读取 `edgeone.json` 的 `devCommand`，若无则读取 `package.json` 的 `dev` 脚本启动前端。  
> **切勿**在 `package.json` 的 `dev` 或 `edgeone.json` 里再写 `edgeone makers dev`，否则会递归调用。  
> 本仓库 `package.json` 的 `dev` 仅为 `vite`，由 makers 调用。

### 6. 构建与部署

```bash
# 构建（前端 + cloud-functions 等，产出写入 .edgeone/）
edgeone makers build

# 构建并部署
edgeone makers deploy

# 部署为新项目 / 预览环境 / token
edgeone makers deploy -n <project-name>
edgeone makers deploy -e preview
edgeone makers deploy -t <token>
```

> 不要把 `edgeone makers build` / `deploy` 写进 `package.json` 的同名脚本再交给 makers 去调，避免递归。  
> `package.json` 里的 `build` 只负责前端（`tsc` + `vite build`），供 makers 在需要时调用。

## 环境变量

基础设施配置由 `internal/config/config.go` 从环境变量加载（数据库、端口）。  
JWT / 存储策略（含每策略配额）等业务项只写数据库，由前端页面操作。  
在 EdgeOne 上通过 Makers 环境变量注入；本地开发由 `edgeone makers dev` + `.env` 提供。

### 管理命令

| 命令 | 说明 |
|------|------|
| `edgeone makers env ls` | 列出远程环境变量 |
| `edgeone makers env set <KEY> <VALUE>` | 设置远程环境变量 |
| `edgeone makers env rm <KEY>` | 删除远程环境变量 |
| `edgeone makers env pull` | 拉取远程变量到本地 `.env` |

### 必填

无强制环境变量。JWT 与存储策略均在前端配置；见下方「JWT 与管理员」「存储策略」。

### JWT 与管理员

| 行为 | 说明 |
|------|------|
| 自动生成 | 启动时若数据库 `cloudreve_settings` 表中无 `jwt_secret`，则生成 32 字节随机密钥并持久化 |
| 环境变量 | **不支持**。不从 `JWT_SECRET` 读取或引导 |
| 查看 / 轮转 | 管理员登录后打开前端 **参数设置**，可查看当前主密钥并一键轮转 |
| 首个用户 | 系统中第一个注册用户自动成为管理员（`is_admin=true`） |
| 轮转效果 | 轮转后所有既有登录令牌立即失效，用户需重新登录 |

### 存储策略（仅前端配置）

与 Cloudreve 一致：管理员在 **存储策略** 页面添加 / 编辑 / 删除 S3 兼容策略，配置写入数据库并热加载，**无需环境变量、无需重启**。初次启动库为空，需自行在页面添加。

| 行为 | 说明 |
|------|------|
| 添加策略 | 名称、Bucket、Endpoint、Region、Access Key、Secret Key、每用户默认配额、是否默认 |
| 每策略配额 | `default_quota` 按策略独立；用量按用户+策略统计；`0` 表示未配置/不可用 |
| 默认策略 | 上传未指定时使用；删除默认策略时自动提升另一条 |
| 上传选择 | 用户端文件页可下拉选择已配置策略 |
| 策略独立 | 每套 S3 使用独立凭证、驱动与配额；某条初始化失败不影响其它策略 |
| 环境变量 | **不支持**。不从 `S3_*` / `S3_POLICIES` / `DEFAULT_QUOTA` 引导 |

### 服务

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 服务端口（EdgeOne / Makers 运行时会注入；Cloud Function 入口优先读此变量） | `8080`（config 默认）；入口未设置时回退 `9000` |

### 用户配额（跟随存储策略）

配额不是全局设置，而是**每个 S3 存储策略各自配置**的 `default_quota`（字节）：

| 行为 | 说明 |
|------|------|
| 配置入口 | 管理员 **存储策略** → 添加/编辑策略 →「每用户默认配额」 |
| 未设置 / 为 0 | 该策略下用户不可上传（配额不足） |
| 计量方式 | 按 `(user_id, storage_policy)` 汇总 `cloudreve_files.size`，与其它策略互不影响 |
| 用户字段 | `cloudreve_users.storage_quota` 固定为 `0`（兼容保留）；`storage_used` 仍为跨策略总用量 |

### 数据库表

全部表统一前缀 `cloudreve_`（GORM `TablePrefix`）：

| 表名 | 用途 |
|------|------|
| `cloudreve_users` | 用户 |
| `cloudreve_files` | 文件/文件夹元数据 |
| `cloudreve_shares` | 分享 |
| `cloudreve_settings` | 系统键值配置（JWT、注册开关等） |
| `cloudreve_storage_policies` | S3 存储策略 |

### 数据库

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DRIVER` | `sqlite` 或 `postgres` | `sqlite` |
| `DB_DSN` | 连接串。SQLite 为文件路径；PostgreSQL 为标准 DSN | `cloudreve.db` |

SQLite 使用纯 Go 驱动（`glebarez/sqlite` / `modernc.org/sqlite`），**不依赖 CGO**，可在 EdgeOne Cloud Functions（`CGO_ENABLED=0`）下编译运行。生产仍建议 PostgreSQL。

PostgreSQL 示例：

```bash
edgeone makers env set DB_DRIVER postgres
edgeone makers env set DB_DSN "host=127.0.0.1 user=cloudreve password=secret dbname=cloudreve port=5432 sslmode=disable"
```

#### EdgeOne Blob（说明，当前未接入）

EdgeOne 侧文件存储是 **Blob**，不是 COS。  
COS / MinIO / R2 等应走 **S3 兼容** 策略（前端添加），不要单独当成「EdgeOne 存储」。

官方说明（[Blob 存储 / Node.js SDK](https://cloud.tencent.com/document/product/1552/131425)）：仅 Node SDK；本仓库为 Go，Blob 驱动尚未实现。

### 配置示例

**SQLite（推荐最小配置；JWT/存储策略在前端配置）：**

```bash
edgeone makers env set DB_DRIVER sqlite
edgeone makers env set DB_DSN cloudreve.db

edgeone makers env pull
```

**PostgreSQL 生产库：**

```bash
edgeone makers env set DB_DRIVER postgres
edgeone makers env set DB_DSN "host=xxx user=xxx password=xxx dbname=cloudreve port=5432 sslmode=require"
```

## 项目结构

```
cloudreve-eo/
├── .edgeone/
│   └── cloud-functions/api-go/  # Makers 构建/部署的 Cloud Functions 产物
├── cloud-functions/
│   └── api.go                 # 本地/源码侧后端入口（由 Makers 运行时使用）
├── internal/                    # 后端业务代码
│   ├── config/                  # 环境变量配置
│   ├── handler/
│   ├── middleware/
│   ├── model/
│   ├── service/
│   └── storage/
├── src/                         # 前端源码
│   ├── api/
│   ├── components/
│   └── pages/
├── docs/superpowers/            # 设计文档与实现计划
├── package.json
├── go.mod
└── vite.config.ts
```

## API 一览

基础路径：`/api`

### 认证（公开）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/auth/register` | 注册 |
| POST | `/auth/login` | 登录，返回 JWT |

### 文件（需 `Authorization: Bearer <token>`）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/files?parent_id=0` | 列出目录 |
| POST | `/files/mkdir` | 创建文件夹 |
| POST | `/files/upload` | 获取上传预签名 URL（可选 `storage_policy`） |
| POST | `/files/upload/callback` | 上传完成回调（可选 `storage_policy`，应与上一步一致） |
| GET | `/files/:id/download` | 获取下载预签名 URL |
| DELETE | `/files/:id` | 删除文件/文件夹 |
| PUT | `/files/:id/rename` | 重命名 |
| PUT | `/files/:id/move` | 移动 |
| GET | `/storage/policies` | 列出已配置的存储策略（供上传选择，无密钥） |

### 分享

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/shares` | 创建分享（需登录） |
| GET | `/shares/:code` | 查看分享信息（公开） |
| GET | `/shares/:code/download` | 下载分享文件（公开） |

### 用户（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/user/profile` | 当前用户 + 各存储策略用量/配额（含 `is_admin`） |
| PUT | `/user/password` | 修改密码 |

### 站点（公开）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/site` | 站点公开信息（如 `allow_register`） |

### 管理员设置（需登录且 `is_admin`）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/settings/security` | 查看 JWT 主密钥与注册开关 |
| POST | `/settings/security/rotate-jwt` | 轮转 JWT 主密钥（旧令牌立即失效） |
| PUT | `/settings/register` | 设置是否允许新用户注册 `{"allow_register":true}` |
| GET | `/admin/storage/policies` | 列出全部存储策略（密钥脱敏，含 `default_quota`） |
| POST | `/admin/storage/policies` | 添加 S3 兼容策略（含 `default_quota`） |
| GET | `/admin/storage/policies/:id` | 策略详情（含密钥，编辑用） |
| PUT | `/admin/storage/policies/:id` | 更新策略（`secret_key` 空则不改） |
| DELETE | `/admin/storage/policies/:id` | 删除策略 |
| POST | `/admin/storage/policies/:id/default` | 设为默认策略 |

### 上传流程

```
前端                         后端（Cloud Function）         对象存储
 │                            │                              │
 │── POST /files/upload ─────▶│                              │
 │◀── 预签名 URL ────────────│                              │
 │                            │                              │
 │──────────── PUT（直传） ─────────────────────────────────▶│
 │                            │                              │
 │── POST /files/upload/callback ──▶│                        │
 │◀── 成功 ─────────────────│                              │
```

## 常用命令

### EdgeOne Makers CLI（主流程，请直接在终端执行）

| 命令 | 说明 |
|------|------|
| `edgeone makers dev` | **本地开发**（默认 `http://127.0.0.1:8088/`） |
| `edgeone makers build` | 构建前端 + Cloud Functions 到 `.edgeone/` |
| `edgeone makers deploy` | 部署到 EdgeOne Makers |
| `edgeone makers link` | 关联远程项目 |
| `edgeone makers env ls` | 列出远程环境变量 |
| `edgeone makers env set K V` | 设置远程环境变量 |
| `edgeone makers env pull` | 拉取环境变量到本地 `.env` |
| `edgeone login` / `whoami` | 登录 / 查看登录状态 |

### package.json scripts（给 makers 或本地工具调用，不是入口）

| 命令 | 说明 |
|------|------|
| `npm run dev` | **仅** `vite`（由 `edgeone makers dev` 内部调用，勿改成 makers 命令） |
| `npm run build` | **仅** 前端构建 `tsc -b && vite build` |
| `npm run preview` | 预览前端构建产物 |
| `npm run lint` | oxlint |

## 许可证

本项目为简化参考实现。请妥善保管 JWT 主密钥（参数设置页可见）与对象存储密钥，勿提交到版本库。

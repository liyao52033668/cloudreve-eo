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

至少需要一套可用的 S3 兼容存储。JWT 主密钥会在首次启动时自动生成并写入数据库，**不必**配置 `JWT_SECRET`。可用 CLI 写入远程，再 `env pull` 到本地：

```bash
edgeone makers env set DEFAULT_STORAGE s3
edgeone makers env set S3_ENDPOINT "https://你的-s3-endpoint"
edgeone makers env set S3_REGION "ap-guangzhou"
edgeone makers env set S3_BUCKET "your-bucket"
edgeone makers env set S3_ACCESS_KEY "your-access-key"
edgeone makers env set S3_SECRET_KEY "your-secret-key"

# 可选
edgeone makers env set DB_DRIVER sqlite
edgeone makers env set DB_DSN cloudreve.db
edgeone makers env set DEFAULT_QUOTA 1073741824
# 可选：仅在数据库尚无密钥时，用环境变量引导写入（一般不需要）
# edgeone makers env set JWT_SECRET "your-bootstrap-secret"

# 查看 / 同步到本地
edgeone makers env ls
edgeone makers env pull
```

也可直接编辑本地 `.env` 做开发调试；线上生效仍建议用 `edgeone makers env set`。

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

业务配置由 `internal/config/config.go` 从环境变量加载。  
在 EdgeOne 上通过 Makers 环境变量注入；本地开发由 `edgeone makers dev` + `.env` 提供。

### 管理命令

| 命令 | 说明 |
|------|------|
| `edgeone makers env ls` | 列出远程环境变量 |
| `edgeone makers env set <KEY> <VALUE>` | 设置远程环境变量 |
| `edgeone makers env rm <KEY>` | 删除远程环境变量 |
| `edgeone makers env pull` | 拉取远程变量到本地 `.env` |

### 必填

使用 S3 作为默认存储时，需配置下方 S3 相关变量（`DEFAULT_STORAGE` 默认为 `s3`）。  
JWT 主密钥会自动生成，见「JWT 与管理员」。

### JWT 与管理员

| 行为 | 说明 |
|------|------|
| 自动生成 | 启动时若数据库 `settings` 表中无 `jwt_secret`，则生成 32 字节随机密钥并持久化 |
| `JWT_SECRET` 环境变量 | **可选**。仅当库中尚无密钥时作为 bootstrap 写入；库中已有则忽略 |
| 查看 / 轮转 | 管理员登录后打开前端 **参数设置**，可查看当前主密钥并一键轮转 |
| 首个用户 | 系统中第一个注册用户自动成为管理员（`is_admin=true`） |
| 轮转效果 | 轮转后所有既有登录令牌立即失效，用户需重新登录 |

### 服务与配额

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 服务端口（EdgeOne / Makers 运行时会注入；Cloud Function 入口优先读此变量） | `8080`（config 默认）；入口未设置时回退 `9000` |
| `DEFAULT_QUOTA` | 新用户默认存储配额（字节） | `1073741824`（1 GiB） |
| `JWT_SECRET` | （可选）首次启动时引导写入的 JWT 主密钥；库中已有则忽略 | 自动生成 |

### 数据库

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DRIVER` | `sqlite` 或 `postgres` | `sqlite` |
| `DB_DSN` | 连接串。SQLite 为文件路径；PostgreSQL 为标准 DSN | `cloudreve.db` |

PostgreSQL 示例：

```bash
edgeone makers env set DB_DRIVER postgres
edgeone makers env set DB_DSN "host=127.0.0.1 user=cloudreve password=secret dbname=cloudreve port=5432 sslmode=disable"
```

### 存储策略（仅 S3 兼容，支持多套）

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DEFAULT_STORAGE` | 默认策略名（须与某套策略的 `name` 一致） | 单套时为 `s3`；多套且未设置时为列表第一项 |
| `S3_POLICIES` | **多套** S3 配置，JSON 数组（推荐） | （空） |
| `S3_ENDPOINT` 等 | **单套**兼容写法，见下 | （空） |

#### 方式 A：多套策略（推荐）

环境变量 `S3_POLICIES` 为 JSON 数组，每项：

| 字段 | 说明 |
|------|------|
| `name` | 策略标识（写入 `files.storage_policy`，上传时选择用） |
| `endpoint` | S3 端点 |
| `region` | 区域 |
| `bucket` | 桶名 |
| `access_key` | Access Key |
| `secret_key` | Secret Key |

```bash
edgeone makers env set DEFAULT_STORAGE minio
edgeone makers env set S3_POLICIES '[
  {"name":"minio","endpoint":"http://127.0.0.1:9001","region":"us-east-1","bucket":"cloudreve","access_key":"minioadmin","secret_key":"minioadmin"},
  {"name":"cos","endpoint":"https://cos.ap-guangzhou.myqcloud.com","region":"ap-guangzhou","bucket":"your-bucket","access_key":"AKID...","secret_key":"..."}
]'
```

- 可同时注册多套；上传时前端下拉选择，或 API 传 `storage_policy`
- 下载 / 删除按文件记录的 `storage_policy` 路由到对应驱动
- 列表接口：`GET /api/storage/policies`（需登录）

#### 方式 B：单套（兼容旧环境变量）

未设置 `S3_POLICIES` 时，使用下列变量，策略名固定为 `s3`：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `S3_ENDPOINT` | 端点 URL（MinIO / COS / R2 等） | （空） |
| `S3_REGION` | 区域 | （空） |
| `S3_BUCKET` | 桶名（非空才会注册该策略） | （空） |
| `S3_ACCESS_KEY` | Access Key | （空） |
| `S3_SECRET_KEY` | Secret Key | （空） |

#### EdgeOne Blob（说明，当前未接入）

EdgeOne 侧文件存储是 **Blob**，不是 COS。  
COS / MinIO / R2 等应走上方 **S3 兼容** 配置，不要单独当成「EdgeOne 存储」。

官方说明（[Blob 存储 / Node.js SDK](https://cloud.tencent.com/document/product/1552/131425)）：

- **当前仅提供 Node.js SDK**：`@edgeone/pages-blob`
- 其他运行时（含 Go）的 SDK **仍在开发中**
- 预签名上传：`store.createUploadUrl(key, { expireSeconds, contentType })`，客户端 PUT 直传
- 适用于 Makers Cloud Functions（Node）；不建议当公网图床 / 通用 CDN

本仓库后端是 **Go + Gin Cloud Function**，因此：

| 项 | 状态 |
|----|------|
| 真正的 Blob 驱动 | **未实现**（无官方 Go SDK，不能直接 `import @edgeone/pages-blob`） |
| 仓库里的 `internal/storage/edgeone.go` | **错误实现**：误把 EdgeOne 当成 COS S3 兼容端点，应忽略 / 待删除或重写 |
| `EDGEONE_BUCKET` / `EDGEONE_SECRET_ID` / `EDGEONE_SECRET_KEY` | 配置项遗留，**不对应**真实 Blob 用法（Blob 用 store 名 + 项目内凭证） |
| 现阶段可用存储 | 请使用 **`DEFAULT_STORAGE=s3`** + `S3_*` |

若以后要支持 Blob，可选方向：旁路 Node Cloud Function 调 Blob SDK、HTTP 调 Blob API（若有公开接口）、或等官方 Go SDK。

### 配置示例

**S3（COS / MinIO 等）+ SQLite：**

```bash
edgeone makers env set DB_DRIVER sqlite
edgeone makers env set DB_DSN cloudreve.db
edgeone makers env set DEFAULT_STORAGE s3
edgeone makers env set S3_ENDPOINT "https://cos.ap-guangzhou.myqcloud.com"
edgeone makers env set S3_REGION ap-guangzhou
edgeone makers env set S3_BUCKET your-bucket
edgeone makers env set S3_ACCESS_KEY your-secret-id
edgeone makers env set S3_SECRET_KEY your-secret-key
edgeone makers env set DEFAULT_QUOTA 1073741824

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
│   └── index.go                 # 本地/源码侧后端入口（由 Makers 运行时使用）
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
| GET | `/storage/policies` | 列出已配置的存储策略（供上传选择） |

### 分享

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/shares` | 创建分享（需登录） |
| GET | `/shares/:code` | 查看分享信息（公开） |
| GET | `/shares/:code/download` | 下载分享文件（公开） |

### 用户（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/user/profile` | 当前用户与存储用量（含 `is_admin`） |
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

# Cloudreve-EO

参考 [Cloudreve](https://github.com/cloudreve/Cloudreve) 的简化版云盘，面向 [EdgeOne Makers](https://cloud.tencent.com/product/teo) 部署。

- **前端**：React + Vite SPA（静态资源）
- **后端**：Go + Gin，作为 EdgeOne Cloud Functions 运行
- **文件传输**：预签名 URL 直连对象存储，不经后端中转

本项目**不是**独立的 Go 服务 + 独立前端开发模型；本地联调与线上部署都走 **EdgeOne Makers CLI**。

[![使用 EdgeOne Makers 部署](https://cdnstatic.tencentcs.com/edgeone/pages/deploy.svg)](https://console.cloud.tencent.com/edgeone/makers/new?repository-url=https://github.com/liyao52033668/cloudreve-eo)

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
| 存储 | S3 兼容对象存储 |
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

### 数据库

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DRIVER` | `sqlite` 或 `postgres` | `sqlite` |
| `DB_DSN` | 连接串。SQLite 为文件路径；PostgreSQL 为标准 DSN | `cloudreve.db` |

SQLite 使用纯 Go 驱动（`glebarez/sqlite` / `modernc.org/sqlite`），**不依赖 CGO**，可在 EdgeOne Cloud Functions（`CGO_ENABLED=0`）下编译运行。生产仍建议 PostgreSQL。

#### SQLite 持久化（`DB_PERSIST`）

云函数实例的本地磁盘不持久，重启后 SQLite 文件会丢失。可通过 `DB_PERSIST` 选择把数据库文件同步到远端：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_PERSIST` | `local`（仅本地文件，适合本地开发）/ `s3`（对象存储）/ `github`（GitHub 仓库） | `local` |
| `DB_PERSIST_INTERVAL` | 同步间隔（秒，最小 5） | `60` |

启动时若本地无数据库文件则先从远端恢复；运行期间通过 `VACUUM INTO` 生成一致性快照，内容有变化才上传。仅 `DB_DRIVER=sqlite` 时有效。

**`DB_PERSIST=s3`（任意 S3 兼容对象存储：COS / R2 / MinIO 等）：**

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_PERSIST_S3_BUCKET` | 存储桶（必填） | — |
| `DB_PERSIST_S3_ACCESS_KEY` | Access Key（必填） | — |
| `DB_PERSIST_S3_SECRET_KEY` | Secret Key（必填） | — |
| `DB_PERSIST_S3_ENDPOINT` | 自定义端点（MinIO/COS/R2 等） | 空（AWS 官方） |
| `DB_PERSIST_S3_REGION` | 区域 | `auto` |
| `DB_PERSIST_S3_KEY` | 对象键 | `cloudreve.db` |
| `DB_PERSIST_S3_PATH_STYLE` | `true` 时使用 path-style | `false` |

**`DB_PERSIST=github`（提交到仓库文件，适合小数据量个人使用）：**

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_PERSIST_GITHUB_TOKEN` | 具有 repo contents 写权限的 Token（必填） | — |
| `DB_PERSIST_GITHUB_REPO` | 仓库 `owner/repo`（必填，建议私有仓库） | — |
| `DB_PERSIST_GITHUB_BRANCH` | 分支 | `main` |
| `DB_PERSIST_GITHUB_PATH` | 仓库内文件路径 | `cloudreve.db` |

> 注意：数据库中含用户密码哈希与 S3 凭证，GitHub 后端务必使用**私有仓库**。GitHub Contents API 单文件上限约 100MB，且每次同步产生一次 commit，数据量大或写入频繁时请改用 `s3`。

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
| `edgeone makers env ls` | 列出远程环境变量 |
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

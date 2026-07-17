# Cloudreve-EO 设计文档

> 参考 Cloudreve 实现的简化版云盘，部署于 EdgeOne 平台。

## 1. 项目概述

Cloudreve-EO 是一个简化版云存储应用，参考 [Cloudreve](https://github.com/cloudreve/Cloudreve) 的核心功能设计，专为 EdgeOne 平台部署优化。

### 核心功能范围（基础版）

- 用户注册/登录
- 文件上传/下载/删除
- 文件夹管理（创建、重命名、移动）
- 文件列表浏览与导航
- 文件分享（生成链接 + 可选提取码）
- 存储策略管理（S3 兼容 + EdgeOne 对象存储）
- 文件预览（图片/文本）

## 2. 技术架构

### 整体架构

```
┌─────────────────────────────────────────────────┐
│                  EdgeOne 平台                     │
│                                                   │
│  ┌──────────────┐     ┌───────────────────────┐  │
│  │  静态资源托管  │     │   全栈应用 (Go 后端)    │  │
│  │  (React SPA) │────▶│   REST API 服务        │  │
│  └──────────────┘     └───────┬───────────────┘  │
│                               │                   │
│                    ┌──────────┼──────────┐       │
│                    ▼          ▼          ▼       │
│              ┌─────────┐ ┌────────┐ ┌────────┐  │
│              │ SQLite/  │ │  S3    │ │EdgeOne │  │
│              │PostgreSQL│ │兼容存储 │ │对象存储 │  │
│              └─────────┘ └────────┘ └────────┘  │
└─────────────────────────────────────────────────┘
```

### 技术选型

| 层 | 技术 |
|---|---|
| 后端 | Go + Gin + GORM |
| 前端 | React 18 + TypeScript + Vite + Ant Design |
| 数据库 | SQLite（默认）/ PostgreSQL（环境变量切换） |
| 存储 | S3 兼容存储 / EdgeOne 对象存储 |
| 认证 | JWT |
| 文件传输 | 预签名 URL 直连对象存储 |

### 关键设计决策

- **前后端分离**：前端部署到 EdgeOne 静态资源托管，后端部署为 EdgeOne 全栈应用
- **文件不经过后端中转**：上传/下载通过预签名 URL 直连对象存储，节省后端带宽
- **统一存储接口**：`StorageDriver` 抽象层，S3 和 EdgeOne 各一个实现
- **数据库可切换**：通过 `DB_DRIVER` 环境变量在 SQLite 和 PostgreSQL 之间切换

## 3. 数据模型

### users 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| username | string | 用户名，唯一 |
| password_hash | string | bcrypt 哈希 |
| storage_quota | int64 | 存储配额（字节） |
| storage_used | int64 | 已用存储 |
| created_at | time | 创建时间 |

### files 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| user_id | uint | 所属用户 |
| parent_id | uint | 父文件夹 ID，0 为根目录 |
| name | string | 文件/文件夹名 |
| is_dir | bool | 是否文件夹 |
| size | int64 | 文件大小（字节） |
| mime_type | string | MIME 类型 |
| storage_key | string | 存储后端中的对象 key |
| storage_policy | string | 存储策略标识（s3/edgeone） |
| created_at | time | 创建时间 |
| updated_at | time | 更新时间 |

### shares 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| user_id | uint | 创建者 |
| file_id | uint | 关联文件 |
| code | string | 分享短码，唯一 |
| password | string | 可选提取码 |
| expire_at | *time | 过期时间，null 为永久 |
| views | int | 浏览次数 |
| created_at | time | 创建时间 |

### 设计要点

- 文件树通过 `parent_id` 自引用实现虚拟目录结构
- `storage_key` 是文件在对象存储中的路径，格式：`{user_id}/{uuid}`
- `storage_policy` 标记文件存在哪个后端，下载时根据此字段路由到对应 Driver
- 原始文件名只存在数据库 `files.name` 中，对象存储中的 key 不含原始文件名

## 4. API 设计

### 认证模块

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/register` | 注册 |
| POST | `/api/auth/login` | 登录，返回 JWT |

### 文件模块

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/files` | 列出目录内容（`?parent_id=0`） |
| POST | `/api/files/mkdir` | 创建文件夹 |
| POST | `/api/files/upload` | 获取上传预签名 URL |
| POST | `/api/files/upload/callback` | 上传完成回调，写入文件记录 |
| GET | `/api/files/:id/download` | 获取下载预签名 URL |
| DELETE | `/api/files/:id` | 删除文件/文件夹 |
| PUT | `/api/files/:id/rename` | 重命名 |
| PUT | `/api/files/:id/move` | 移动到另一个目录 |

### 分享模块

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/shares` | 创建分享链接 |
| GET | `/api/shares/:code` | 获取分享信息（公开） |
| GET | `/api/shares/:code/download` | 下载分享文件（公开） |

### 用户模块

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/user/profile` | 获取当前用户信息（含存储用量） |
| PUT | `/api/user/password` | 修改密码 |

### 上传流程

```
前端                        后端                      对象存储
 │                           │                          │
 │── POST /files/upload ────▶│                          │
 │◀── 预签名 URL ───────────│                          │
 │                           │                          │
 │──────────── PUT (直传) ──────────────────────────────▶│
 │                           │                          │
 │── POST /files/upload/callback ──▶│                   │
 │                           │── 写入 files 记录 ──▶    │
 │◀── 成功 ────────────────│                          │
```

## 5. 存储抽象层

### 统一接口

```go
type StorageDriver interface {
    GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error)
    GenerateDownloadURL(key string, expire time.Duration) (string, error)
    Delete(key string) error
    GetSize(key string) (int64, error)
}
```

### S3Driver

使用 AWS SDK，兼容所有 S3 协议的存储（MinIO、COS、R2 等）。

环境变量：
- `S3_ENDPOINT`
- `S3_REGION`
- `S3_BUCKET`
- `S3_ACCESS_KEY`
- `S3_SECRET_KEY`

### EdgeOneDriver

使用 EdgeOne 对象存储 API。

环境变量：
- `EDGEONE_BUCKET`
- `EDGEONE_SECRET_ID`
- `EDGEONE_SECRET_KEY`

### 存储策略路由

```go
type StoragePolicyManager struct {
    defaultDriver StorageDriver
    drivers       map[string]StorageDriver
}
```

- `DEFAULT_STORAGE=s3|edgeone` 设置默认策略
- 每个文件记录 `storage_policy` 字段，下载时路由到对应 Driver

### 对象 Key 规则

```
{user_id}/{uuid}
```

例如 `1/550e8400-e29b-41d4-a716-446655440000`。

## 6. 前端设计

### 页面清单

| 页面 | 路径 | 说明 |
|------|------|------|
| 登录 | `/login` | 登录表单 |
| 注册 | `/register` | 注册表单 |
| 文件管理 | `/` | 主页面，文件列表/文件夹导航 |
| 分享查看 | `/share/:code` | 公开页面，查看/下载分享文件 |

### 文件管理页面布局

```
┌─────────────────────────────────────────┐
│  Cloudreve-EO              [用户] [退出] │
├─────────────────────────────────────────┤
│  路径导航:  根 / 文档 / 工作             │
├─────────────────────────────────────────┤
│  [+ 上传]  [+ 新建文件夹]               │
├─────────────────────────────────────────┤
│  ☐  工作/                2024-01-15     │
│  ☐  文档/                2024-01-14     │
│  ☐  报告.pdf    2.3MB   2024-01-13     │
│  ☐  照片.png    1.1MB   2024-01-12     │
├─────────────────────────────────────────┤
│  已用: 3.4MB / 1GB                     │
└─────────────────────────────────────────┘
```

### 交互功能

- 拖拽上传 + 点击上传按钮，显示进度条
- 下载通过预签名 URL 直连存储
- 操作菜单：重命名、移动、删除、分享
- 面包屑导航
- 文件预览：图片直接预览，文本文件弹窗查看
- 分享弹窗：生成链接 + 可选提取码 + 过期时间

### 前端技术栈

- React 18 + TypeScript
- Vite 构建
- React Router 路由
- Axios HTTP 请求
- Ant Design UI 组件库

## 7. 环境变量汇总

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DRIVER` | 数据库驱动 | `sqlite` |
| `DB_DSN` | 数据库连接字符串 | `cloudreve.db` |
| `JWT_SECRET` | JWT 签名密钥 | （必填） |
| `DEFAULT_STORAGE` | 默认存储策略 | `s3` |
| `S3_ENDPOINT` | S3 端点 | — |
| `S3_REGION` | S3 区域 | — |
| `S3_BUCKET` | S3 桶名 | — |
| `S3_ACCESS_KEY` | S3 访问密钥 | — |
| `S3_SECRET_KEY` | S3 密钥 | — |
| `EDGEONE_BUCKET` | EdgeOne 桶名 | — |
| `EDGEONE_SECRET_ID` | EdgeOne Secret ID | — |
| `EDGEONE_SECRET_KEY` | EdgeOne Secret Key | — |
| `DEFAULT_QUOTA` | 默认用户配额（字节） | `1073741824`（1GB） |
| `PORT` | 服务端口 | `8080` |

## 8. 项目结构

```
cloudreve-eo/
├── cmd/
│   └── server/
│       └── main.go              # 入口
├── internal/
│   ├── config/                  # 配置加载
│   ├── model/                   # 数据模型
│   ├── handler/                 # HTTP handlers
│   │   ├── auth.go
│   │   ├── file.go
│   │   ├── share.go
│   │   └── user.go
│   ├── middleware/               # JWT 中间件等
│   ├── service/                 # 业务逻辑
│   └── storage/                 # 存储驱动
│       ├── driver.go            # StorageDriver 接口
│       ├── s3.go
│       ├── edgeone.go
│       └── manager.go           # StoragePolicyManager
├── web/                         # 前端项目
│   ├── src/
│   │   ├── api/                 # API 调用
│   │   ├── components/          # 组件
│   │   ├── pages/               # 页面
│   │   ├── stores/              # 状态管理
│   │   └── App.tsx
│   ├── package.json
│   └── vite.config.ts
├── go.mod
├── go.sum
└── Makefile
```

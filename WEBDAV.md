# WebDAV 服务

Cloudreve-EO 现在支持对外提供 WebDAV 协议服务，允许第三方客户端（如 Rclone、Windows 资源管理器、macOS Finder）挂载访问您的云盘文件。

## 功能特性

- ✅ 支持标准 WebDAV 协议（RFC 4918）
- ✅ HTTP Basic Auth 认证（用户名 + WebDAV 专用密码）
- ✅ 文件/目录的增删改查（PROPFIND、GET、PUT、DELETE、MKCOL、MOVE）
- ✅ 与现有存储后端完全集成（S3、WebDAV、GitHub、Filen 等）
- ✅ 配额管理（继承用户组配额限制）
- ✅ 管理员全局开关控制

## 使用方法

### 1. 启用 WebDAV 服务

管理员登录后台，进入「参数设置」页面：

1. 找到「WebDAV 服务」卡片
2. 开启「启用 WebDAV 服务」开关
3. 设置 WebDAV 访问地址（系统自动生成，格式：`https://your-domain.com/api/dav/`）

### 2. 设置 WebDAV 密码

每个用户需要单独设置 WebDAV 密码（与登录密码独立）：

1. 进入「参数设置」页面
2. 在「WebDAV 服务」卡片中找到「WebDAV 密码」
3. 输入新密码（至少 6 位）并点击「设置密码」

### 3. 客户端配置

#### Windows 资源管理器

1. 右键「此电脑」→「映射网络驱动器」
2. 文件夹填入：`https://your-domain.com/api/dav/`
3. 勾选「使用其他凭据连接」
4. 输入用户名和 WebDAV 密码

#### macOS Finder

1. Finder → 前往 → 连接服务器（⌘K）
2. 服务器地址：`https://your-domain.com/api/dav/`
3. 选择「注册用户」，输入用户名和 WebDAV 密码

#### Rclone

```bash
rclone config
# 选择 "WebDAV"
# URL: https://your-domain.com/api/dav/
# Vendor: other
# User: your-username
# Password: your-webdav-password
```

#### Cyberduck

1. 新建连接 → WebDAV (HTTPS)
2. 服务器：`your-domain.com`
3. 路径：`/api/dav/`
4. 用户名和密码

## 技术实现

### 后端架构

- **协议层**：使用 `golang.org/x/net/webdav` 标准库实现 WebDAV 服务端
- **认证**：HTTP Basic Auth，密码使用 bcrypt 加密存储
- **文件系统**：实现 `webdav.FileSystem` 接口，映射到用户文件树
- **存储集成**：复用现有 `StorageDriver` 接口，支持所有存储后端

### 数据模型

- `User.WebDAVPassword`：用户 WebDAV 密码（bcrypt hash）
- `Setting.webdav_enabled`：全局 WebDAV 服务开关

### 路由

- 外部访问路径：`/api/dav/*`
- EdgeOne 剥离 `/api` 前缀后，gin 收到 `/dav/*`
- WebDAV handler 使用 `Prefix: "/dav"`，FileSystem 处理用户根目录下的相对路径

### 关键文件

- `internal/handler/webdav_server.go`：WebDAV 服务端实现
- `internal/model/user.go`：用户模型（添加 WebDAVPassword 字段）
- `internal/model/setting.go`：系统设置（添加 webdav_enabled）
- `internal/handler/setting.go`：WebDAV 设置 API
- `internal/handler/user.go`：用户 WebDAV 密码 API
- `src/pages/Settings.tsx`：前端设置页面
- `src/api/webdav.ts`：前端 WebDAV API
- `cloud-functions/api.go`：路由注册

## 安全注意事项

1. **HTTPS 必须**：WebDAV 使用 Basic Auth，密码明文传输，必须启用 HTTPS
2. **独立密码**：WebDAV 密码与登录密码独立，降低泄露风险
3. **全局开关**：管理员可随时禁用 WebDAV 服务
4. **用户封禁**：被封禁用户无法使用 WebDAV

## 限制

### 文件大小限制（重要）

**WebDAV PUT 上传限制为 5MB**。这是由 EdgeOne 云函数网关的 6MB 请求体限制决定的。

- **原因**：EdgeOne 云函数网关限制单次请求 body ≤6MB。WebDAV PUT 请求体是完整文件内容，超过 6MB 会被网关直接拒绝（413 错误），云函数根本接收不到。
- **解决方案**：大文件请使用**网页端上传**（网页端已实现分片上传，能绕开 6MB 限制）。
- **对比**：项目作为 WebDAV **客户端**（把外部 WebDAV 当存储后端）时，已经用 5MB 分片策略绕过了这个限制。但作为 WebDAV **服务端**时，无法控制客户端如何发送请求。

### 其他限制

- 不支持 LOCK/UNLOCK（无锁文件系统）
- 不支持 PROPPATCH（自定义属性）
- 不支持 Range 读取的流式优化（简单实现）

## 故障排查

### 无法连接

1. 检查 WebDAV 服务是否已启用
2. 检查用户名和密码是否正确
3. 检查 HTTPS 证书是否有效
4. 查看后端日志（logx 输出）

### 文件操作失败

1. 检查用户配额是否充足
2. 检查存储后端是否正常
3. 查看后端日志中的错误信息

### 性能问题

1. 大文件上传/下载会占用较多内存
2. 目录列表操作涉及多次数据库查询
3. 考虑使用客户端缓存减少请求

## 未来改进

- [ ] 支持 LOCK/UNLOCK
- [ ] 支持 PROPPATCH
- [ ] 流式上传（避免内存缓冲）
- [ ] Range 读取优化
- [ ] 连接数限制
- [ ] 访问日志审计
- [ ] 只读模式

### 数据库

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DRIVER` | `sqlite` 或 `postgres` | `sqlite` |
| `DB_DSN` | 连接串。SQLite 为文件路径；PostgreSQL 为标准 DSN | `cloudreve.db`（`DB_PERSIST=edgeone-blob` 时为 `/tmp/cloudreve.db`） |

SQLite 使用纯 Go 驱动（`glebarez/sqlite` / `modernc.org/sqlite`），**不依赖 CGO**，可在 EdgeOne Cloud Functions（`CGO_ENABLED=0`）下编译运行。生产仍建议 PostgreSQL。

#### SQLite 持久化（`DB_PERSIST`）

云函数实例的本地磁盘不持久，重启后 SQLite 文件会丢失。可通过 `DB_PERSIST` 选择把数据库文件同步到远端：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_PERSIST` | `local`（仅本地文件，适合本地开发）/ `s3`（对象存储）/ `github`（GitHub 仓库）/ `edgeone-blob`（EdgeOne Blob） | `local` |
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

> 注意：数据库中含用户密码哈希与 S3 凭证，GitHub 后端务必使用**私有仓库**。GitHub Contents API 单文件上限约 100MB，且每次同步产生一次 commit，数据量大或写入频繁时请改用 `s3` 或 `edgeone-blob`。

**`DB_PERSIST=edgeone-blob`（存到 EdgeOne Makers Blob，免第三方对象存储）：**

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_PERSIST_EDGEONE_SECRET` | 共享密钥（必填） | — |
| `DB_PERSIST_EDGEONE_BASE_URL` | 站点地址（可选，末尾不带 `/`）；留空时由首个请求的 `Host` 头自动推导 | 自动推导 |

Blob SDK（`@edgeone/pages-blob`）仅有 Node 版本，因此本方案由 Node 云函数 `cloud-functions/db-blob.js` 代理读写：Go 主程序下载快照时直接 `GET /db-blob`，上传时先向该函数请求预签名 URL 再直写 Blob（绕开云函数 6MB 请求体上限）。

> EdgeOne 云函数工作目录只读，仅 `/tmp` 可写。使用本后端时若未显式配置 `DB_DSN`，默认数据库文件与一致性快照会自动落在 `/tmp/cloudreve.db`，无需手动设置；若显式配置了相对路径会因无法写文件而报 `unable to open database file`，请改用 `/tmp/` 前缀的绝对路径。

> Go 与 Node 是两个独立的函数运行时，共用同一对外域名（边缘按路径分发），进程间没有内部通道，Go 只能通过该域名回调 Node 函数。由于平台不向函数注入站点域名，未显式配置 `DB_PERSIST_EDGEONE_BASE_URL` 时采用**懒恢复**：初始化推迟到首个请求，从请求 `Host` 头自动推导站点地址再恢复数据库，因此通常**无需手动填地址**。`/db-blob` 是公开路由，`DB_PERSIST_EDGEONE_SECRET` 用于鉴权，防止数据库（含密码哈希、S3 凭证）被任意下载或覆盖。环境变量是项目级的，两个运行时读取同一份，每个变量只需设置一次。

```bash
edgeone makers env set DB_PERSIST edgeone-blob
edgeone makers env set DB_PERSIST_EDGEONE_SECRET <随机长密钥>
```

> ⚠️ 懒恢复依赖回调能访问站点域名。若站点只有 EdgeOne 临时预览地址（`https://xxx.edgeone.cool?eo_token=...`），Go 的服务端回调不带 `eo_token` 会被边缘拦截（401），请绑定自定义域名后使用本后端，或改用 `s3` / `github`。Blob 免费版单账户容量 1GB，超出请改用 `s3`。

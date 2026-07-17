# Cloudreve-EO Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现一个简化版云盘，支持用户管理、文件上传下载、文件夹管理、文件分享，部署于 EdgeOne 平台。

**Architecture:** Go (Gin) 后端提供 REST API，React (Vite) 前端 SPA。文件通过预签名 URL 直连 S3/EdgeOne 对象存储，不经过后端中转。GORM 抽象数据库层，支持 SQLite/PostgreSQL 切换。

**Tech Stack:** Go 1.22+, Gin, GORM, AWS SDK Go v2, React 18, TypeScript, Vite, Ant Design, React Router, Axios, JWT (golang-jwt)

## Global Constraints

- Go 1.22+，使用标准库 `net/http` 兼容写法
- 前端 Node.js 18+，npm 包管理
- 所有 API 路径前缀 `/api/`
- JWT 认证，中间件统一校验（分享公开接口除外）
- 文件上传/下载通过预签名 URL 直连对象存储
- 对象 Key 格式：`{user_id}/{uuid}`
- 环境变量驱动配置，无配置文件

---

### Task 1: 项目初始化与配置模块

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`
- Create: `internal/config/config.go`
- Create: `Makefile`

**Interfaces:**
- Produces: `config.Config` struct，后续所有模块依赖

- [ ] **Step 1: 初始化 Go module**

```bash
cd /home/huazong/clouddreve-eo
go mod init github.com/cloudreve-eo/cloudreve-eo
```

- [ ] **Step 2: 安装依赖**

```bash
go get github.com/gin-gonic/gin
go get gorm.io/gorm
go get gorm.io/driver/sqlite
go get gorm.io/driver/postgres
go get github.com/golang-jwt/jwt/v5
go get github.com/aws/aws-sdk-go-v2
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/credentials
go get github.com/aws/aws-sdk-go-v2/service/s3
go get github.com/google/uuid
go get golang.org/x/crypto/bcrypt
```

- [ ] **Step 3: 编写配置模块**

创建 `internal/config/config.go`：

```go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DB     DBConfig
	JWT    JWTConfig
	S3     S3Config
	EdgeOne EdgeOneConfig
	Server ServerConfig
	Storage StorageConfig
}

type DBConfig struct {
	Driver string
	DSN    string
}

type JWTConfig struct {
	Secret string
}

type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

type EdgeOneConfig struct {
	Bucket    string
	SecretID  string
	SecretKey string
}

type ServerConfig struct {
	Port string
}

type StorageConfig struct {
	Default      string
	DefaultQuota int64
}

func Load() (*Config, error) {
	quota := int64(1073741824)
	if q := os.Getenv("DEFAULT_QUOTA"); q != "" {
		v, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid DEFAULT_QUOTA: %w", err)
		}
		quota = v
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbDriver := os.Getenv("DB_DRIVER")
	if dbDriver == "" {
		dbDriver = "sqlite"
	}

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		dbDSN = "cloudreve.db"
	}

	defaultStorage := os.Getenv("DEFAULT_STORAGE")
	if defaultStorage == "" {
		defaultStorage = "s3"
	}

	return &Config{
		DB: DBConfig{
			Driver: dbDriver,
			DSN:    dbDSN,
		},
		JWT: JWTConfig{
			Secret: os.Getenv("JWT_SECRET"),
		},
		S3: S3Config{
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			Region:    os.Getenv("S3_REGION"),
			Bucket:    os.Getenv("S3_BUCKET"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
		},
		EdgeOne: EdgeOneConfig{
			Bucket:    os.Getenv("EDGEONE_BUCKET"),
			SecretID:  os.Getenv("EDGEONE_SECRET_ID"),
			SecretKey: os.Getenv("EDGEONE_SECRET_KEY"),
		},
		Server: ServerConfig{
			Port: port,
		},
		Storage: StorageConfig{
			Default:      defaultStorage,
			DefaultQuota: quota,
		},
	}, nil
}
```

- [ ] **Step 4: 编写 main.go 骨架**

创建 `cmd/server/main.go`：

```go
package main

import (
	"fmt"
	"log"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	fmt.Printf("Cloudreve-EO 启动中，端口: %s\n", cfg.Server.Port)
	// 后续任务会逐步填充：数据库初始化、路由注册、服务启动
}
```

- [ ] **Step 5: 编写 Makefile**

创建 `Makefile`：

```makefile
.PHONY: build run test clean

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./...

clean:
	rm -rf bin/
```

- [ ] **Step 6: 验证编译通过**

Run: `make build`
Expected: 编译成功，生成 `bin/server`

- [ ] **Step 7: Commit**

```bash
git init
git add go.mod go.sum cmd/server/main.go internal/config/config.go Makefile
git commit -m "feat: 项目初始化，配置模块骨架"
```

---

### Task 2: 数据模型与数据库初始化

**Files:**
- Create: `internal/model/user.go`
- Create: `internal/model/file.go`
- Create: `internal/model/share.go`
- Create: `internal/model/db.go`

**Interfaces:**
- Consumes: `config.Config`
- Produces: `model.User`, `model.File`, `model.Share` structs；`model.InitDB(cfg)` 函数

- [ ] **Step 1: 编写 User 模型**

创建 `internal/model/user.go`：

```go
package model

import "time"

type User struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Username     string `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string `gorm:"size:128;not null" json:"-"`
	StorageQuota int64  `gorm:"not null;default:1073741824" json:"storage_quota"`
	StorageUsed  int64  `gorm:"not null;default:0" json:"storage_used"`
	CreatedAt    time.Time `json:"created_at"`
}
```

- [ ] **Step 2: 编写 File 模型**

创建 `internal/model/file.go`：

```go
package model

import "time"

type File struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	UserID        uint   `gorm:"index;not null" json:"user_id"`
	ParentID      uint   `gorm:"index;not null;default:0" json:"parent_id"`
	Name          string `gorm:"size:255;not null" json:"name"`
	IsDir         bool   `gorm:"not null;default:false" json:"is_dir"`
	Size          int64  `gorm:"not null;default:0" json:"size"`
	MimeType      string `gorm:"size:128" json:"mime_type"`
	StorageKey    string `gorm:"size:512" json:"storage_key"`
	StoragePolicy string `gorm:"size:32" json:"storage_policy"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
```

- [ ] **Step 3: 编写 Share 模型**

创建 `internal/model/share.go`：

```go
package model

import "time"

type Share struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"index;not null" json:"user_id"`
	FileID    uint       `gorm:"index;not null" json:"file_id"`
	Code      string     `gorm:"uniqueIndex;size:16;not null" json:"code"`
	Password  string     `gorm:"size:16" json:"-"`
	ExpireAt  *time.Time `json:"expire_at"`
	Views     int        `gorm:"not null;default:0" json:"views"`
	CreatedAt time.Time  `json:"created_at"`
}
```

- [ ] **Step 4: 编写数据库初始化**

创建 `internal/model/db.go`：

```go
package model

import (
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB(cfg *config.Config) error {
	var dialector gorm.Dialector

	switch cfg.DB.Driver {
	case "sqlite":
		dialector = sqlite.Open(cfg.DB.DSN)
	case "postgres":
		dialector = postgres.Open(cfg.DB.DSN)
	default:
		return fmt.Errorf("不支持的数据库驱动: %s", cfg.DB.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	if err := db.AutoMigrate(&User{}, &File{}, &Share{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	DB = db
	return nil
}
```

- [ ] **Step 5: 更新 main.go 集成数据库**

修改 `cmd/server/main.go`：

```go
package main

import (
	"fmt"
	"log"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if err := model.InitDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	fmt.Printf("Cloudreve-EO 启动中，端口: %s\n", cfg.Server.Port)
}
```

- [ ] **Step 6: 验证编译通过**

Run: `make build`
Expected: 编译成功

- [ ] **Step 7: Commit**

```bash
git add internal/model/ cmd/server/main.go
git commit -m "feat: 数据模型与数据库初始化"
```

---

### Task 3: 存储抽象层 — 接口与 S3Driver

**Files:**
- Create: `internal/storage/driver.go`
- Create: `internal/storage/s3.go`
- Create: `internal/storage/manager.go`

**Interfaces:**
- Produces: `storage.StorageDriver` 接口、`storage.S3Driver`、`storage.StoragePolicyManager`

- [ ] **Step 1: 编写 StorageDriver 接口**

创建 `internal/storage/driver.go`：

```go
package storage

import "time"

type StorageDriver interface {
	GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error)
	GenerateDownloadURL(key string, expire time.Duration) (string, error)
	Delete(key string) error
	GetSize(key string) (int64, error)
}
```

- [ ] **Step 2: 编写 S3Driver**

创建 `internal/storage/s3.go`：

```go
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Driver struct {
	client *s3.Client
	bucket string
}

func NewS3Driver(endpoint, region, bucket, accessKey, secretKey string) (*S3Driver, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(
		func(service, reg string, options ...interface{}) (aws.Endpoint, error) {
			if endpoint != "" {
				return aws.Endpoint{URL: endpoint}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		},
	)

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
		awsconfig.WithEndpointResolverWithOptions(resolver),
	)
	if err != nil {
		return nil, fmt.Errorf("加载 S3 配置失败: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return &S3Driver{client: client, bucket: bucket}, nil
}

func (d *S3Driver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	presigner := s3.NewPresignClient(d.client)
	result, err := presigner.PresignPutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(d.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("生成上传 URL 失败: %w", err)
	}
	return result.URL, nil
}

func (d *S3Driver) GenerateDownloadURL(key string, expire time.Duration) (string, error) {
	presigner := s3.NewPresignClient(d.client)
	result, err := presigner.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("生成下载 URL 失败: %w", err)
	}
	return result.URL, nil
}

func (d *S3Driver) Delete(key string) error {
	_, err := d.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("删除对象失败: %w", err)
	}
	return nil
}

func (d *S3Driver) GetSize(key string) (int64, error) {
	result, err := d.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("获取对象大小失败: %w", err)
	}
	return *result.ContentLength, nil
}

// 确保 S3Driver 实现 StorageDriver 接口
var _ StorageDriver = (*S3Driver)(nil)

// 避免未使用 import 的编译错误
var _ s3types.StorageClass
```

- [ ] **Step 3: 编写 StoragePolicyManager**

创建 `internal/storage/manager.go`：

```go
package storage

import (
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

type StoragePolicyManager struct {
	defaultDriver StorageDriver
	defaultPolicy string
	drivers       map[string]StorageDriver
}

func NewStoragePolicyManager(cfg *config.Config) (*StoragePolicyManager, error) {
	mgr := &StoragePolicyManager{
		drivers:       make(map[string]StorageDriver),
		defaultPolicy: cfg.Storage.Default,
	}

	if cfg.Storage.Default == "s3" || cfg.S3.Bucket != "" {
		driver, err := NewS3Driver(
			cfg.S3.Endpoint,
			cfg.S3.Region,
			cfg.S3.Bucket,
			cfg.S3.AccessKey,
			cfg.S3.SecretKey,
		)
		if err != nil {
			return nil, fmt.Errorf("初始化 S3 驱动失败: %w", err)
		}
		mgr.drivers["s3"] = driver
		if cfg.Storage.Default == "s3" {
			mgr.defaultDriver = driver
		}
	}

	if cfg.Storage.Default == "edgeone" || cfg.EdgeOne.Bucket != "" {
		driver, err := NewEdgeOneDriver(
			cfg.EdgeOne.Bucket,
			cfg.EdgeOne.SecretID,
			cfg.EdgeOne.SecretKey,
		)
		if err != nil {
			return nil, fmt.Errorf("初始化 EdgeOne 驱动失败: %w", err)
		}
		mgr.drivers["edgeone"] = driver
		if cfg.Storage.Default == "edgeone" {
			mgr.defaultDriver = driver
		}
	}

	if mgr.defaultDriver == nil {
		return nil, fmt.Errorf("默认存储策略 %s 未配置", cfg.Storage.Default)
	}

	return mgr, nil
}

func (m *StoragePolicyManager) DefaultDriver() StorageDriver {
	return m.defaultDriver
}

func (m *StoragePolicyManager) DefaultPolicy() string {
	return m.defaultPolicy
}

func (m *StoragePolicyManager) GetDriver(policy string) (StorageDriver, error) {
	driver, ok := m.drivers[policy]
	if !ok {
		return nil, fmt.Errorf("存储策略 %s 不存在", policy)
	}
	return driver, nil
}
```

- [ ] **Step 4: 验证编译**

Run: `make build`
Expected: 编译失败（EdgeOneDriver 尚未实现，下一步实现）

- [ ] **Step 5: Commit**

```bash
git add internal/storage/
git commit -m "feat: 存储抽象层接口与 S3Driver"
```

---

### Task 4: 存储抽象层 — EdgeOneDriver

**Files:**
- Create: `internal/storage/edgeone.go`

**Interfaces:**
- Consumes: `storage.StorageDriver` 接口
- Produces: `storage.EdgeOneDriver`

- [ ] **Step 1: 编写 EdgeOneDriver**

EdgeOne 对象存储兼容 S3 协议，因此 EdgeOneDriver 内部复用 S3Driver。

创建 `internal/storage/edgeone.go`：

```go
package storage

import (
	"fmt"
	"time"
)

type EdgeOneDriver struct {
	s3 *S3Driver
}

func NewEdgeOneDriver(bucket, secretID, secretKey string) (*EdgeOneDriver, error) {
	// EdgeOne 对象存储兼容 S3 协议
	// endpoint 格式根据实际 EdgeOne 文档配置
	endpoint := fmt.Sprintf("https://cos.%s.myqcloud.com", bucket)
	s3Driver, err := NewS3Driver(endpoint, "", bucket, secretID, secretKey)
	if err != nil {
		return nil, fmt.Errorf("初始化 EdgeOne 驱动失败: %w", err)
	}
	return &EdgeOneDriver{s3: s3Driver}, nil
}

func (d *EdgeOneDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return d.s3.GenerateUploadURL(key, contentType, expire)
}

func (d *EdgeOneDriver) GenerateDownloadURL(key string, expire time.Duration) (string, error) {
	return d.s3.GenerateDownloadURL(key, expire)
}

func (d *EdgeOneDriver) Delete(key string) error {
	return d.s3.Delete(key)
}

func (d *EdgeOneDriver) GetSize(key string) (int64, error) {
	return d.s3.GetSize(key)
}

var _ StorageDriver = (*EdgeOneDriver)(nil)
```

- [ ] **Step 2: 验证编译通过**

Run: `make build`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/storage/edgeone.go
git commit -m "feat: EdgeOne 存储驱动"
```

---

### Task 5: JWT 中间件与认证 Handler

**Files:**
- Create: `internal/middleware/auth.go`
- Create: `internal/handler/auth.go`
- Create: `internal/service/auth_service.go`

**Interfaces:**
- Consumes: `model.User`, `model.DB`, `config.JWTConfig`
- Produces: `handler.AuthHandler`，`middleware.JWTAuth()` 中间件

- [ ] **Step 1: 编写 JWT 工具与中间件**

创建 `internal/middleware/auth.go`：

```go
package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uint, secret string) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未提供认证信息"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证格式错误"})
			c.Abort()
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(parts[1], claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证无效或已过期"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Next()
	}
}
```

- [ ] **Step 2: 编写认证 Service**

创建 `internal/service/auth_service.go`：

```go
package service

import (
	"errors"
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	cfg *config.Config
}

func NewAuthService(cfg *config.Config) *AuthService {
	return &AuthService{cfg: cfg}
}

func (s *AuthService) Register(username, password string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	user := &model.User{
		Username:     username,
		PasswordHash: string(hash),
		StorageQuota: s.cfg.Storage.DefaultQuota,
	}

	if err := model.DB.Create(user).Error; err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

func (s *AuthService) Login(username, password string) (*model.User, error) {
	var user model.User
	if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	return &user, nil
}
```

- [ ] **Step 3: 编写认证 Handler**

创建 `internal/handler/auth.go`：

```go
package handler

import (
	"net/http"

	"github.com/cloudreve-eo/cloudreve-eo/internal/middleware"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService *service.AuthService
	jwtSecret   string
}

func NewAuthHandler(authService *service.AuthService, jwtSecret string) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		jwtSecret:   jwtSecret,
	}
}

type registerRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	user, err := h.authService.Register(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	token, err := middleware.GenerateToken(user.ID, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token": token,
		"user":  user,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	user, err := h.authService.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	token, err := middleware.GenerateToken(user.ID, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}
```

- [ ] **Step 4: 验证编译**

Run: `make build`
Expected: 编译成功

- [ ] **Step 5: Commit**

```bash
git add internal/middleware/ internal/service/auth_service.go internal/handler/auth.go
git commit -m "feat: JWT 认证中间件与登录注册"
```

---

### Task 6: 文件管理 Service 与 Handler

**Files:**
- Create: `internal/service/file_service.go`
- Create: `internal/handler/file.go`

**Interfaces:**
- Consumes: `model.File`, `model.User`, `storage.StoragePolicyManager`, `model.DB`
- Produces: `handler.FileHandler`

- [ ] **Step 1: 编写文件 Service**

创建 `internal/service/file_service.go`：

```go
package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FileService struct {
	storageMgr *storage.StoragePolicyManager
}

func NewFileService(mgr *storage.StoragePolicyManager) *FileService {
	return &FileService{storageMgr: mgr}
}

func (s *FileService) ListFiles(userID uint, parentID uint) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND parent_id = ?", userID, parentID).
		Order("is_dir DESC, name ASC").
		Find(&files).Error
	return files, err
}

func (s *FileService) Mkdir(userID uint, parentID uint, name string) (*model.File, error) {
	dir := &model.File{
		UserID:   userID,
		ParentID: parentID,
		Name:     name,
		IsDir:    true,
	}
	if err := model.DB.Create(dir).Error; err != nil {
		return nil, err
	}
	return dir, nil
}

func (s *FileService) GetUploadURL(userID uint, fileName string, contentType string) (string, string, error) {
	key := fmt.Sprintf("%d/%s", userID, uuid.New().String())
	policy := s.storageMgr.DefaultPolicy()
	driver := s.storageMgr.DefaultDriver()

	url, err := driver.GenerateUploadURL(key, contentType, 30*time.Minute)
	if err != nil {
		return "", "", err
	}
	return url, key, nil
}

func (s *FileService) UploadCallback(userID uint, parentID uint, fileName, storageKey string, size int64, mimeType string) (*model.File, error) {
	file := &model.File{
		UserID:        userID,
		ParentID:      parentID,
		Name:          fileName,
		IsDir:         false,
		Size:          size,
		MimeType:      mimeType,
		StorageKey:    storageKey,
		StoragePolicy: s.storageMgr.DefaultPolicy(),
	}

	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(file).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			Update("storage_used", gorm.Expr("storage_used + ?", size)).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (s *FileService) GetDownloadURL(userID uint, fileID uint) (string, error) {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("文件不存在")
		}
		return "", err
	}
	if file.IsDir {
		return "", errors.New("不能下载文件夹")
	}

	driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
	if err != nil {
		return "", err
	}
	return driver.GenerateDownloadURL(file.StorageKey, 30*time.Minute)
}

func (s *FileService) Delete(userID uint, fileID uint) error {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("文件不存在")
		}
		return err
	}

	return model.DB.Transaction(func(tx *gorm.DB) error {
		if file.IsDir {
			var count int64
			tx.Model(&model.File{}).Where("parent_id = ? AND user_id = ?", fileID, userID).Count(&count)
			if count > 0 {
				return errors.New("文件夹不为空")
			}
		} else {
			driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
			if err != nil {
				return err
			}
			if err := driver.Delete(file.StorageKey); err != nil {
				return fmt.Errorf("删除存储对象失败: %w", err)
			}
			if err := tx.Model(&model.User{}).Where("id = ?", userID).
				Update("storage_used", gorm.Expr("storage_used - ?", file.Size)).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&file).Error
	})
}

func (s *FileService) Rename(userID uint, fileID uint, newName string) error {
	result := model.DB.Model(&model.File{}).
		Where("id = ? AND user_id = ?", fileID, userID).
		Update("name", newName)
	if result.RowsAffected == 0 {
		return errors.New("文件不存在")
	}
	return result.Error
}

func (s *FileService) Move(userID uint, fileID uint, newParentID uint) error {
	if newParentID != 0 {
		var parent model.File
		if err := model.DB.Where("id = ? AND user_id = ? AND is_dir = ?", newParentID, userID, true).First(&parent).Error; err != nil {
			return errors.New("目标文件夹不存在")
		}
	}
	result := model.DB.Model(&model.File{}).
		Where("id = ? AND user_id = ?", fileID, userID).
		Update("parent_id", newParentID)
	if result.RowsAffected == 0 {
		return errors.New("文件不存在")
	}
	return result.Error
}
```

- [ ] **Step 2: 编写文件 Handler**

创建 `internal/handler/file.go`：

```go
package handler

import (
	"net/http"
	"strconv"

	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
)

type FileHandler struct {
	fileService *service.FileService
}

func NewFileHandler(fs *service.FileService) *FileHandler {
	return &FileHandler{fileService: fs}
}

func (h *FileHandler) List(c *gin.Context) {
	userID := c.GetUint("user_id")
	parentID, _ := strconv.ParseUint(c.Query("parent_id"), 10, 32)

	files, err := h.fileService.ListFiles(userID, uint(parentID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

type mkdirRequest struct {
	ParentID uint   `json:"parent_id"`
	Name     string `json:"name" binding:"required"`
}

func (h *FileHandler) Mkdir(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req mkdirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir, err := h.fileService.Mkdir(userID, req.ParentID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": dir})
}

type uploadRequest struct {
	FileName    string `json:"file_name" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	ParentID    uint   `json:"parent_id"`
}

func (h *FileHandler) Upload(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req uploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	url, key, err := h.fileService.GetUploadURL(userID, req.FileName, req.ContentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"upload_url": url, "storage_key": key})
}

type uploadCallbackRequest struct {
	FileName    string `json:"file_name" binding:"required"`
	StorageKey  string `json:"storage_key" binding:"required"`
	Size        int64  `json:"size" binding:"required"`
	MimeType    string `json:"mime_type"`
	ParentID    uint   `json:"parent_id"`
}

func (h *FileHandler) UploadCallback(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req uploadCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file, err := h.fileService.UploadCallback(userID, req.ParentID, req.FileName, req.StorageKey, req.Size, req.MimeType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": file})
}

func (h *FileHandler) Download(c *gin.Context) {
	userID := c.GetUint("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	url, err := h.fileService.GetDownloadURL(userID, uint(fileID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url})
}

func (h *FileHandler) Delete(c *gin.Context) {
	userID := c.GetUint("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	if err := h.fileService.Delete(userID, uint(fileID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

type renameRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *FileHandler) Rename(c *gin.Context) {
	userID := c.GetUint("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var req renameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.fileService.Rename(userID, uint(fileID), req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "重命名成功"})
}

type moveRequest struct {
	ParentID uint `json:"parent_id" binding:"required"`
}

func (h *FileHandler) Move(c *gin.Context) {
	userID := c.GetUint("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var req moveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.fileService.Move(userID, uint(fileID), req.ParentID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "移动成功"})
}
```

- [ ] **Step 3: 验证编译**

Run: `make build`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/service/file_service.go internal/handler/file.go
git commit -m "feat: 文件管理 Service 与 Handler"
```

---

### Task 7: 分享 Service 与 Handler

**Files:**
- Create: `internal/service/share_service.go`
- Create: `internal/handler/share.go`

**Interfaces:**
- Consumes: `model.Share`, `model.File`, `storage.StoragePolicyManager`
- Produces: `handler.ShareHandler`

- [ ] **Step 1: 编写分享 Service**

创建 `internal/service/share_service.go`：

```go
package service

import (
	"errors"
	"math/rand"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"gorm.io/gorm"
)

type ShareService struct {
	storageMgr *storage.StoragePolicyManager
}

func NewShareService(mgr *storage.StoragePolicyManager) *ShareService {
	return &ShareService{storageMgr: mgr}
}

func generateCode() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func (s *ShareService) Create(userID uint, fileID uint, password string, expireAt *time.Time) (*model.Share, error) {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("文件不存在")
		}
		return nil, err
	}

	share := &model.Share{
		UserID:   userID,
		FileID:   fileID,
		Code:     generateCode(),
		Password: password,
		ExpireAt: expireAt,
	}
	if err := model.DB.Create(share).Error; err != nil {
		return nil, err
	}
	return share, nil
}

func (s *ShareService) GetByCode(code string, password string) (*model.Share, *model.File, error) {
	var share model.Share
	if err := model.DB.Where("code = ?", code).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("分享不存在")
		}
		return nil, nil, err
	}

	if share.ExpireAt != nil && share.ExpireAt.Before(time.Now()) {
		return nil, nil, errors.New("分享已过期")
	}

	if share.Password != "" && share.Password != password {
		return nil, nil, errors.New("提取码错误")
	}

	var file model.File
	if err := model.DB.First(&file, share.FileID).Error; err != nil {
		return nil, nil, errors.New("文件不存在")
	}

	model.DB.Model(&share).Update("views", share.Views+1)
	return &share, &file, nil
}

func (s *ShareService) GetDownloadURL(code string, password string) (string, error) {
	share, file, err := s.GetByCode(code, password)
	if err != nil {
		return "", err
	}
	if file.IsDir {
		return "", errors.New("不能下载文件夹")
	}

	driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
	if err != nil {
		return "", err
	}
	url, err := driver.GenerateDownloadURL(file.StorageKey, 30*time.Minute)
	if err != nil {
		return "", err
	}

	model.DB.Model(share).Update("views", share.Views+1)
	return url, nil
}
```

- [ ] **Step 2: 编写分享 Handler**

创建 `internal/handler/share.go`：

```go
package handler

import (
	"net/http"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	shareService *service.ShareService
}

func NewShareHandler(ss *service.ShareService) *ShareHandler {
	return &ShareHandler{shareService: ss}
}

type createShareRequest struct {
	FileID   uint   `json:"file_id" binding:"required"`
	Password string `json:"password"`
	ExpireAt string `json:"expire_at"`
}

func (h *ShareHandler) Create(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req createShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var expireAt *time.Time
	if req.ExpireAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpireAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "过期时间格式错误"})
			return
		}
		expireAt = &t
	}

	share, err := h.shareService.Create(userID, req.FileID, req.Password, expireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"share": share})
}

func (h *ShareHandler) Get(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	share, file, err := h.shareService.GetByCode(code, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"share": share, "file": file})
}

func (h *ShareHandler) Download(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	url, err := h.shareService.GetDownloadURL(code, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url})
}
```

- [ ] **Step 3: 验证编译**

Run: `make build`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/service/share_service.go internal/handler/share.go
git commit -m "feat: 文件分享 Service 与 Handler"
```

---

### Task 8: 用户 Handler 与路由注册

**Files:**
- Create: `internal/handler/user.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: 所有 handler、middleware、service、storage
- Produces: 完整可运行的 HTTP 服务

- [ ] **Step 1: 编写用户 Handler**

创建 `internal/handler/user.go`：

```go
package handler

import (
	"net/http"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserHandler struct{}

func NewUserHandler() *UserHandler {
	return &UserHandler{}
}

func (h *UserHandler) Profile(c *gin.Context) {
	userID := c.GetUint("user_id")
	var user model.User
	if err := model.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user model.User
	if err := model.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "原密码错误"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	if err := model.DB.Model(&user).Update("password_hash", string(hash)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新密码失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}

// 避免未使用 import 的编译错误
var _ = gorm.ErrRecordNotFound
```

- [ ] **Step 2: 更新 main.go 完成路由注册**

替换 `cmd/server/main.go` 全部内容：

```go
package main

import (
	"fmt"
	"log"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/handler"
	"github.com/cloudreve-eo/cloudreve-eo/internal/middleware"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if cfg.JWT.Secret == "" {
		log.Fatal("JWT_SECRET 环境变量未设置")
	}

	if err := model.InitDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	storageMgr, err := storage.NewStoragePolicyManager(cfg)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	authService := service.NewAuthService(cfg)
	fileService := service.NewFileService(storageMgr)
	shareService := service.NewShareService(storageMgr)

	authHandler := handler.NewAuthHandler(authService, cfg.JWT.Secret)
	fileHandler := handler.NewFileHandler(fileService)
	shareHandler := handler.NewShareHandler(shareService)
	userHandler := handler.NewUserHandler()

	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	api := r.Group("/api")

	auth := api.Group("/auth")
	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)

	protected := api.Group("")
	protected.Use(middleware.JWTAuth(cfg.JWT.Secret))
	{
		files := protected.Group("/files")
		files.GET("", fileHandler.List)
		files.POST("/mkdir", fileHandler.Mkdir)
		files.POST("/upload", fileHandler.Upload)
		files.POST("/upload/callback", fileHandler.UploadCallback)
		files.GET("/:id/download", fileHandler.Download)
		files.DELETE("/:id", fileHandler.Delete)
		files.PUT("/:id/rename", fileHandler.Rename)
		files.PUT("/:id/move", fileHandler.Move)

		shares := protected.Group("/shares")
		shares.POST("", shareHandler.Create)

		user := protected.Group("/user")
		user.GET("/profile", userHandler.Profile)
		user.PUT("/password", userHandler.ChangePassword)
	}

	publicShares := api.Group("/shares")
	publicShares.GET("/:code", shareHandler.Get)
	publicShares.GET("/:code/download", shareHandler.Download)

	fmt.Printf("Cloudreve-EO 启动中，端口: %s\n", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
```

- [ ] **Step 3: 修复 user.go 中 gin 的 import**

`internal/handler/user.go` 需要添加 gin import：

```go
import (
	"net/http"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)
```

同时删除文件末尾的 `var _ = gorm.ErrRecordNotFound` 行。

- [ ] **Step 4: 验证编译**

Run: `make build`
Expected: 编译成功

- [ ] **Step 5: 启动测试**

Run: `JWT_SECRET=test-secret make run`
Expected: 输出 `Cloudreve-EO 启动中，端口: 8080`

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go internal/handler/user.go
git commit -m "feat: 用户 Handler、路由注册、服务启动"
```

---

### Task 9: 前端项目初始化

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/tsconfig.node.json`
- Create: `web/index.html`
- Create: `web/src/main.tsx`
- Create: `web/src/App.tsx`
- Create: `web/src/vite-env.d.ts`

- [ ] **Step 1: 用 Vite 创建 React + TypeScript 项目**

```bash
cd /home/huazong/clouddreve-eo
npm create vite@latest web -- --template react-ts
cd web
npm install
npm install antd axios react-router-dom @ant-design/icons
```

- [ ] **Step 2: 配置 vite 代理**

修改 `web/vite.config.ts`：

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

- [ ] **Step 3: 编写 App.tsx 骨架**

替换 `web/src/App.tsx`：

```tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<div>登录页面（待实现）</div>} />
        <Route path="/register" element={<div>注册页面（待实现）</div>} />
        <Route path="/" element={<div>文件管理（待实现）</div>} />
        <Route path="/share/:code" element={<div>分享查看（待实现）</div>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
```

- [ ] **Step 4: 验证前端启动**

Run: `cd web && npm run dev`
Expected: Vite 开发服务器启动，浏览器可访问

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat: 前端项目初始化"
```

---

### Task 10: 前端 API 层与认证页面

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/auth.ts`
- Create: `web/src/pages/Login.tsx`
- Create: `web/src/pages/Register.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 编写 Axios 客户端**

创建 `web/src/api/client.ts`：

```typescript
import axios from 'axios'

const client = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export default client
```

- [ ] **Step 2: 编写认证 API**

创建 `web/src/api/auth.ts`：

```typescript
import client from './client'

export interface LoginParams {
  username: string
  password: string
}

export interface RegisterParams {
  username: string
  password: string
}

export interface AuthResponse {
  token: string
  user: {
    id: number
    username: string
    storage_quota: number
    storage_used: number
  }
}

export const login = (params: LoginParams) =>
  client.post<AuthResponse>('/auth/login', params)

export const register = (params: RegisterParams) =>
  client.post<AuthResponse>('/auth/register', params)
```

- [ ] **Step 3: 编写登录页面**

创建 `web/src/pages/Login.tsx`：

```tsx
import { useState } from 'react'
import { Form, Input, Button, Card, message } from 'antd'
import { UserOutlined, LockOutlined } from '@ant-design/icons'
import { useNavigate, Link } from 'react-router-dom'
import { login } from '../api/auth'

export default function Login() {
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const res = await login(values)
      localStorage.setItem('token', res.data.token)
      message.success('登录成功')
      navigate('/')
    } catch (err: any) {
      message.error(err.response?.data?.error || '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="Cloudreve-EO 登录" style={{ width: 400 }}>
        <Form onFinish={onFinish}>
          <Form.Item name="username" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block>
              登录
            </Button>
          </Form.Item>
          <div style={{ textAlign: 'center' }}>
            没有账号？<Link to="/register">去注册</Link>
          </div>
        </Form>
      </Card>
    </div>
  )
}
```

- [ ] **Step 4: 编写注册页面**

创建 `web/src/pages/Register.tsx`：

```tsx
import { useState } from 'react'
import { Form, Input, Button, Card, message } from 'antd'
import { UserOutlined, LockOutlined } from '@ant-design/icons'
import { useNavigate, Link } from 'react-router-dom'
import { register } from '../api/auth'

export default function Register() {
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const res = await register(values)
      localStorage.setItem('token', res.data.token)
      message.success('注册成功')
      navigate('/')
    } catch (err: any) {
      message.error(err.response?.data?.error || '注册失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="Cloudreve-EO 注册" style={{ width: 400 }}>
        <Form onFinish={onFinish}>
          <Form.Item name="username" rules={[{ required: true, min: 3, message: '用户名至少3个字符' }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, min: 6, message: '密码至少6个字符' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block>
              注册
            </Button>
          </Form.Item>
          <div style={{ textAlign: 'center' }}>
            已有账号？<Link to="/login">去登录</Link>
          </div>
        </Form>
      </Card>
    </div>
  )
}
```

- [ ] **Step 5: 更新 App.tsx 路由**

```tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route path="/" element={<div>文件管理（待实现）</div>} />
        <Route path="/share/:code" element={<div>分享查看（待实现）</div>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
```

- [ ] **Step 6: 验证**

Run: `cd web && npm run build`
Expected: 构建成功

- [ ] **Step 7: Commit**

```bash
git add web/src/
git commit -m "feat: 前端 API 层与认证页面"
```

---

### Task 11: 前端文件管理页面

**Files:**
- Create: `web/src/api/files.ts`
- Create: `web/src/pages/Files.tsx`
- Create: `web/src/components/FileList.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 编写文件 API**

创建 `web/src/api/files.ts`：

```typescript
import client from './client'

export interface FileItem {
  id: number
  user_id: number
  parent_id: number
  name: string
  is_dir: boolean
  size: number
  mime_type: string
  storage_key: string
  storage_policy: string
  created_at: string
  updated_at: string
}

export const listFiles = (parentId: number = 0) =>
  client.get<{ files: FileItem[] }>('/files', { params: { parent_id: parentId } })

export const mkdir = (parentId: number, name: string) =>
  client.post('/files/mkdir', { parent_id: parentId, name })

export const getUploadURL = (fileName: string, contentType: string, parentId: number = 0) =>
  client.post<{ upload_url: string; storage_key: string }>('/files/upload', {
    file_name: fileName,
    content_type: contentType,
    parent_id: parentId,
  })

export const uploadCallback = (fileName: string, storageKey: string, size: number, mimeType: string, parentId: number = 0) =>
  client.post('/files/upload/callback', {
    file_name: fileName,
    storage_key: storageKey,
    size,
    mime_type: mimeType,
    parent_id: parentId,
  })

export const getDownloadURL = (fileId: number) =>
  client.get<{ download_url: string }>(`/files/${fileId}/download`)

export const deleteFile = (fileId: number) =>
  client.delete(`/files/${fileId}`)

export const renameFile = (fileId: number, name: string) =>
  client.put(`/files/${fileId}/rename`, { name })

export const moveFile = (fileId: number, parentId: number) =>
  client.put(`/files/${fileId}/move`, { parent_id: parentId })
```

- [ ] **Step 2: 编写文件列表组件**

创建 `web/src/components/FileList.tsx`：

```tsx
import { Table, Button, Dropdown, Modal, Input, message, Space } from 'antd'
import { FolderOutlined, FileOutlined, DownloadOutlined, DeleteOutlined, EditOutlined, MoreOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { FileItem } from '../api/files'
import { deleteFile, renameFile, getDownloadURL } from '../api/files'
import { useState } from 'react'

interface Props {
  files: FileItem[]
  onRefresh: () => void
  onOpenDir: (dirId: number) => void
}

export default function FileList({ files, onRefresh, onOpenDir }: Props) {
  const [renameModal, setRenameModal] = useState<{ visible: boolean; file?: FileItem }>({ visible: false })
  const [newName, setNewName] = useState('')

  const formatSize = (bytes: number) => {
    if (bytes === 0) return '-'
    const units = ['B', 'KB', 'MB', 'GB']
    let i = 0
    let size = bytes
    while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
    return `${size.toFixed(1)} ${units[i]}`
  }

  const handleDownload = async (file: FileItem) => {
    try {
      const res = await getDownloadURL(file.id)
      window.open(res.data.download_url, '_blank')
    } catch {
      message.error('获取下载链接失败')
    }
  }

  const handleDelete = (file: FileItem) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定删除 "${file.name}" 吗？`,
      onOk: async () => {
        try {
          await deleteFile(file.id)
          message.success('删除成功')
          onRefresh()
        } catch (err: any) {
          message.error(err.response?.data?.error || '删除失败')
        }
      },
    })
  }

  const handleRename = async () => {
    if (!renameModal.file || !newName) return
    try {
      await renameFile(renameModal.file.id, newName)
      message.success('重命名成功')
      setRenameModal({ visible: false })
      onRefresh()
    } catch (err: any) {
      message.error(err.response?.data?.error || '重命名失败')
    }
  }

  const columns: ColumnsType<FileItem> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record) => (
        <Space>
          {record.is_dir ? <FolderOutlined style={{ color: '#faad14' }} /> : <FileOutlined />}
          <a onClick={() => record.is_dir && onOpenDir(record.id)}>{name}</a>
        </Space>
      ),
    },
    { title: '大小', dataIndex: 'size', width: 120, render: formatSize },
    { title: '修改时间', dataIndex: 'updated_at', width: 180, render: (v: string) => new Date(v).toLocaleString() },
    {
      title: '操作', width: 120,
      render: (_, record) => (
        <Dropdown menu={{
          items: [
            ...(!record.is_dir ? [{ key: 'download', label: '下载', icon: <DownloadOutlined /> }] : []),
            { key: 'rename', label: '重命名', icon: <EditOutlined /> },
            { key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true },
          ],
          onClick: ({ key }) => {
            if (key === 'download') handleDownload(record)
            else if (key === 'rename') { setRenameModal({ visible: true, file: record }); setNewName(record.name) }
            else if (key === 'delete') handleDelete(record)
          },
        }}>
          <Button type="text" icon={<MoreOutlined />} />
        </Dropdown>
      ),
    },
  ]

  return (
    <>
      <Table columns={columns} dataSource={files} rowKey="id" pagination={false} />
      <Modal title="重命名" open={renameModal.visible} onOk={handleRename} onCancel={() => setRenameModal({ visible: false })}>
        <Input value={newName} onChange={(e) => setNewName(e.target.value)} />
      </Modal>
    </>
  )
}
```

- [ ] **Step 3: 编写文件管理页面**

创建 `web/src/pages/Files.tsx`：

```tsx
import { useState, useEffect, useCallback } from 'react'
import { Layout, Breadcrumb, Button, Upload, Modal, Input, message, Space } from 'antd'
import { UploadOutlined, FolderAddOutlined, LogoutOutlined } from '@ant-design/icons'
import FileList from '../components/FileList'
import { listFiles, mkdir, getUploadURL, uploadCallback, type FileItem } from '../api/files'
import { useNavigate } from 'react-router-dom'

const { Header, Content } = Layout

interface BreadcrumbItem { title: string; id: number }

export default function Files() {
  const [files, setFiles] = useState<FileItem[]>([])
  const [currentDir, setCurrentDir] = useState(0)
  const [breadcrumb, setBreadcrumb] = useState<BreadcrumbItem[]>([{ title: '根目录', id: 0 }])
  const [mkdirModal, setMkdirModal] = useState(false)
  const [dirName, setDirName] = useState('')
  const navigate = useNavigate()

  const loadFiles = useCallback(async () => {
    try {
      const res = await listFiles(currentDir)
      setFiles(res.data.files)
    } catch {
      message.error('加载文件列表失败')
    }
  }, [currentDir])

  useEffect(() => { loadFiles() }, [loadFiles])

  const handleOpenDir = async (dirId: number) => {
    setCurrentDir(dirId)
    if (dirId === 0) {
      setBreadcrumb([{ title: '根目录', id: 0 }])
    } else {
      setBreadcrumb(prev => [...prev, { title: files.find(f => f.id === dirId)?.name || '', id: dirId }])
    }
  }

  const handleMkdir = async () => {
    if (!dirName) return
    try {
      await mkdir(currentDir, dirName)
      message.success('创建成功')
      setMkdirModal(false)
      setDirName('')
      loadFiles()
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建失败')
    }
  }

  const handleUpload = async (file: File) => {
    try {
      const { data } = await getUploadURL(file.name, file.type, currentDir)
      await fetch(data.upload_url, { method: 'PUT', body: file, headers: { 'Content-Type': file.type } })
      await uploadCallback(file.name, data.storage_key, file.size, file.type, currentDir)
      message.success(`${file.name} 上传成功`)
      loadFiles()
    } catch {
      message.error(`${file.name} 上传失败`)
    }
    return false
  }

  const handleLogout = () => {
    localStorage.removeItem('token')
    navigate('/login')
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529' }}>
        <span style={{ color: '#fff', fontSize: 18 }}>Cloudreve-EO</span>
        <Button icon={<LogoutOutlined />} type="text" style={{ color: '#fff' }} onClick={handleLogout}>退出</Button>
      </Header>
      <Content style={{ padding: '24px', maxWidth: 1200, margin: '0 auto', width: '100%' }}>
        <Breadcrumb style={{ marginBottom: 16 }} items={breadcrumb.map(b => ({ title: b.title, key: b.id }))} />
        <Space style={{ marginBottom: 16 }}>
          <Upload beforeUpload={handleUpload} showUploadList={false}>
            <Button icon={<UploadOutlined />} type="primary">上传文件</Button>
          </Upload>
          <Button icon={<FolderAddOutlined />} onClick={() => setMkdirModal(true)}>新建文件夹</Button>
        </Space>
        <FileList files={files} onRefresh={loadFiles} onOpenDir={handleOpenDir} />
      </Content>
      <Modal title="新建文件夹" open={mkdirModal} onOk={handleMkdir} onCancel={() => setMkdirModal(false)}>
        <Input value={dirName} onChange={(e) => setDirName(e.target.value)} placeholder="文件夹名称" />
      </Modal>
    </Layout>
  )
}
```

- [ ] **Step 4: 更新 App.tsx**

```tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import Files from './pages/Files'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route path="/" element={<Files />} />
        <Route path="/share/:code" element={<div>分享查看（待实现）</div>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
```

- [ ] **Step 5: 验证构建**

Run: `cd web && npm run build`
Expected: 构建成功

- [ ] **Step 6: Commit**

```bash
git add web/src/
git commit -m "feat: 前端文件管理页面"
```

---

### Task 12: 前端分享功能

**Files:**
- Create: `web/src/api/shares.ts`
- Create: `web/src/pages/ShareView.tsx`
- Create: `web/src/components/ShareModal.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/FileList.tsx`（添加分享按钮）

- [ ] **Step 1: 编写分享 API**

创建 `web/src/api/shares.ts`：

```typescript
import client from './client'

export interface ShareInfo {
  id: number
  code: string
  expire_at: string | null
  views: number
  created_at: string
}

export const createShare = (fileId: number, password?: string, expireAt?: string) =>
  client.post('/shares', { file_id: fileId, password, expire_at: expireAt })

export const getShare = (code: string, password?: string) =>
  client.get(`/shares/${code}`, { params: { password } })

export const getShareDownload = (code: string, password?: string) =>
  client.get<{ download_url: string }>(`/shares/${code}/download`, { params: { password } })
```

- [ ] **Step 2: 编写分享弹窗组件**

创建 `web/src/components/ShareModal.tsx`：

```tsx
import { useState } from 'react'
import { Modal, Input, DatePicker, Button, message, Space } from 'antd'
import { createShare } from '../api/shares'

interface Props {
  open: boolean
  fileId: number | null
  onClose: () => void
}

export default function ShareModal({ open, fileId, onClose }: Props) {
  const [password, setPassword] = useState('')
  const [expireAt, setExpireAt] = useState<string | undefined>()
  const [shareLink, setShareLink] = useState('')

  const handleCreate = async () => {
    if (!fileId) return
    try {
      const res = await createShare(fileId, password || undefined, expireAt)
      const code = res.data.share.code
      const link = `${window.location.origin}/share/${code}`
      setShareLink(link)
      message.success('分享链接已生成')
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建分享失败')
    }
  }

  const handleCopy = () => {
    navigator.clipboard.writeText(shareLink)
    message.success('已复制到剪贴板')
  }

  return (
    <Modal title="创建分享" open={open} onCancel={() => { onClose(); setShareLink(''); setPassword('') }} footer={null}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Input.Password placeholder="提取码（可选）" value={password} onChange={(e) => setPassword(e.target.value)} />
        <DatePicker showTime placeholder="过期时间（可选）" onChange={(_, dateStr) => setExpireAt(dateStr as string)} style={{ width: '100%' }} />
        <Button type="primary" onClick={handleCreate} block>生成链接</Button>
        {shareLink && (
          <Space.Compact style={{ width: '100%' }}>
            <Input value={shareLink} readOnly />
            <Button type="primary" onClick={handleCopy}>复制</Button>
          </Space.Compact>
        )}
      </Space>
    </Modal>
  )
}
```

- [ ] **Step 3: 编写分享查看页面**

创建 `web/src/pages/ShareView.tsx`：

```tsx
import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Card, Input, Button, message, Space, Typography } from 'antd'
import { DownloadOutlined } from '@ant-design/icons'
import { getShare, getShareDownload } from '../api/shares'

const { Title, Text } = Typography

export default function ShareView() {
  const { code } = useParams<{ code: string }>()
  const [password, setPassword] = useState('')
  const [file, setFile] = useState<any>(null)
  const [error, setError] = useState('')
  const [needPassword, setNeedPassword] = useState(false)

  useEffect(() => {
    if (code) loadShare('')
  }, [code])

  const loadShare = async (pwd: string) => {
    if (!code) return
    try {
      const res = await getShare(code, pwd)
      setFile(res.data.file)
      setError('')
    } catch (err: any) {
      const msg = err.response?.data?.error || '加载失败'
      if (msg.includes('提取码')) {
        setNeedPassword(true)
        setError('')
      } else {
        setError(msg)
      }
    }
  }

  const handleDownload = async () => {
    if (!code) return
    try {
      const res = await getShareDownload(code, password || undefined)
      window.open(res.data.download_url, '_blank')
    } catch {
      message.error('获取下载链接失败')
    }
  }

  if (error) return <div style={{ textAlign: 'center', marginTop: 100 }}><Text type="danger">{error}</Text></div>

  if (needPassword && !file) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Card title="输入提取码" style={{ width: 360 }}>
          <Space direction="vertical" style={{ width: '100%' }}>
            <Input.Password value={password} onChange={(e) => setPassword(e.target.value)} placeholder="提取码" />
            <Button type="primary" block onClick={() => loadShare(password)}>确认</Button>
          </Space>
        </Card>
      </div>
    )
  }

  if (!file) return <div style={{ textAlign: 'center', marginTop: 100 }}>加载中...</div>

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="分享文件" style={{ width: 400 }}>
        <Title level={4}>{file.name}</Title>
        <Text type="secondary">大小: {(file.size / 1024 / 1024).toFixed(2)} MB</Text>
        <div style={{ marginTop: 24 }}>
          <Button type="primary" icon={<DownloadOutlined />} block onClick={handleDownload}>下载文件</Button>
        </div>
      </Card>
    </div>
  )
}
```

- [ ] **Step 4: 更新 FileList 添加分享按钮**

在 `web/src/components/FileList.tsx` 的 Dropdown items 中，在 download 项后添加：

```tsx
{ key: 'share', label: '分享', icon: <ShareAltOutlined /> },
```

并在 `onClick` 处理中添加：

```tsx
else if (key === 'share') { setShareFile(record) }
```

同时添加 `ShareModal` 组件引用和状态。

- [ ] **Step 5: 更新 App.tsx 路由**

```tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import Files from './pages/Files'
import ShareView from './pages/ShareView'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route path="/" element={<Files />} />
        <Route path="/share/:code" element={<ShareView />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
```

- [ ] **Step 6: 验证构建**

Run: `cd web && npm run build`
Expected: 构建成功

- [ ] **Step 7: Commit**

```bash
git add web/src/
git commit -m "feat: 前端分享功能"
```

---

### Task 13: 集成测试与最终联调

**Files:**
- Modify: `cmd/server/main.go`（如需要）
- Modify: `Makefile`

- [ ] **Step 1: 启动后端服务**

Run: `JWT_SECRET=test-secret DEFAULT_STORAGE=s3 S3_ENDPOINT=http://localhost:9000 S3_REGION=us-east-1 S3_BUCKET=test S3_ACCESS_KEY=minioadmin S3_SECRET_KEY=minioadmin make run`
Expected: 服务启动成功

- [ ] **Step 2: 测试注册接口**

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser","password":"test123456"}'
```

Expected: 返回 `{"token":"...","user":{...}}`

- [ ] **Step 3: 测试登录接口**

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser","password":"test123456"}'
```

Expected: 返回 token

- [ ] **Step 4: 测试文件列表**

```bash
TOKEN="<上一步获取的token>"
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/files?parent_id=0
```

Expected: 返回 `{"files":[]}`

- [ ] **Step 5: 测试创建文件夹**

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  http://localhost:8080/api/files/mkdir -d '{"parent_id":0,"name":"测试文件夹"}'
```

Expected: 返回创建的文件夹信息

- [ ] **Step 6: 前端联调**

Run: `cd web && npm run dev`
Expected: 前端启动，可以注册、登录、浏览文件

- [ ] **Step 7: 最终 Commit**

```bash
git add -A
git commit -m "feat: Cloudreve-EO 基础版完成"
```

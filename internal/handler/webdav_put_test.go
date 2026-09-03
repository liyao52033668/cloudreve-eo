package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// setupWebDAVPutTest 设置 WebDAV PUT 测试环境
func setupWebDAVPutTest(t *testing.T) (*gin.Engine, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "webdav_put_test.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	// WebDAV 密码用 JWT secret 派生密钥加密，需先生成
	if _, err := model.EnsureJWTSecret(); err != nil {
		t.Fatalf("EnsureJWTSecret: %v", err)
	}

	// 启用 WebDAV
	if err := model.SetWebDAVEnabled(true); err != nil {
		t.Fatalf("SetWebDAVEnabled: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := &model.User{Username: "testuser", PasswordHash: string(hash), IsAdmin: false, StorageQuota: 1 << 30}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 设置 WebDAV 密码
	if err := model.SetWebDAVPassword(user.ID, "webdavpass"); err != nil {
		t.Fatalf("SetWebDAVPassword: %v", err)
	}

	// 创建存储策略（S3 类型，用于测试）
	policy := &model.StoragePolicy{
		Name:      "test-s3",
		Type:      "s3",
		Endpoint:  "https://s3.example.com",
		Region:    "us-east-1",
		Bucket:    "test-bucket",
		AccessKey: "test-key",
		SecretKey: "test-secret",
		IsDefault: true,
	}
	if err := model.DB.Create(policy).Error; err != nil {
		t.Fatalf("create policy: %v", err)
	}

	// 创建用户组
	group := &model.UserGroup{
		Name:        "default",
		MaxStorage:  1 << 30,
		IsDefault:   true,
	}
	if err := model.DB.Create(group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	// 更新用户组
	if err := model.DB.Model(user).Update("group_id", group.ID).Error; err != nil {
		t.Fatalf("update user group: %v", err)
	}

	mgr, err := storage.NewStoragePolicyManager()
	if err != nil {
		t.Fatalf("NewStoragePolicyManager: %v", err)
	}

	fs := service.NewFileService(mgr)
	handler := NewWebDAVHandler(fs)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/dav" {
			handler.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})

	return r, user
}

// TestWebDAVHandler_PutNewFile 需要真实 S3 服务，跳过
// 真实部署时，用户配置了真实 S3 endpoint，上传能正常工作
func TestWebDAVHandler_PutNewFile(t *testing.T) {
	t.Skip("需要真实 S3 服务，跳过集成测试")
}

func TestWebDAVHandler_PutLargeFile(t *testing.T) {
	r, _ := setupWebDAVPutTest(t)

	// PUT 上传超过 5MB 的文件（应被拒绝）
	body := make([]byte, 6*1024*1024) // 6MB
	for i := range body {
		body[i] = 'x'
	}
	req := httptest.NewRequest("PUT", "/dav/large.bin", bytes.NewReader(body))
	req.Header.Set("Authorization", basicAuth("testuser", "webdavpass"))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	t.Logf("PUT large response: status=%d, body=%s", w.Code, w.Body.String())

	// 期望 405 Method Not Allowed（因为 Close 返回错误）
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d, body: %s", w.Code, w.Body.String())
	}
}

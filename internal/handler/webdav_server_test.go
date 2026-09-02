package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func setupWebDAVTest(t *testing.T) (*gin.Engine, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "webdav_test.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
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
		if strings.HasPrefix(c.Request.URL.Path, "/dav") {
			handler.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})

	return r, user
}

func basicAuth(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestWebDAVHandler_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "webdav_disabled.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	// 默认关闭 WebDAV
	mgr, _ := storage.NewStoragePolicyManager()
	fs := service.NewFileService(mgr)
	handler := NewWebDAVHandler(fs)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/dav") {
			handler.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", basicAuth("user", "pass"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestWebDAVHandler_NoAuth(t *testing.T) {
	r, _ := setupWebDAVTest(t)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebDAVHandler_WrongPassword(t *testing.T) {
	r, _ := setupWebDAVTest(t)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", basicAuth("testuser", "wrongpass"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebDAVHandler_PropfindRoot(t *testing.T) {
	r, _ := setupWebDAVTest(t)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", basicAuth("testuser", "webdavpass"))
	req.Header.Set("Depth", "0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 根目录 PROPFIND 应返回 207 Multi-Status
	if w.Code != http.StatusMultiStatus {
		t.Errorf("expected 207, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestWebDAVHandler_Mkcol(t *testing.T) {
	r, _ := setupWebDAVTest(t)

	// 创建目录
	req := httptest.NewRequest("MKCOL", "/dav/testdir", nil)
	req.Header.Set("Authorization", basicAuth("testuser", "webdavpass"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d, body: %s", w.Code, w.Body.String())
	}

	// 验证目录已创建
	var count int64
	if err := model.DB.Model(&model.File{}).Where("user_id = ? AND name = ? AND is_dir = ?", 1, "testdir", true).Count(&count).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 dir, got %d", count)
	}
}

func TestWebDAVHandler_Delete(t *testing.T) {
	r, user := setupWebDAVTest(t)

	// 先创建目录
	req := httptest.NewRequest("MKCOL", "/dav/deldir", nil)
	req.Header.Set("Authorization", basicAuth("testuser", "webdavpass"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("mkcol: %d", w.Code)
	}

	// 删除目录
	req = httptest.NewRequest("DELETE", "/dav/deldir", nil)
	req.Header.Set("Authorization", basicAuth("testuser", "webdavpass"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("expected 204 or 200, got %d, body: %s", w.Code, w.Body.String())
	}

	// 验证目录已删除
	var count int64
	if err := model.DB.Model(&model.File{}).Where("user_id = ? AND name = ? AND is_dir = ?", user.ID, "deldir", true).Count(&count).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 dirs, got %d", count)
	}
}

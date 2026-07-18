package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/middleware"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func setupSettingHandler(t *testing.T) (*gin.Engine, *service.JWTSecretStore, *model.User, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "handler_setting.db"),
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	secret, err := model.EnsureJWTSecret()
	if err != nil {
		t.Fatalf("EnsureJWTSecret: %v", err)
	}
	store := service.NewJWTSecretStore(secret)
	h := NewSettingHandler(store)

	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	admin := &model.User{Username: "admin", PasswordHash: string(hash), IsAdmin: true, StorageQuota: 1}
	user := &model.User{Username: "user", PasswordHash: string(hash), IsAdmin: false, StorageQuota: 1}
	if err := model.DB.Create(admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	r := gin.New()
	api := r.Group("/api")
	api.GET("/site", h.GetPublicSite)
	protected := api.Group("")
	protected.Use(middleware.JWTAuth(store))
	adminG := protected.Group("")
	adminG.Use(middleware.RequireAdmin())
	{
		adminG.GET("/settings/security", h.GetSecurity)
		adminG.POST("/settings/security/rotate-jwt", h.RotateJWTSecret)
		adminG.PUT("/settings/register", h.UpdateRegister)
	}
	return r, store, admin, user
}

func authHeader(t *testing.T, userID uint, secret string) string {
	t.Helper()
	tok, err := middleware.GenerateToken(userID, secret)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	return "Bearer " + tok
}

func TestSettingHandler_GetSecurity_AdminOnly(t *testing.T) {
	r, store, admin, user := setupSettingHandler(t)

	// 普通用户 403
	req := httptest.NewRequest(http.MethodGet, "/api/settings/security", nil)
	req.Header.Set("Authorization", authHeader(t, user.ID, store.Get()))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}

	// 管理员 200
	req = httptest.NewRequest(http.MethodGet, "/api/settings/security", nil)
	req.Header.Set("Authorization", authHeader(t, admin.ID, store.Get()))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		JWTSecret     string `json:"jwt_secret"`
		AllowRegister bool   `json:"allow_register"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.JWTSecret != store.Get() {
		t.Errorf("jwt_secret = %q, want %q", body.JWTSecret, store.Get())
	}
	if !body.AllowRegister {
		t.Error("allow_register should default to true")
	}
}

func TestSettingHandler_RotateJWTSecret(t *testing.T) {
	r, store, admin, _ := setupSettingHandler(t)
	old := store.Get()

	req := httptest.NewRequest(http.MethodPost, "/api/settings/security/rotate-jwt", nil)
	req.Header.Set("Authorization", authHeader(t, admin.ID, old))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body["jwt_secret"] == "" || body["jwt_secret"] == old {
		t.Errorf("expected new secret; old=%q new=%q", old, body["jwt_secret"])
	}
	if store.Get() != body["jwt_secret"] {
		t.Errorf("store not updated: store=%q body=%q", store.Get(), body["jwt_secret"])
	}

	// 旧 token 应失效
	req = httptest.NewRequest(http.MethodGet, "/api/settings/security", nil)
	req.Header.Set("Authorization", authHeader(t, admin.ID, old))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("old token status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestSettingHandler_UpdateRegister(t *testing.T) {
	r, store, admin, user := setupSettingHandler(t)

	// 普通用户 403
	body := bytes.NewBufferString(`{"allow_register":false}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/register", body)
	req.Header.Set("Authorization", authHeader(t, user.ID, store.Get()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}

	// 管理员关闭注册
	body = bytes.NewBufferString(`{"allow_register":false}`)
	req = httptest.NewRequest(http.MethodPut, "/api/settings/register", body)
	req.Header.Set("Authorization", authHeader(t, admin.ID, store.Get()))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	// 公开接口反映关闭状态
	req = httptest.NewRequest(http.MethodGet, "/api/site", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("site status = %d, want %d", w.Code, http.StatusOK)
	}
	var site map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil {
		t.Fatalf("json: %v", err)
	}
	if site["allow_register"] {
		t.Error("allow_register should be false")
	}
}


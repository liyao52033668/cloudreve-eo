package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func setupUserHandler(t *testing.T) (*UserHandler, *gin.Engine, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "handler_user.db"),
		},
		Storage: config.StorageConfig{
			DefaultQuota: 1073741824,
		},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := &model.User{
		Username:     "profileuser",
		PasswordHash: string(hash),
		StorageQuota: cfg.Storage.DefaultQuota,
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	h := NewUserHandler()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Next()
	})
	r.GET("/api/user/profile", h.Profile)
	r.PUT("/api/user/password", h.ChangePassword)
	return h, r, user
}

func TestUserHandler_Profile_Returns200(t *testing.T) {
	_, r, user := setupUserHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/user/profile", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	u, ok := resp["user"].(map[string]any)
	if !ok {
		t.Fatalf("user = %T, want object", resp["user"])
	}
	if u["username"] != user.Username {
		t.Errorf("username = %v, want %s", u["username"], user.Username)
	}
	if _, hasHash := u["password_hash"]; hasHash {
		t.Error("password_hash should not be exposed in JSON")
	}
}

func TestUserHandler_Profile_UserNotFound_Returns404(t *testing.T) {
	_, r, user := setupUserHandler(t)

	// Override middleware user_id to a non-existent id
	r = gin.New()
	h := NewUserHandler()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID+9999)
		c.Next()
	})
	r.GET("/api/user/profile", h.Profile)

	req := httptest.NewRequest(http.MethodGet, "/api/user/profile", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestUserHandler_ChangePassword_Success_Returns200(t *testing.T) {
	_, r, created := setupUserHandler(t)

	body := map[string]string{
		"old_password": "oldpassword",
		"new_password": "newpassword",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if msg, _ := resp["message"].(string); msg != "密码修改成功" {
		t.Errorf("message = %v, want 密码修改成功", resp["message"])
	}

	// Verify new password works against DB
	var user model.User
	if err := model.DB.First(&user, created.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("newpassword")); err != nil {
		t.Error("new password hash does not match newpassword")
	}
}

func TestUserHandler_ChangePassword_WrongOldPassword_Returns400(t *testing.T) {
	_, r, _ := setupUserHandler(t)

	body := map[string]string{
		"old_password": "wrongpassword",
		"new_password": "newpassword",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestUserHandler_ChangePassword_ShortNewPassword_Returns400(t *testing.T) {
	_, r, _ := setupUserHandler(t)

	body := map[string]string{
		"old_password": "oldpassword",
		"new_password": "12345",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

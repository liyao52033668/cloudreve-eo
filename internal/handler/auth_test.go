package handler

import (
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/gin-gonic/gin"
)

func setupAuthHandler(t *testing.T) (*AuthHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	model.DB = nil
	t.Cleanup(func() { model.DB = nil })

	cfg := &config.Config{
		DB: config.DBConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "handler_auth.db"),
		},
		Storage: config.StorageConfig{
			DefaultQuota: 1073741824,
		},
		JWT: config.JWTConfig{Secret: "handler-test-secret"},
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	svc := service.NewAuthService(cfg)
	h := NewAuthHandler(svc, service.NewJWTSecretStore(cfg.JWT.Secret))

	r := gin.New()
	r.POST("/api/auth/register", h.Register)
	r.POST("/api/auth/login", h.Login)
	return h, r
}

func TestAuthHandler_Register_Returns201AndToken(t *testing.T) {
	_, r := setupAuthHandler(t)

	body := map[string]string{"username": "alice", "password": "password123"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Error("expected non-empty token")
	}
	user, ok := resp["user"].(map[string]any)
	if !ok {
		t.Fatalf("user = %T, want object", resp["user"])
	}
	if user["username"] != "alice" {
		t.Errorf("username = %v, want alice", user["username"])
	}
	if _, hasHash := user["password_hash"]; hasHash {
		t.Error("password_hash should not be exposed in JSON")
	}
}

func TestAuthHandler_Login_Returns200AndToken(t *testing.T) {
	_, r := setupAuthHandler(t)

	// Register first
	regBody := map[string]string{"username": "bob", "password": "password123"}
	b, _ := json.Marshal(regBody)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register status = %d; body=%s", w.Code, w.Body.String())
	}

	// Login
	loginBody := map[string]string{"username": "bob", "password": "password123"}
	b, _ = json.Marshal(loginBody)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if token, _ := resp["token"].(string); token == "" {
		t.Error("expected non-empty token")
	}
}

func TestAuthHandler_BadRequest_Returns400(t *testing.T) {
	_, r := setupAuthHandler(t)

	// Missing fields
	b := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("register empty: status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	// Username too short
	b = []byte(`{"username":"ab","password":"password123"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("register short username: status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	// Login missing password
	b = []byte(`{"username":"alice"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("login missing password: status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

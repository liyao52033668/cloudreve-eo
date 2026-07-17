package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateToken_ParsesUserID(t *testing.T) {
	const secret = "test-secret"
	const userID uint = 42

	tokenStr, err := GenerateToken(userID, secret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if tokenStr == "" {
		t.Fatal("GenerateToken() returned empty token")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		t.Fatalf("ParseWithClaims() error = %v", err)
	}
	if !token.Valid {
		t.Fatal("token is not valid")
	}
	if claims.UserID != userID {
		t.Errorf("UserID = %d, want %d", claims.UserID, userID)
	}
	if claims.ExpiresAt == nil {
		t.Error("ExpiresAt should be set")
	}
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret"

	r := gin.New()
	r.GET("/protected", JWTAuth(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Missing Authorization header
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing auth: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Bad format
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Token abc")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("bad format: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Invalid token value
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_ValidTokenSetsUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret"
	const userID uint = 7

	tokenStr, err := GenerateToken(userID, secret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	var gotUserID any
	r := gin.New()
	r.GET("/protected", JWTAuth(secret), func(c *gin.Context) {
		gotUserID, _ = c.Get("user_id")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	id, ok := gotUserID.(uint)
	if !ok {
		t.Fatalf("user_id type = %T, want uint", gotUserID)
	}
	if id != userID {
		t.Errorf("user_id = %d, want %d", id, userID)
	}
}

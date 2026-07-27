package auth_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/auth"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := auth.HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == "secret123" {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}
	if !auth.CheckPassword(hash, "secret123") {
		t.Fatal("CheckPassword should accept correct password")
	}
	if auth.CheckPassword(hash, "wrong") {
		t.Fatal("CheckPassword should reject wrong password")
	}
}

func TestIssueAndParseJWT(t *testing.T) {
	secret := "test-jwt-secret"
	token, err := auth.IssueToken(secret, 42)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	userID, err := auth.ParseToken(secret, token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if userID != 42 {
		t.Fatalf("userID: got %d want 42", userID)
	}

	if _, err := auth.ParseToken("other-secret", token); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func setupSeededDB(t *testing.T) (*gorm.DB, uint) {
	t.Helper()
	gormDB, err := db.ConnectSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	cfg := &config.Config{
		AdminUsername: "admin",
		AdminPassword: "admin123",
		InitialCash:   100000,
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	var user models.User
	if err := gormDB.Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	return gormDB, user.ID
}

func TestLoginSuccessAndFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, userID := setupSeededDB(t)
	secret := "test-jwt-secret"
	router := httpserver.NewRouter(gormDB, secret)

	t.Run("success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "admin",
			"password": "admin123",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		token := resp["token"]
		if token == "" {
			t.Fatal("expected token in response")
		}
		parsedID, err := auth.ParseToken(secret, token)
		if err != nil {
			t.Fatalf("ParseToken: %v", err)
		}
		if parsedID != userID {
			t.Fatalf("token user id: got %d want %d", parsedID, userID)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "admin",
			"password": "bad",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d want 401", w.Code)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "nobody",
			"password": "admin123",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d want 401", w.Code)
		}
	})
}

func TestMeRequiresBearerAndReturnsUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, userID := setupSeededDB(t)
	secret := "test-jwt-secret"
	router := httpserver.NewRouter(gormDB, secret)

	t.Run("unauthorized without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d want 401", w.Code)
		}
	})

	t.Run("success with bearer", func(t *testing.T) {
		token, err := auth.IssueToken(secret, userID)
		if err != nil {
			t.Fatalf("IssueToken: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if resp["username"] != "admin" {
			t.Fatalf("username: got %v want admin", resp["username"])
		}
		idVal, ok := resp["id"].(float64)
		if !ok || uint(idVal) != userID {
			t.Fatalf("id: got %v want %d", resp["id"], userID)
		}
	})
}

func TestMiddlewareAuthSetsUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "test-jwt-secret"
	token, err := auth.IssueToken(secret, 7)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	r := gin.New()
	r.GET("/protected", auth.MiddlewareAuth(secret), func(c *gin.Context) {
		uid, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing user_id"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": uid})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if uint(resp["user_id"].(float64)) != 7 {
		t.Fatalf("user_id: got %v want 7", resp["user_id"])
	}
}

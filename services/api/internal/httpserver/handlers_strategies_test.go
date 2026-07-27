package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

type countingReloader struct {
	calls int
}

func (r *countingReloader) Reload(context.Context) error {
	r.calls++
	return nil
}

func validStrategyBody(name string) map[string]any {
	return map[string]any{
		"name":                   name,
		"description":            "custom strategy",
		"pre_open_minutes":       10,
		"intraday_every_minutes": 60,
		"intraday_start_et":      "10:00",
		"intraday_end_et":        "15:00",
		"execution_mode":         "auto_reject_breaches",
	}
}

func TestStrategiesListIncludesSeedDefault(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/strategies", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
	}

	var resp []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v body=%s", err, w.Body.String())
	}
	if len(resp) < 1 {
		t.Fatalf("expected at least 1 strategy, got %d", len(resp))
	}

	var found bool
	for _, item := range resp {
		if item["name"] == "整体策略1" {
			found = true
			if item["is_system_default"] != true {
				t.Fatalf("整体策略1 should be system default: %+v", item)
			}
			if item["is_active"] != true {
				t.Fatalf("整体策略1 should be active: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("seed strategy 整体策略1 not found in %v", resp)
	}
}

func TestStrategiesCreateActivateDelete(t *testing.T) {
	reloader := &countingReloader{}
	router, gormDB, secret, _, _ := setupAPIWithScheduler(t, reloader)
	token := bearerToken(t, secret, gormDB)

	var defaultStrategy models.Strategy
	if err := gormDB.Where("name = ?", "整体策略1").First(&defaultStrategy).Error; err != nil {
		t.Fatalf("find default: %v", err)
	}

	t.Run("create", func(t *testing.T) {
		body, _ := json.Marshal(validStrategyBody("Custom Strategy"))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/strategies", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("status: got %d want 201 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if resp["name"] != "Custom Strategy" {
			t.Fatalf("name: got %v", resp["name"])
		}
		if resp["is_active"] != false {
			t.Fatalf("created strategy should not be active: %+v", resp)
		}
		if resp["is_system_default"] != false {
			t.Fatalf("created strategy should not be system default: %+v", resp)
		}
	})

	var created models.Strategy
	if err := gormDB.Where("name = ?", "Custom Strategy").First(&created).Error; err != nil {
		t.Fatalf("find created: %v", err)
	}

	t.Run("activate switches active strategy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/strategies/%d/activate", created.ID), nil)
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
		if resp["is_active"] != true {
			t.Fatalf("activated strategy should be active: %+v", resp)
		}
		if reloader.calls != 1 {
			t.Fatalf("Reload calls: got %d want 1", reloader.calls)
		}

		var defaultRow models.Strategy
		if err := gormDB.First(&defaultRow, defaultStrategy.ID).Error; err != nil {
			t.Fatalf("reload default: %v", err)
		}
		if defaultRow.IsActive {
			t.Fatal("default strategy should no longer be active")
		}
	})

	t.Run("delete system default forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/strategies/%d", defaultStrategy.ID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status: got %d want 403 body=%s", w.Code, w.Body.String())
		}
	})
}

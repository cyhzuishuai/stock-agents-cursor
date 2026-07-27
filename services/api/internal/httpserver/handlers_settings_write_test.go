package httpserver_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

func TestWatchlistCRUD(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	// MSFT is seeded; remove so POST can create it fresh.
	if err := gormDB.Where("symbol = ?", "MSFT").Delete(&models.WatchlistSymbol{}).Error; err != nil {
		t.Fatalf("clear MSFT: %v", err)
	}

	// POST MSFT
	body := bytes.NewBufferString(`{"symbol":"msft","can_hold":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/watchlist", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status: got %d body=%s", w.Code, w.Body.String())
	}

	// duplicate → 409
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/settings/watchlist", bytes.NewBufferString(`{"symbol":"MSFT"}`))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate: got %d", w2.Code)
	}

	// PATCH can_hold false
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/watchlist/MSFT", bytes.NewBufferString(`{"can_hold":false}`))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("PATCH: got %d body=%s", w3.Code, w3.Body.String())
	}

	// DELETE
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/watchlist/MSFT", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("DELETE: got %d", w4.Code)
	}

	// DELETE missing → 404
	w5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/watchlist/MSFT", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w5, req5)
	if w5.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing: got %d", w5.Code)
	}
}

func TestRiskPatch(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/risk/max_order_notional",
		bytes.NewBufferString(`{"value":12345}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH existing: got %d body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/risk/does_not_exist",
		bytes.NewBufferString(`{"value":1}`))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("PATCH missing: got %d", w2.Code)
	}

	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/risk/max_order_notional",
		bytes.NewBufferString(`{"value":"nope"}`))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("PATCH bad value: got %d", w3.Code)
	}
}

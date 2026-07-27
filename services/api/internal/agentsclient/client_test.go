package agentsclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
)

func TestCallRetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &agentsclient.Client{
		MaxRetries: 2,
	}

	raw, err := client.Call(context.Background(), server.URL, map[string]string{"ping": "pong"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}

	var out map[string]bool
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out["ok"] {
		t.Fatalf("expected ok=true, got %v", out)
	}
}

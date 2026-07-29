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

func TestClientResumePostsToResumePath(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		w.Write([]byte(`{"result":{},"trace":{}}`))
	}))
	defer srv.Close()
	c := &agentsclient.Client{HTTP: srv.Client()}
	_, err := c.Resume(context.Background(), srv.URL, map[string]any{
		"thread_id":      "1:analyst",
		"human_response": map[string]any{"text": "ok"},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if sawPath != "/v1/resume" {
		t.Fatalf("path %s", sawPath)
	}
}

func TestResumeDoesNotRetryOn500(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := &agentsclient.Client{MaxRetries: 2}

	_, err := client.Resume(context.Background(), server.URL, map[string]any{
		"thread_id": "1:analyst",
	}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts.Load())
	}
}

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

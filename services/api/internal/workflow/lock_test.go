package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/redis/go-redis/v9"
)

func TestAcquireWorkflowLockDoubleAcquireFails(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	ttl := time.Minute

	unlock, err := workflow.AcquireWorkflowLock(ctx, rdb, ttl)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer unlock()

	_, err = workflow.AcquireWorkflowLock(ctx, rdb, ttl)
	if !errors.Is(err, workflow.ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got %v", err)
	}
}

func TestAcquireWorkflowLockUsesGlobalBusyKey(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	unlock, err := workflow.AcquireWorkflowLock(context.Background(), rdb, time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer unlock()

	if !mr.Exists("workflow:run:lock:busy") {
		t.Fatal("expected global busy lock key workflow:run:lock:busy")
	}
}

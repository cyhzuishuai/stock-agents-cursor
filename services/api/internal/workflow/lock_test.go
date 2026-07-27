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

func TestAcquireEODLockDoubleAcquireFails(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	tradeDate := "2026-07-23"
	ttl := time.Minute

	unlock, err := workflow.AcquireEODLock(ctx, rdb, tradeDate, ttl)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer unlock()

	_, err = workflow.AcquireEODLock(ctx, rdb, tradeDate, ttl)
	if !errors.Is(err, workflow.ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got %v", err)
	}
}

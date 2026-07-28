package scheduler

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/workflow"
)

type lockHeldRunner struct{}

func (lockHeldRunner) RunWorkflow(context.Context, workflow.RunParams) (uint, error) {
	return 0, workflow.ErrLockHeld
}

func TestRunJobSkipsErrLockHeld(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	s, err := New(Options{
		Runner:   lockHeldRunner{},
		Location: loc,
		Now:      func() time.Time { return time.Date(2026, 7, 27, 10, 0, 0, 0, loc) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	s.runJob(context.Background(), 1, workflow.TriggerIntraday, workflow.ExecutionModeAutoReject)
	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "lock held") {
		t.Fatalf("expected lock-held skip log, got %q", buf.String())
	}
}

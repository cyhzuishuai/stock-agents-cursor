package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/scheduler"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
)

func mustNY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func TestMatchesScheduleWeekdayAt1630(t *testing.T) {
	loc := mustNY(t)
	expr := scheduler.DefaultCronExpr

	monday := time.Date(2026, 7, 27, 16, 30, 0, 0, loc)
	ok, err := scheduler.MatchesSchedule(monday, expr, loc)
	if err != nil {
		t.Fatalf("MatchesSchedule: %v", err)
	}
	if !ok {
		t.Fatal("expected Monday 16:30 ET to match schedule")
	}
}

func TestMatchesScheduleSkipsWeekend(t *testing.T) {
	loc := mustNY(t)
	expr := scheduler.DefaultCronExpr

	saturday := time.Date(2026, 7, 25, 16, 30, 0, 0, loc)
	ok, err := scheduler.MatchesSchedule(saturday, expr, loc)
	if err != nil {
		t.Fatalf("MatchesSchedule: %v", err)
	}
	if ok {
		t.Fatal("expected Saturday 16:30 ET not to match weekday schedule")
	}
}

func TestMatchesScheduleSkipsOffMinute(t *testing.T) {
	loc := mustNY(t)
	expr := scheduler.DefaultCronExpr

	monday := time.Date(2026, 7, 27, 16, 29, 0, 0, loc)
	ok, err := scheduler.MatchesSchedule(monday, expr, loc)
	if err != nil {
		t.Fatalf("MatchesSchedule: %v", err)
	}
	if ok {
		t.Fatal("expected 16:29 not to match 16:30 schedule")
	}
}

func TestTradeDateUsesNewYork(t *testing.T) {
	loc := mustNY(t)
	// 2026-07-28 02:00 UTC is still 2026-07-27 in New York (EDT, UTC-4).
	at := time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC)
	got := scheduler.TradeDate(at, loc)
	if got != "2026-07-27" {
		t.Fatalf("TradeDate: got %q want 2026-07-27", got)
	}
}

type recordingRunner struct {
	mu     sync.Mutex
	params []workflow.RunParams
	err    error
}

func (r *recordingRunner) RunEOD(_ context.Context, params workflow.RunParams) (uint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.params = append(r.params, params)
	if r.err != nil {
		return 0, r.err
	}
	return 1, nil
}

type fakeSource struct {
	mu    sync.Mutex
	strat *models.Strategy
	err   error
}

func (f *fakeSource) Active(context.Context) (*models.Strategy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.strat, f.err
}

func (f *fakeSource) set(strat *models.Strategy) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strat = strat
}

func defaultStrategy(id uint) *models.Strategy {
	return &models.Strategy{
		ID:                   id,
		Name:                 "整体策略1",
		IsActive:             true,
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        workflow.ExecutionModeAutoReject,
	}
}

func TestReloadRegistersJobsFromActiveStrategy(t *testing.T) {
	loc := mustNY(t)
	runner := &recordingRunner{}
	src := &fakeSource{strat: defaultStrategy(1)}

	s, err := scheduler.New(scheduler.Options{
		Runner:   runner,
		Location: loc,
		Source:   src,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := s.JobCount(); got < 7 {
		t.Fatalf("JobCount: got %d want >= 7 (1 pre-open + 6 hourly)", got)
	}

	src.set(nil)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatalf("Reload cleanup: %v", err)
	}
}

func TestReloadReplacesJobs(t *testing.T) {
	loc := mustNY(t)
	runner := &recordingRunner{}
	src := &fakeSource{strat: defaultStrategy(1)}

	s, err := scheduler.New(scheduler.Options{
		Runner:   runner,
		Location: loc,
		Source:   src,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Reload(context.Background()); err != nil {
		t.Fatalf("Reload #1: %v", err)
	}
	first := s.JobCount()
	if first < 7 {
		t.Fatalf("first JobCount: got %d want >= 7", first)
	}

	src.set(&models.Strategy{
		ID:                   2,
		Name:                 "pre-open only",
		IsActive:             true,
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 0,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        workflow.ExecutionModeRequireApproval,
	})
	if err := s.Reload(context.Background()); err != nil {
		t.Fatalf("Reload #2: %v", err)
	}
	if got := s.JobCount(); got != 1 {
		t.Fatalf("after replace JobCount: got %d want 1", got)
	}
}

func TestReloadNoActiveStrategyRegistersNoJobs(t *testing.T) {
	loc := mustNY(t)
	runner := &recordingRunner{}
	src := &fakeSource{strat: nil}

	s, err := scheduler.New(scheduler.Options{
		Runner:   runner,
		Location: loc,
		Source:   src,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := s.JobCount(); got != 0 {
		t.Fatalf("JobCount: got %d want 0", got)
	}
}

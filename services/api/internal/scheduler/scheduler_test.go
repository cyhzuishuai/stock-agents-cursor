package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/scheduler"
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
	tradeDates []string
}

func (r *recordingRunner) RunEOD(_ context.Context, tradeDate string) (uint, error) {
	r.tradeDates = append(r.tradeDates, tradeDate)
	return 1, nil
}

func TestSchedulerRunsOnInjectedTick(t *testing.T) {
	loc := mustNY(t)
	runner := &recordingRunner{}
	now := time.Date(2026, 7, 27, 16, 30, 0, 0, loc)

	s, err := scheduler.New(scheduler.Options{
		Runner:   runner,
		CronExpr: scheduler.DefaultCronExpr,
		Location: loc,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(runner.tradeDates) != 1 || runner.tradeDates[0] != "2026-07-27" {
		t.Fatalf("RunEOD trade dates: %#v", runner.tradeDates)
	}
}

func TestSchedulerSkipsNonMatchingTick(t *testing.T) {
	loc := mustNY(t)
	runner := &recordingRunner{}
	now := time.Date(2026, 7, 25, 16, 30, 0, 0, loc) // Saturday

	s, err := scheduler.New(scheduler.Options{
		Runner:   runner,
		CronExpr: scheduler.DefaultCronExpr,
		Location: loc,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(runner.tradeDates) != 0 {
		t.Fatalf("expected no run on Saturday, got %#v", runner.tradeDates)
	}
}

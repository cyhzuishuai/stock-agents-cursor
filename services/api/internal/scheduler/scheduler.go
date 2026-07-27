// Package scheduler triggers workflow runs from the active strategy schedule in US/Eastern.
//
// Jobs are derived via strategy.BuildJobSpecs (pre-open + intraday). Hot-reload on
// activate/PATCH replaces cron entries. EOD_CRON is legacy-only (see deploy/README.md);
// with no active strategy the scheduler registers no automatic ticks.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/robfig/cron/v3"
)

const (
	// DefaultLocation is the US/Eastern market timezone used for scheduling.
	DefaultLocation = "America/New_York"
	// DefaultCronExpr is the legacy single-tick expression (Mon–Fri 16:30 ET).
	DefaultCronExpr = "30 16 * * 1-5"
)

// EODRunner runs the end-of-day workflow for a trade date.
type EODRunner interface {
	RunEOD(ctx context.Context, params workflow.RunParams) (uint, error)
}

// StrategySource loads the currently active strategy (nil if none).
type StrategySource interface {
	Active(ctx context.Context) (*models.Strategy, error)
}

// Options configures the strategy-driven scheduler.
type Options struct {
	Runner   EODRunner
	Location *time.Location
	Now      func() time.Time
	Source   StrategySource
}

// Scheduler runs workflow ticks from the active strategy under a mutex-guarded cron.
type Scheduler struct {
	runner EODRunner
	loc    *time.Location
	now    func() time.Time
	source StrategySource
	mu     sync.Mutex
	cron   *cron.Cron
}

// CronExprFromEnv returns EOD_CRON or DefaultCronExpr (legacy; DB strategy is authoritative).
func CronExprFromEnv() string {
	if v := os.Getenv("EOD_CRON"); v != "" {
		return v
	}
	return DefaultCronExpr
}

// NewYorkLocation loads America/New_York or falls back to UTC.
func NewYorkLocation() *time.Location {
	loc, err := time.LoadLocation(DefaultLocation)
	if err != nil {
		return time.UTC
	}
	return loc
}

// New validates options and builds a Scheduler.
func New(opts Options) (*Scheduler, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("scheduler: runner is required")
	}
	loc := opts.Location
	if loc == nil {
		loc = NewYorkLocation()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{
		runner: opts.Runner,
		loc:    loc,
		now:    now,
		source: opts.Source,
	}, nil
}

// MatchesSchedule reports whether at falls on a scheduled cron tick in loc.
func MatchesSchedule(at time.Time, expr string, loc *time.Location) (bool, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return false, fmt.Errorf("parse cron %q: %w", expr, err)
	}
	inLoc := at.In(loc)
	next := schedule.Next(inLoc.Add(-59 * time.Second))
	return next.Truncate(time.Minute).Equal(inLoc.Truncate(time.Minute)), nil
}

// TradeDate returns YYYY-MM-DD for at in the given location.
func TradeDate(at time.Time, loc *time.Location) string {
	return at.In(loc).Format("2006-01-02")
}

// JobCount returns the number of registered cron entries (for tests).
func (s *Scheduler) JobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron == nil {
		return 0
	}
	return len(s.cron.Entries())
}

// Reload stops existing jobs and registers ticks from the active strategy.
func (s *Scheduler) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var strat *models.Strategy
	var jobs []strategy.JobSpec
	if s.source != nil {
		active, err := s.source.Active(ctx)
		if err != nil {
			return err
		}
		strat = active
		if strat != nil {
			jobs, err = strategy.BuildJobSpecs(*strat)
			if err != nil {
				return fmt.Errorf("scheduler: build job specs: %w", err)
			}
		}
	}

	if s.cron != nil {
		stopCtx := s.cron.Stop()
		<-stopCtx.Done()
		s.cron = nil
	}

	c := cron.New(cron.WithLocation(s.loc))
	if strat == nil {
		fmt.Fprintf(os.Stderr, "scheduler: no active strategy; registering no jobs\n")
	} else {
		id := strat.ID
		mode := strat.ExecutionMode
		for _, job := range jobs {
			trigger := job.Trigger
			expr := job.CronExpr
			if _, err := c.AddFunc(expr, func() {
				s.runJob(context.Background(), id, trigger, mode)
			}); err != nil {
				return fmt.Errorf("scheduler: add cron %q: %w", expr, err)
			}
		}
	}
	s.cron = c
	c.Start()
	return nil
}

func (s *Scheduler) runJob(ctx context.Context, strategyID uint, trigger, executionMode string) {
	now := s.now()
	sid := strategyID
	_, err := s.runner.RunEOD(ctx, workflow.RunParams{
		TradeDate:     TradeDate(now, s.loc),
		StrategyID:    &sid,
		Trigger:       trigger,
		ExecutionMode: executionMode,
	})
	if err == nil {
		return
	}
	if errors.Is(err, workflow.ErrLockHeld) {
		fmt.Fprintf(os.Stderr, "scheduler: skip (lock held): %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "scheduler: eod run: %v\n", err)
}

// Start performs an initial Reload then blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) error {
	if err := s.Reload(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil {
		stopCtx := s.cron.Stop()
		<-stopCtx.Done()
		s.cron = nil
	}
	return nil
}

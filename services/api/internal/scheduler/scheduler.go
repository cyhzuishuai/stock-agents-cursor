// Package scheduler triggers end-of-day workflow runs on a cron schedule in US/Eastern.
//
// Default schedule: 16:30 America/New_York, Monday–Friday (cron "30 16 * * 1-5").
// Override with EOD_CRON; expressions are evaluated in America/New_York unless the
// expression includes an explicit timezone (robfig/cron v3).
//
// Manual trigger: POST /internal/eod/run with header X-Internal-Token (INTERNAL_EOD_TOKEN).
package scheduler

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/robfig/cron/v3"
)

const (
	// DefaultLocation is the US/Eastern market timezone used for EOD scheduling.
	DefaultLocation = "America/New_York"
	// DefaultCronExpr fires at 16:30 Mon–Fri in DefaultLocation (after regular close).
	DefaultCronExpr = "30 16 * * 1-5"
)

// EODRunner runs the end-of-day workflow for a trade date.
type EODRunner interface {
	RunEOD(ctx context.Context, params workflow.RunParams) (uint, error)
}

// Options configures the EOD scheduler.
type Options struct {
	Runner   EODRunner
	CronExpr string
	Location *time.Location
	Now      func() time.Time
}

// Scheduler runs EOD on a cron schedule.
type Scheduler struct {
	runner   EODRunner
	cronExpr string
	loc      *time.Location
	now      func() time.Time
	schedule cron.Schedule
	cron     *cron.Cron
}

// CronExprFromEnv returns EOD_CRON or DefaultCronExpr.
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
	expr := opts.CronExpr
	if expr == "" {
		expr = CronExprFromEnv()
	}
	loc := opts.Location
	if loc == nil {
		loc = NewYorkLocation()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("scheduler: parse cron %q: %w", expr, err)
	}

	return &Scheduler{
		runner:   opts.Runner,
		cronExpr: expr,
		loc:      loc,
		now:      now,
		schedule: schedule,
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

// Tick runs EOD once when the injected/current time matches the schedule.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.now()
	ok, err := MatchesSchedule(now, s.cronExpr, s.loc)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	tradeDate := TradeDate(now, s.loc)
	_, err = s.runner.RunEOD(ctx, workflow.RunParams{
		TradeDate: tradeDate,
		Trigger:   workflow.TriggerLegacyEOD,
	})
	return err
}

// Start registers the cron job and blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.cron != nil {
		return fmt.Errorf("scheduler: already started")
	}
	c := cron.New(cron.WithLocation(s.loc))
	_, err := c.AddFunc(s.cronExpr, func() {
		if err := s.Tick(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "scheduler: eod tick: %v\n", err)
		}
	})
	if err != nil {
		return fmt.Errorf("scheduler: add cron: %w", err)
	}
	s.cron = c
	c.Start()

	<-ctx.Done()
	stopCtx := c.Stop()
	<-stopCtx.Done()
	return nil
}

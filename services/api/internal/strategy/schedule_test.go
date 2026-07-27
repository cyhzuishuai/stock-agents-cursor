package strategy

import (
	"fmt"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestBuildJobSpecsDefaultStrategy(t *testing.T) {
	s := models.Strategy{
		Name:                 "Test Strategy",
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        ExecutionModeAutoReject,
	}
	jobs, err := BuildJobSpecs(s)
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	var exprs []string
	for _, j := range jobs {
		triggers = append(triggers, j.Trigger)
		exprs = append(exprs, j.CronExpr)
	}
	if !contains(exprs, "20 9 * * 1-5") {
		t.Fatalf("missing pre-open: %#v", exprs)
	}
	for _, hour := range []int{10, 11, 12, 13, 14, 15} {
		want := fmt.Sprintf("0 %d * * 1-5", hour)
		if !contains(exprs, want) {
			t.Fatalf("missing %s in %#v", want, exprs)
		}
	}
}

func TestBuildJobSpecsNoPreOpenWhenZero(t *testing.T) {
	s := models.Strategy{
		Name:                 "Test Strategy",
		PreOpenMinutes:       0,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        ExecutionModeAutoReject,
	}
	jobs, err := BuildJobSpecs(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Trigger == TriggerPreOpen {
			t.Fatalf("unexpected pre_open job: %#v", j)
		}
	}
}

func TestBuildJobSpecsNoIntradayWhenZero(t *testing.T) {
	s := models.Strategy{
		Name:                 "Test Strategy",
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 0,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        ExecutionModeAutoReject,
	}
	jobs, err := BuildJobSpecs(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Trigger == TriggerIntraday {
			t.Fatalf("unexpected intraday job: %#v", j)
		}
	}
}

func TestBuildJobSpecsInvalidTime(t *testing.T) {
	s := models.Strategy{
		Name:                 "Test Strategy",
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "bad",
		IntradayEndET:        "15:00",
		ExecutionMode:        ExecutionModeAutoReject,
	}
	_, err := BuildJobSpecs(s)
	if err == nil {
		t.Fatal("expected error for invalid HH:MM")
	}
}

func TestBuildJobSpecsInvalidInterval(t *testing.T) {
	s := models.Strategy{
		Name:                 "Test Strategy",
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 10,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        ExecutionModeAutoReject,
	}
	_, err := BuildJobSpecs(s)
	if err == nil {
		t.Fatal("expected error for intraday_every_minutes < 15")
	}
}

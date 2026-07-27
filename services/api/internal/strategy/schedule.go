package strategy

import (
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

const (
	TriggerPreOpen  = "pre_open"
	TriggerIntraday = "intraday"
)

type JobSpec struct {
	CronExpr string // robfig minute-hour-dom-month-dow, Mon-Fri
	Trigger  string // pre_open | intraday
}

const usRegularOpenMinutes = 9*60 + 30

func BuildJobSpecs(s models.Strategy) ([]JobSpec, error) {
	if err := ValidateStrategyFields(s); err != nil {
		return nil, err
	}

	var jobs []JobSpec

	if s.PreOpenMinutes > 0 {
		fire := usRegularOpenMinutes - s.PreOpenMinutes
		if fire < 0 {
			return nil, fmt.Errorf("pre_open_minutes exceeds market open offset")
		}
		jobs = append(jobs, JobSpec{
			CronExpr: minutesToCron(fire),
			Trigger:  TriggerPreOpen,
		})
	}

	if s.IntradayEveryMinutes > 0 {
		start, err := parseHHMM(s.IntradayStartET)
		if err != nil {
			return nil, err
		}
		end, err := parseHHMM(s.IntradayEndET)
		if err != nil {
			return nil, err
		}
		for t := start; t <= end; t += s.IntradayEveryMinutes {
			jobs = append(jobs, JobSpec{
				CronExpr: minutesToCron(t),
				Trigger:  TriggerIntraday,
			})
		}
	}

	return jobs, nil
}

func minutesToCron(minutes int) string {
	return fmt.Sprintf("%d %d * * 1-5", minutes%60, minutes/60)
}

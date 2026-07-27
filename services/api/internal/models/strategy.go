package models

import "time"

type Strategy struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	Name                 string    `gorm:"uniqueIndex;size:128" json:"name"`
	Description          string    `gorm:"type:text" json:"description"`
	IsSystemDefault      bool      `json:"is_system_default"`
	IsActive             bool      `gorm:"index" json:"is_active"`
	PreOpenMinutes       int       `json:"pre_open_minutes"`
	IntradayEveryMinutes int       `json:"intraday_every_minutes"`
	IntradayStartET      string    `gorm:"size:5" json:"intraday_start_et"`
	IntradayEndET        string    `gorm:"size:5" json:"intraday_end_et"`
	ExecutionMode        string    `gorm:"size:64" json:"execution_mode"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

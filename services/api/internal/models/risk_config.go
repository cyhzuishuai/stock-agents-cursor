package models

type RiskRuleConfig struct {
	ID         uint    `gorm:"primaryKey" json:"id"`
	Key        string  `gorm:"uniqueIndex" json:"key"`
	ValueFloat float64 `json:"value_float"`
}

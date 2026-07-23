package models

type NavSnapshot struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	TradeDate string  `gorm:"uniqueIndex" json:"trade_date"`
	Nav       float64 `json:"nav"`
	Cash      float64 `json:"cash"`
	Equity    float64 `json:"equity"`
}

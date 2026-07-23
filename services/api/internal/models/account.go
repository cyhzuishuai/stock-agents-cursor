package models

type Account struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	Currency       string  `json:"currency"`
	Cash           float64 `json:"cash"`
	InitialCapital float64 `json:"initial_capital"`
}

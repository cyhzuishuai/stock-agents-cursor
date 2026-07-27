package models

type WatchlistSymbol struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Symbol  string `gorm:"uniqueIndex" json:"symbol"`
	CanHold bool   `gorm:"not null;default:true" json:"can_hold"`
}

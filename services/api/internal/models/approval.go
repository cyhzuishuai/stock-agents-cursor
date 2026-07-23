package models

type Approval struct {
	ID                uint   `gorm:"primaryKey" json:"id"`
	ProposalID        uint   `gorm:"uniqueIndex" json:"proposal_id"`
	Status            string `json:"status"` // pending|approved|rejected
	BreachReasonsJSON string `gorm:"type:text" json:"breach_reasons"`
	Note              string `json:"note"`
	DecidedBy         *uint  `json:"decided_by"`
}

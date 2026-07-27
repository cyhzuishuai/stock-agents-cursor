package models

type WorkflowRun struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TradeDate  string `gorm:"index" json:"trade_date"`
	Status     string `gorm:"index" json:"status"`
	ErrorMsg   string `json:"error_msg"`
	StrategyID *uint  `json:"strategy_id"`
	Trigger    string `json:"trigger"`
}

type WorkflowStepResult struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	RunID       uint   `gorm:"index" json:"run_id"`
	Step        string `json:"step"`
	Status      string `json:"status"`
	PayloadJSON string `gorm:"type:text" json:"payload_json"`
}

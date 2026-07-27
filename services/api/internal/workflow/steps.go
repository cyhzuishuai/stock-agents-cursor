package workflow

import "time"

const (
	StepCreated   = "created"
	StepData      = "data"
	StepResearch  = "research"
	StepDecision  = "decision"
	StepPortfolio = "portfolio"
	StepRisk      = "risk"

	StatusCreated          = "created"
	StatusFailed           = "failed"
	StatusAwaitingApproval = "awaiting_approval"
	StatusExecuted         = "executed"
	StatusCancelled        = "cancelled"

	ProposalPendingAuto      = "pending_auto"
	ProposalAwaitingApproval = "awaiting_approval"
	ProposalSubmitted        = "submitted"
	ProposalFilled           = "filled"
	ProposalRejected         = "rejected"
	ProposalCancelled        = "cancelled"

	ApprovalPending   = "pending"
	ApprovalApproved  = "approved"
	ApprovalRejected  = "rejected"
	ApprovalCancelled = "cancelled"

	StepStatusOK     = "ok"
	StepStatusFailed = "failed"

	TriggerManual    = "manual"
	TriggerPreOpen   = "pre_open"
	TriggerIntraday  = "intraday"
	TriggerLegacyEOD = "legacy_eod"

	ExecutionModeAutoReject      = "auto_reject_breaches"
	ExecutionModeRequireApproval = "require_approval"
	ExecutionModeBypassRisk      = "bypass_risk"

	DefaultLockTTL   = 30 * time.Minute
	DataAgentTimeout = 60 * time.Second
	LLMAgentTimeout  = 120 * time.Second

	BrokerSyncTimeout      = 15 * time.Second
	BrokerSyncPollInterval = 250 * time.Millisecond
)

// AgentStep describes one sequential agent call in the EOD chain.
type AgentStep struct {
	Name    string
	Timeout time.Duration
}

// AgentChain is data → research → decision → portfolio → risk.
func AgentChain() []AgentStep {
	return []AgentStep{
		{Name: StepData, Timeout: DataAgentTimeout},
		{Name: StepResearch, Timeout: LLMAgentTimeout},
		{Name: StepDecision, Timeout: LLMAgentTimeout},
		{Name: StepPortfolio, Timeout: LLMAgentTimeout},
		{Name: StepRisk, Timeout: LLMAgentTimeout},
	}
}

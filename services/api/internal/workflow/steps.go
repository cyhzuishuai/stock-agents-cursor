package workflow

import "time"

const (
	StepCreated   = "created"
	StepData      = "data"
	StepResearch  = "research"
	StepDecision  = "decision"
	StepPortfolio = "portfolio"
	StepRisk      = "risk"

	StatusCreated           = "created"
	StatusFailed            = "failed"
	StatusAwaitingApproval  = "awaiting_approval"
	StatusExecuted          = "executed"

	ProposalPendingAuto      = "pending_auto"
	ProposalAwaitingApproval = "awaiting_approval"
	ProposalFilled           = "filled"

	ApprovalPending = "pending"

	StepStatusOK     = "ok"
	StepStatusFailed = "failed"

	DefaultLockTTL       = 30 * time.Minute
	DataAgentTimeout     = 60 * time.Second
	LLMAgentTimeout      = 120 * time.Second
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

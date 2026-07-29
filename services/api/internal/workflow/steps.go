package workflow

import "time"

const (
	StepCreated   = "created"
	StepAnalyst   = "analyst"
	StepPortfolio = "portfolio"

	// Legacy step names retained for historical WorkflowStepResult rows / UI reads.
	StepData     = "data"
	StepResearch = "research"
	StepDecision = "decision"
	StepRisk     = "risk"

	StatusCreated          = "created"
	StatusFailed           = "failed"
	StatusAwaitingApproval = "awaiting_approval"
	StatusAwaitingAgentInput = "awaiting_agent_input"
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

	StepStatusOK           = "ok"
	StepStatusFailed       = "failed"
	StepStatusInterrupted  = "interrupted"

	TriggerManual   = "manual"
	TriggerPreOpen  = "pre_open"
	TriggerIntraday = "intraday"

	ExecutionModeAutoReject      = "auto_reject_breaches"
	ExecutionModeRequireApproval = "require_approval"
	ExecutionModeBypassRisk      = "bypass_risk"

	DefaultLockTTL        = 30 * time.Minute
	DataAgentTimeout      = 60 * time.Second // legacy
	// Live plan/act/reflect + tools routinely exceed 2–3 minutes per agent.
	LLMAgentTimeout     = 480 * time.Second
	AnalystAgentTimeout = 600 * time.Second

	BrokerSyncTimeout      = 15 * time.Second
	BrokerSyncPollInterval = 250 * time.Millisecond
)

// AgentStep describes one sequential agent call in the workflow chain.
type AgentStep struct {
	Name    string
	Timeout time.Duration
}

// AgentChain is analyst → portfolio (tool-loop runtime).
func AgentChain() []AgentStep {
	return []AgentStep{
		{Name: StepAnalyst, Timeout: AnalystAgentTimeout},
		{Name: StepPortfolio, Timeout: LLMAgentTimeout},
	}
}

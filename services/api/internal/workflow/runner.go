package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/risk"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	// ErrInvalidPortfolioSchema is returned when portfolio agent output fails schema checks.
	ErrInvalidPortfolioSchema = errors.New("invalid portfolio_result schema")
	// ErrRunNotAwaitingAgentInput is returned when ResumeAgent is called on a run that is not paused for HITL.
	ErrRunNotAwaitingAgentInput = errors.New("run is not awaiting agent input")
)

// RunParams configures a single workflow execution.
type RunParams struct {
	TradeDate     string
	Force         bool // retained for API compat; no longer blocks same-day sequential runs
	StrategyID    *uint
	Trigger       string // manual|pre_open|intraday
	ExecutionMode string // empty → require_approval (or strategy if StrategyID set)
}

// LedgerAPI is the ledger surface used by the workflow runner.
type LedgerAPI interface {
	AccountSnapshot(ctx context.Context, accountID uint) (ledger.AccountSnapshot, error)
	ApplyFill(ctx context.Context, req ledger.FillRequest) (models.Order, error)
	UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (models.NavSnapshot, error)
}

type Runner struct {
	DB     *gorm.DB
	Agents *agentsclient.Client
	Ledger LedgerAPI
	Risk   risk.Engine
	Redis  redis.Cmdable
	Broker broker.Client // required in production; tests use a fake
	Config *config.Config
}

type agentRunRequest struct {
	RunID            string               `json:"run_id"`
	TradeDate        string               `json:"trade_date"`
	Watchlist        []string             `json:"watchlist"`
	Agent            string               `json:"agent"`
	ThreadID         string               `json:"thread_id,omitempty"`
	AccountSnapshot  AgentAccountSnapshot `json:"account_snapshot"`
	RiskContext      AgentRiskContext     `json:"risk_context"`
	PriorStepOutputs map[string]any       `json:"prior_step_outputs"`
}

type agentEnvelope struct {
	Status        string          `json:"status"`
	ThreadID      string          `json:"thread_id"`
	HumanRequest  json.RawMessage `json:"human_request"`
	Result        json.RawMessage `json:"result"`
	Trace         json.RawMessage `json:"trace"`
	Handoff       json.RawMessage `json:"handoff"`
	WorkingMemory json.RawMessage `json:"working_memory"`
}

type agentResumeRequest struct {
	ThreadID      string          `json:"thread_id"`
	HumanResponse json.RawMessage `json:"human_response"`
}

type portfolioProposal struct {
	Symbol              string   `json:"symbol"`
	Side                string   `json:"side"`
	Qty                 float64  `json:"qty"`
	TargetWeight        *float64 `json:"target_weight"`
	StopLoss            *float64 `json:"stop_loss"`
	TakeProfit          *float64 `json:"take_profit"`
	EstimatedNotional   float64  `json:"estimated_notional"`
	EstimatedCashImpact float64  `json:"estimated_cash_impact"`
}

type portfolioResult struct {
	Proposals []portfolioProposal `json:"proposals"`
}

func (r *Runner) RunWorkflow(ctx context.Context, params RunParams) (runID uint, err error) {
	unlock, err := AcquireWorkflowLock(ctx, r.Redis, DefaultLockTTL)
	if err != nil {
		return 0, err
	}
	defer unlock()

	mode, err := r.resolveExecutionMode(ctx, params)
	if err != nil {
		return 0, err
	}

	run := models.WorkflowRun{
		TradeDate:  params.TradeDate,
		Status:     StatusCreated,
		StrategyID: params.StrategyID,
		Trigger:    params.Trigger,
	}
	if err := r.DB.WithContext(ctx).Create(&run).Error; err != nil {
		return 0, fmt.Errorf("create workflow run: %w", err)
	}

	return run.ID, r.runWorkflow(ctx, &run, mode)
}

func (r *Runner) resolveExecutionMode(ctx context.Context, params RunParams) (string, error) {
	if params.ExecutionMode != "" {
		return params.ExecutionMode, nil
	}
	if params.StrategyID != nil {
		var st models.Strategy
		if err := r.DB.WithContext(ctx).First(&st, *params.StrategyID).Error; err != nil {
			return "", fmt.Errorf("load strategy %d: %w", *params.StrategyID, err)
		}
		if st.ExecutionMode != "" {
			return st.ExecutionMode, nil
		}
	}
	return ExecutionModeRequireApproval, nil
}

func (r *Runner) runWorkflow(ctx context.Context, run *models.WorkflowRun, executionMode string) error {
	marks, anyFill, err := r.runWorkflowThroughFills(ctx, run, executionMode)
	if err != nil {
		if anyFill {
			_ = r.finalizeAfterPartialFill(ctx, run, err)
		} else if !isPostFillStatus(run.Status) && run.Status != StatusAwaitingAgentInput {
			_ = r.failRun(ctx, run, err)
		}
		return err
	}

	if run.Status == StatusAwaitingAgentInput {
		return nil
	}

	if err := r.upsertNAV(ctx, run.TradeDate, marks); err != nil {
		// Status already executed/awaiting_approval — do not overwrite to failed.
		return fmt.Errorf("upsert nav: %w", err)
	}
	return nil
}

func (r *Runner) upsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) error {
	if r.Broker != nil {
		acct, err := r.Broker.GetAccount(ctx)
		if err == nil {
			cash := acct.Cash
			nav := acct.Equity
			if nav == 0 && acct.PortfolioValue > 0 {
				nav = acct.PortfolioValue
			}
			equity := nav - cash
			if equity < 0 {
				equity = 0
			}
			snap := models.NavSnapshot{
				TradeDate: tradeDate,
				Cash:      cash,
				Equity:    equity,
				Nav:       nav,
			}
			var existing models.NavSnapshot
			if err := r.DB.WithContext(ctx).Where("trade_date = ?", tradeDate).Limit(1).Find(&existing).Error; err != nil {
				return err
			}
			if existing.ID == 0 {
				return r.DB.WithContext(ctx).Create(&snap).Error
			}
			snap.ID = existing.ID
			return r.DB.WithContext(ctx).Save(&snap).Error
		}
	}
	_, err := r.Ledger.UpsertNAV(ctx, tradeDate, marks)
	return err
}

func isPostFillStatus(status string) bool {
	return status == StatusExecuted || status == StatusAwaitingApproval
}

// finalizeAfterPartialFill sets a terminal run status after at least one successful fill
// followed by a later error: cancel pending_auto orphans; awaiting_approval if pending
// approvals remain, else failed.
func (r *Runner) finalizeAfterPartialFill(ctx context.Context, run *models.WorkflowRun, cause error) error {
	if err := r.DB.WithContext(ctx).Model(&models.TradeProposal{}).
		Where("run_id = ? AND status = ?", run.ID, ProposalPendingAuto).
		Update("status", ProposalCancelled).Error; err != nil {
		return fmt.Errorf("cancel pending_auto orphans: %w", err)
	}

	var pendingApprovals int64
	if err := r.DB.WithContext(ctx).Model(&models.Approval{}).
		Joins("JOIN trade_proposals ON trade_proposals.id = approvals.proposal_id").
		Where("trade_proposals.run_id = ? AND approvals.status = ?", run.ID, ApprovalPending).
		Count(&pendingApprovals).Error; err != nil {
		return fmt.Errorf("count pending approvals: %w", err)
	}

	var awaitingProposals int64
	if err := r.DB.WithContext(ctx).Model(&models.TradeProposal{}).
		Where("run_id = ? AND status = ?", run.ID, ProposalAwaitingApproval).
		Count(&awaitingProposals).Error; err != nil {
		return fmt.Errorf("count awaiting proposals: %w", err)
	}

	if pendingApprovals > 0 || awaitingProposals > 0 {
		run.ErrorMsg = cause.Error()
		return r.DB.WithContext(ctx).Model(run).Updates(map[string]any{
			"status":    StatusAwaitingApproval,
			"error_msg": cause.Error(),
		}).Error
	}

	run.Status = StatusFailed
	run.ErrorMsg = cause.Error()
	return r.DB.WithContext(ctx).Model(run).Updates(map[string]any{
		"status":    StatusFailed,
		"error_msg": cause.Error(),
	}).Error
}

func (r *Runner) runWorkflowThroughFills(ctx context.Context, run *models.WorkflowRun, executionMode string) (map[string]float64, bool, error) {
	account, err := r.loadAccount(ctx)
	if err != nil {
		return nil, false, err
	}
	watchlist, err := r.loadWatchlist(ctx)
	if err != nil {
		return nil, false, err
	}
	agentSnap, err := buildAgentSnapshot(ctx, r.Broker)
	if err != nil {
		return nil, false, fmt.Errorf("agent snapshot: %w", err)
	}
	riskCtx := buildRiskContext(executionMode, r.Config)

	prior := map[string]any{}
	interrupted, err := r.runAgentChain(ctx, run, AgentChain(), prior, watchlist, agentSnap, riskCtx)
	if err != nil {
		return nil, false, err
	}
	if interrupted {
		return nil, false, nil
	}

	return r.finishProposalsAndFills(ctx, run, executionMode, prior, account)
}

// ResumeAgent continues a run paused at awaiting_agent_input after Python HITL.
func (r *Runner) ResumeAgent(ctx context.Context, runID uint, agent string, humanResponse json.RawMessage) error {
	unlock, err := AcquireWorkflowLock(ctx, r.Redis, DefaultLockTTL)
	if err != nil {
		return err
	}
	defer unlock()

	var run models.WorkflowRun
	if err := r.DB.WithContext(ctx).First(&run, runID).Error; err != nil {
		return err
	}
	if run.Status != StatusAwaitingAgentInput {
		return ErrRunNotAwaitingAgentInput
	}

	stepMeta, ok := agentStepByName(agent)
	if !ok {
		return fmt.Errorf("unknown agent %q", agent)
	}

	var interruptedStep models.WorkflowStepResult
	if err := r.DB.WithContext(ctx).
		Where("run_id = ? AND step = ? AND status = ?", runID, agent, StepStatusInterrupted).
		First(&interruptedStep).Error; err != nil {
		return fmt.Errorf("interrupted step for agent %s: %w", agent, err)
	}

	mode, err := r.resolveExecutionMode(ctx, RunParams{StrategyID: run.StrategyID})
	if err != nil {
		return err
	}

	baseURL, err := r.agentURL(agent)
	if err != nil {
		return err
	}
	threadID := fmt.Sprintf("%d:%s", runID, agent)
	raw, err := r.Agents.Resume(ctx, baseURL, agentResumeRequest{
		ThreadID:      threadID,
		HumanResponse: humanResponse,
	}, stepMeta.Timeout)
	if err != nil {
		_ = r.updateStep(ctx, runID, agent, StepStatusFailed, fmt.Sprintf(`{"error":%q}`, err.Error()))
		_ = r.failRun(ctx, &run, err)
		return fmt.Errorf("resume agent %s: %w", agent, err)
	}

	var envelope agentEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		_ = r.failRun(ctx, &run, err)
		return fmt.Errorf("decode resume envelope: %w", err)
	}
	if isInterruptedEnvelope(envelope) {
		if err := r.updateStep(ctx, runID, agent, StepStatusInterrupted, string(raw)); err != nil {
			return err
		}
		return nil
	}

	if err := r.updateStep(ctx, runID, agent, StepStatusOK, string(raw)); err != nil {
		return err
	}

	prior, err := r.loadPriorFromOKSteps(ctx, runID)
	if err != nil {
		_ = r.failRun(ctx, &run, err)
		return err
	}

	remaining, err := remainingAgentChain(agent)
	if err != nil {
		_ = r.failRun(ctx, &run, err)
		return err
	}

	watchlist, err := r.loadWatchlist(ctx)
	if err != nil {
		_ = r.failRun(ctx, &run, err)
		return err
	}
	agentSnap, err := buildAgentSnapshot(ctx, r.Broker)
	if err != nil {
		_ = r.failRun(ctx, &run, err)
		return fmt.Errorf("agent snapshot: %w", err)
	}
	riskCtx := buildRiskContext(mode, r.Config)

	interrupted, err := r.runAgentChain(ctx, &run, remaining, prior, watchlist, agentSnap, riskCtx)
	if err != nil {
		if run.Status != StatusAwaitingAgentInput {
			_ = r.failRun(ctx, &run, err)
		}
		return err
	}
	if interrupted {
		return nil
	}

	account, err := r.loadAccount(ctx)
	if err != nil {
		_ = r.failRun(ctx, &run, err)
		return err
	}
	marks, anyFill, err := r.finishProposalsAndFills(ctx, &run, mode, prior, account)
	if err != nil {
		if anyFill {
			_ = r.finalizeAfterPartialFill(ctx, &run, err)
		} else if !isPostFillStatus(run.Status) && run.Status != StatusAwaitingAgentInput {
			_ = r.failRun(ctx, &run, err)
		}
		return err
	}
	if run.Status == StatusAwaitingAgentInput {
		return nil
	}
	if err := r.upsertNAV(ctx, run.TradeDate, marks); err != nil {
		return fmt.Errorf("upsert nav: %w", err)
	}
	return nil
}

func (r *Runner) runAgentChain(
	ctx context.Context,
	run *models.WorkflowRun,
	chain []AgentStep,
	prior map[string]any,
	watchlist []string,
	agentSnap AgentAccountSnapshot,
	riskCtx AgentRiskContext,
) (interrupted bool, err error) {
	for _, step := range chain {
		if err := r.setRunStatus(ctx, run, step.Name); err != nil {
			return false, err
		}
		baseURL, err := r.agentURL(step.Name)
		if err != nil {
			return false, err
		}
		body := agentRunRequest{
			RunID:            strconv.FormatUint(uint64(run.ID), 10),
			TradeDate:        run.TradeDate,
			Watchlist:        watchlist,
			Agent:            step.Name,
			ThreadID:         fmt.Sprintf("%d:%s", run.ID, step.Name),
			AccountSnapshot:  agentSnap,
			RiskContext:      riskCtx,
			PriorStepOutputs: prior,
		}
		raw, err := r.Agents.Call(ctx, baseURL, body, step.Timeout)
		if err != nil {
			_ = r.persistStep(ctx, run.ID, step.Name, StepStatusFailed, fmt.Sprintf(`{"error":%q}`, err.Error()))
			return false, fmt.Errorf("agent %s: %w", step.Name, err)
		}
		var envelope agentEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return false, fmt.Errorf("decode %s envelope: %w", step.Name, err)
		}
		if isInterruptedEnvelope(envelope) {
			if err := r.persistStep(ctx, run.ID, step.Name, StepStatusInterrupted, string(raw)); err != nil {
				return false, err
			}
			if err := r.setRunStatus(ctx, run, StatusAwaitingAgentInput); err != nil {
				return false, err
			}
			return true, nil
		}
		// Persist full raw envelope {result,trace}.
		if err := r.persistStep(ctx, run.ID, step.Name, StepStatusOK, string(raw)); err != nil {
			return false, err
		}
		if err := applyEnvelopeToPrior(step.Name, envelope, prior); err != nil {
			return false, err
		}
	}
	return false, nil
}

func isInterruptedEnvelope(envelope agentEnvelope) bool {
	return envelope.Status == StepStatusInterrupted || envelope.Status == "interrupted"
}

func applyEnvelopeToPrior(stepName string, envelope agentEnvelope, prior map[string]any) error {
	if len(envelope.Result) == 0 {
		return fmt.Errorf("agent %s: missing result in envelope", stepName)
	}
	var resultObj any
	if err := json.Unmarshal(envelope.Result, &resultObj); err != nil {
		return fmt.Errorf("decode %s result: %w", stepName, err)
	}
	prior[stepName] = resultObj
	if stepName == StepAnalyst {
		if len(envelope.Handoff) > 0 && string(envelope.Handoff) != "null" {
			var handoff any
			if err := json.Unmarshal(envelope.Handoff, &handoff); err != nil {
				return fmt.Errorf("decode analyst handoff: %w", err)
			}
			prior["analyst_handoff"] = handoff
		}
		if len(envelope.WorkingMemory) > 0 && string(envelope.WorkingMemory) != "null" {
			var mem any
			if err := json.Unmarshal(envelope.WorkingMemory, &mem); err != nil {
				return fmt.Errorf("decode analyst working_memory: %w", err)
			}
			prior["analyst_working_memory"] = mem
		}
	}
	return nil
}

func (r *Runner) loadPriorFromOKSteps(ctx context.Context, runID uint) (map[string]any, error) {
	var steps []models.WorkflowStepResult
	if err := r.DB.WithContext(ctx).
		Where("run_id = ? AND status = ?", runID, StepStatusOK).
		Order("id ASC").
		Find(&steps).Error; err != nil {
		return nil, err
	}
	prior := map[string]any{}
	for _, step := range steps {
		var envelope agentEnvelope
		if err := json.Unmarshal([]byte(step.PayloadJSON), &envelope); err != nil {
			return nil, fmt.Errorf("decode stored %s envelope: %w", step.Step, err)
		}
		if err := applyEnvelopeToPrior(step.Step, envelope, prior); err != nil {
			return nil, err
		}
	}
	return prior, nil
}

func agentStepByName(name string) (AgentStep, bool) {
	for _, step := range AgentChain() {
		if step.Name == name {
			return step, true
		}
	}
	return AgentStep{}, false
}

func remainingAgentChain(afterAgent string) ([]AgentStep, error) {
	chain := AgentChain()
	for i, step := range chain {
		if step.Name == afterAgent {
			return chain[i+1:], nil
		}
	}
	return nil, fmt.Errorf("unknown agent %q", afterAgent)
}

func (r *Runner) finishProposalsAndFills(
	ctx context.Context,
	run *models.WorkflowRun,
	executionMode string,
	prior map[string]any,
	account models.Account,
) (map[string]float64, bool, error) {
	portRaw, err := json.Marshal(prior[StepPortfolio])
	if err != nil {
		return nil, false, fmt.Errorf("marshal portfolio result: %w", err)
	}
	if err := validatePortfolioResult(portRaw); err != nil {
		return nil, false, err
	}
	var port portfolioResult
	if err := json.Unmarshal(portRaw, &port); err != nil {
		return nil, false, fmt.Errorf("parse portfolio proposals: %w", err)
	}

	proposals := make([]models.TradeProposal, 0, len(port.Proposals))
	for _, p := range port.Proposals {
		proposals = append(proposals, models.TradeProposal{
			RunID:               run.ID,
			Symbol:              p.Symbol,
			Side:                p.Side,
			Qty:                 p.Qty,
			TargetWeight:        p.TargetWeight,
			StopLoss:            p.StopLoss,
			TakeProfit:          p.TakeProfit,
			EstimatedNotional:   p.EstimatedNotional,
			EstimatedCashImpact: p.EstimatedCashImpact,
			Status:              ProposalPendingAuto,
		})
	}
	if len(proposals) > 0 {
		if err := r.DB.WithContext(ctx).Create(&proposals).Error; err != nil {
			return nil, false, fmt.Errorf("insert proposals: %w", err)
		}
	}

	// Marks without data step: broker prices, else estimated_notional/qty.
	marks := map[string]float64{}
	marks = r.mergeBrokerMarks(ctx, marks)
	for _, p := range port.Proposals {
		if _, ok := marks[p.Symbol]; ok {
			continue
		}
		if p.Qty > 0 && p.EstimatedNotional > 0 {
			marks[p.Symbol] = p.EstimatedNotional / p.Qty
		}
	}

	state, err := r.portfolioState(ctx, account.ID, marks)
	if err != nil {
		return nil, false, err
	}

	canHold, err := r.loadCanHoldMap(ctx)
	if err != nil {
		return nil, false, err
	}

	pendingApprovals := false
	anyFill := false
	for i := range proposals {
		p := &proposals[i]
		if strings.EqualFold(p.Side, "buy") && !canHold[p.Symbol] {
			reasons, err := json.Marshal([]string{"not_holdable"})
			if err != nil {
				return nil, anyFill, err
			}
			p.Status = ProposalRejected
			p.BreachReasonsJSON = string(reasons)
			if err := r.DB.WithContext(ctx).Model(p).Updates(map[string]any{
				"status":              ProposalRejected,
				"breach_reasons_json": string(reasons),
			}).Error; err != nil {
				return nil, anyFill, err
			}
			continue
		}
		markPrice, ok := marks[p.Symbol]
		if !ok || markPrice <= 0 {
			return nil, anyFill, fmt.Errorf("missing close for symbol %s", p.Symbol)
		}

		allowSubmit := false
		if executionMode == ExecutionModeBypassRisk {
			allowSubmit = p.Qty > 0
		} else {
			// Gate math must use qty×mark, not agent-estimated notional/cash impact.
			// Marks are for risk gate only — never used as fill price.
			notional, cashImpact := fillNotionalAndCashImpact(p.Side, p.Qty, markPrice)
			decision := r.Risk.Evaluate(state, risk.Proposal{
				Symbol:              p.Symbol,
				Side:                p.Side,
				Qty:                 p.Qty,
				EstimatedNotional:   notional,
				EstimatedCashImpact: cashImpact,
				FillPrice:           markPrice,
			})
			if decision.AutoExecute {
				allowSubmit = true
			} else {
				reasons, err := json.Marshal(decision.BreachReasons)
				if err != nil {
					return nil, anyFill, err
				}
				if executionMode == ExecutionModeAutoReject {
					p.Status = ProposalRejected
					p.BreachReasonsJSON = string(reasons)
					if err := r.DB.WithContext(ctx).Model(p).Updates(map[string]any{
						"status":              ProposalRejected,
						"breach_reasons_json": string(reasons),
					}).Error; err != nil {
						return nil, anyFill, err
					}
					continue
				}

				approval := models.Approval{
					ProposalID:        p.ID,
					Status:            ApprovalPending,
					BreachReasonsJSON: string(reasons),
				}
				if err := r.DB.WithContext(ctx).Create(&approval).Error; err != nil {
					return nil, anyFill, fmt.Errorf("create approval: %w", err)
				}
				p.Status = ProposalAwaitingApproval
				if err := r.DB.WithContext(ctx).Model(p).Update("status", ProposalAwaitingApproval).Error; err != nil {
					return nil, anyFill, err
				}
				pendingApprovals = true
				continue
			}
		}

		if !allowSubmit {
			continue
		}

		if r.Broker == nil {
			return nil, anyFill, ErrBrokerNotConfigured
		}

		if err := r.submitAndSync(ctx, account.ID, run, p); err != nil {
			if !errors.Is(err, ErrSubmitOrder) {
				// Post-submit sync failure (mirror/GetOrder): leave proposal as submitted
				// for reconciliation; fail the run instead of treating it as a reject.
				return nil, anyFill, err
			}
			reasons, mErr := json.Marshal([]string{fmt.Sprintf("broker: %v", err)})
			if mErr != nil {
				return nil, anyFill, mErr
			}
			p.Status = ProposalRejected
			p.BreachReasonsJSON = string(reasons)
			if err := r.DB.WithContext(ctx).Model(p).Updates(map[string]any{
				"status":              ProposalRejected,
				"breach_reasons_json": string(reasons),
			}).Error; err != nil {
				return nil, anyFill, err
			}
			continue
		}
		if p.Status == ProposalFilled {
			anyFill = true
		}
		// Refresh state for subsequent proposals in the same run.
		state, err = r.portfolioState(ctx, account.ID, marks)
		if err != nil {
			return nil, anyFill, err
		}
	}

	finalStatus := StatusExecuted
	if pendingApprovals {
		finalStatus = StatusAwaitingApproval
	}
	if err := r.setRunStatus(ctx, run, finalStatus); err != nil {
		return nil, anyFill, err
	}
	return marks, true, nil
}

// validatePortfolioResult checks required fields from packages/contracts/portfolio_result.schema.json.
func validatePortfolioResult(raw []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPortfolioSchema, err)
	}
	if _, ok := probe["proposals"]; !ok {
		return fmt.Errorf("%w: missing required field proposals", ErrInvalidPortfolioSchema)
	}

	var port portfolioResult
	if err := json.Unmarshal(raw, &port); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPortfolioSchema, err)
	}
	if port.Proposals == nil {
		return fmt.Errorf("%w: proposals must be an array", ErrInvalidPortfolioSchema)
	}
	for i, p := range port.Proposals {
		if p.Symbol == "" {
			return fmt.Errorf("%w: proposal[%d] missing symbol", ErrInvalidPortfolioSchema, i)
		}
		if p.Side != "buy" && p.Side != "sell" {
			return fmt.Errorf("%w: proposal[%d] invalid side %q", ErrInvalidPortfolioSchema, i, p.Side)
		}
	}
	return nil
}

// fillNotionalAndCashImpact recomputes gate inputs from qty × fill price.
func fillNotionalAndCashImpact(side string, qty, fillPrice float64) (notional, cashImpact float64) {
	notional = qty * fillPrice
	switch side {
	case "buy":
		cashImpact = -notional
	case "sell":
		cashImpact = notional
	default:
		cashImpact = 0
	}
	return notional, cashImpact
}

func (r *Runner) agentURL(_ string) (string, error) {
	if r.Agents == nil {
		return "", fmt.Errorf("agents client is nil")
	}
	if strings.TrimSpace(r.Agents.RuntimeURL) == "" {
		return "", fmt.Errorf("agent runtime URL is empty")
	}
	return r.Agents.RuntimeURL, nil
}

func (r *Runner) loadAccount(ctx context.Context) (models.Account, error) {
	var account models.Account
	if err := r.DB.WithContext(ctx).First(&account).Error; err != nil {
		return models.Account{}, fmt.Errorf("load account: %w", err)
	}
	return account, nil
}

func (r *Runner) loadWatchlist(ctx context.Context) ([]string, error) {
	var rows []models.WatchlistSymbol
	if err := r.DB.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load watchlist: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Symbol)
	}
	return out, nil
}

func (r *Runner) loadCanHoldMap(ctx context.Context) (map[string]bool, error) {
	var rows []models.WatchlistSymbol
	if err := r.DB.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load can_hold map: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.Symbol] = row.CanHold
	}
	return out, nil
}

func (r *Runner) portfolioState(ctx context.Context, accountID uint, marks map[string]float64) (risk.PortfolioState, error) {
	if r.Broker != nil {
		return r.portfolioStateFromBroker(ctx, marks)
	}
	var account models.Account
	if err := r.DB.WithContext(ctx).First(&account, accountID).Error; err != nil {
		return risk.PortfolioState{}, err
	}
	var positions []models.Position
	if err := r.DB.WithContext(ctx).Where("account_id = ?", accountID).Find(&positions).Error; err != nil {
		return risk.PortfolioState{}, err
	}

	posMap := make(map[string]float64, len(positions))
	var equity float64 = account.Cash
	for _, p := range positions {
		posMap[p.Symbol] = p.Qty
		mark, ok := marks[p.Symbol]
		if !ok {
			return risk.PortfolioState{}, fmt.Errorf("%w: %s", ledger.ErrMissingMark, p.Symbol)
		}
		equity += p.Qty * mark
	}
	return risk.PortfolioState{
		Cash:      account.Cash,
		Equity:    equity,
		Positions: posMap,
		Marks:     marks,
	}, nil
}

func (r *Runner) portfolioStateFromBroker(ctx context.Context, marks map[string]float64) (risk.PortfolioState, error) {
	acct, err := r.Broker.GetAccount(ctx)
	if err != nil {
		return risk.PortfolioState{}, err
	}
	positions, err := r.Broker.ListPositions(ctx)
	if err != nil {
		return risk.PortfolioState{}, err
	}
	merged := copyMarks(marks)
	posMap := make(map[string]float64, len(positions))
	for _, p := range positions {
		posMap[p.Symbol] = p.Qty
		if p.CurrentPrice > 0 {
			merged[p.Symbol] = p.CurrentPrice
		}
	}
	equity := acct.Equity
	if equity == 0 && acct.PortfolioValue > 0 {
		equity = acct.PortfolioValue
	}
	return risk.PortfolioState{
		Cash:      acct.Cash,
		Equity:    equity,
		Positions: posMap,
		Marks:     merged,
	}, nil
}

func (r *Runner) mergeBrokerMarks(ctx context.Context, marks map[string]float64) map[string]float64 {
	out := copyMarks(marks)
	if r.Broker == nil {
		return out
	}
	positions, err := r.Broker.ListPositions(ctx)
	if err != nil {
		return out
	}
	for _, p := range positions {
		if p.CurrentPrice > 0 {
			out[p.Symbol] = p.CurrentPrice
		}
	}
	return out
}

func copyMarks(marks map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(marks))
	for k, v := range marks {
		out[k] = v
	}
	return out
}

func (r *Runner) persistStep(ctx context.Context, runID uint, step, status, payload string) error {
	row := models.WorkflowStepResult{
		RunID:       runID,
		Step:        step,
		Status:      status,
		PayloadJSON: payload,
	}
	if err := r.DB.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("persist step %s: %w", step, err)
	}
	return nil
}

func (r *Runner) updateStep(ctx context.Context, runID uint, step, status, payload string) error {
	res := r.DB.WithContext(ctx).Model(&models.WorkflowStepResult{}).
		Where("run_id = ? AND step = ?", runID, step).
		Updates(map[string]any{
			"status":       status,
			"payload_json": payload,
		})
	if res.Error != nil {
		return fmt.Errorf("update step %s: %w", step, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("update step %s: no row", step)
	}
	return nil
}

func (r *Runner) setRunStatus(ctx context.Context, run *models.WorkflowRun, status string) error {
	run.Status = status
	if err := r.DB.WithContext(ctx).Model(run).Update("status", status).Error; err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	return nil
}

func (r *Runner) failRun(ctx context.Context, run *models.WorkflowRun, cause error) error {
	run.Status = StatusFailed
	run.ErrorMsg = cause.Error()
	return r.DB.WithContext(ctx).Model(run).Updates(map[string]any{
		"status":    StatusFailed,
		"error_msg": cause.Error(),
	}).Error
}


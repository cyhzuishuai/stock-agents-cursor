package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/risk"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	// ErrInvalidPortfolioSchema is returned when portfolio agent output fails schema checks.
	ErrInvalidPortfolioSchema = errors.New("invalid portfolio_result schema")
)

// RunParams configures a single EOD workflow execution.
type RunParams struct {
	TradeDate     string
	Force         bool // retained for API compat; no longer blocks same-day sequential runs
	StrategyID    *uint
	Trigger       string // manual|pre_open|intraday|legacy_eod
	ExecutionMode string // empty → require_approval (or strategy if StrategyID set)
}

// LedgerAPI is the ledger surface used by the EOD runner.
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
}

type agentRunRequest struct {
	RunID            string                 `json:"run_id"`
	TradeDate        string                 `json:"trade_date"`
	Watchlist        []string               `json:"watchlist"`
	AccountSnapshot  ledger.AccountSnapshot `json:"account_snapshot"`
	PriorStepOutputs map[string]any         `json:"prior_step_outputs"`
}

type dataBar struct {
	Symbol string  `json:"symbol"`
	Close  float64 `json:"close"`
}

type dataResult struct {
	Bars []dataBar `json:"bars"`
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

func (r *Runner) RunEOD(ctx context.Context, params RunParams) (runID uint, err error) {
	unlock, err := AcquireEODLock(ctx, r.Redis, DefaultLockTTL)
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

	return run.ID, r.runEOD(ctx, &run, mode)
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

func (r *Runner) runEOD(ctx context.Context, run *models.WorkflowRun, executionMode string) error {
	marks, anyFill, err := r.runEODThroughFills(ctx, run, executionMode)
	if err != nil {
		if anyFill {
			_ = r.finalizeAfterPartialFill(ctx, run, err)
		} else if !isPostFillStatus(run.Status) {
			_ = r.failRun(ctx, run, err)
		}
		return err
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

func (r *Runner) runEODThroughFills(ctx context.Context, run *models.WorkflowRun, executionMode string) (map[string]float64, bool, error) {
	account, err := r.loadAccount(ctx)
	if err != nil {
		return nil, false, err
	}
	watchlist, err := r.loadWatchlist(ctx)
	if err != nil {
		return nil, false, err
	}
	snap, err := r.Ledger.AccountSnapshot(ctx, account.ID)
	if err != nil {
		return nil, false, fmt.Errorf("account snapshot: %w", err)
	}

	prior := map[string]any{}
	for _, step := range AgentChain() {
		if err := r.setRunStatus(ctx, run, step.Name); err != nil {
			return nil, false, err
		}
		baseURL, err := r.agentURL(step.Name)
		if err != nil {
			return nil, false, err
		}
		body := agentRunRequest{
			RunID:            strconv.FormatUint(uint64(run.ID), 10),
			TradeDate:        run.TradeDate,
			Watchlist:        watchlist,
			AccountSnapshot:  snap,
			PriorStepOutputs: prior,
		}
		raw, err := r.Agents.Call(ctx, baseURL, body, step.Timeout)
		if err != nil {
			_ = r.persistStep(ctx, run.ID, step.Name, StepStatusFailed, fmt.Sprintf(`{"error":%q}`, err.Error()))
			return nil, false, fmt.Errorf("agent %s: %w", step.Name, err)
		}
		if err := r.persistStep(ctx, run.ID, step.Name, StepStatusOK, string(raw)); err != nil {
			return nil, false, err
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, false, fmt.Errorf("decode %s payload: %w", step.Name, err)
		}
		prior[step.Name] = decoded
	}

	dataRaw, err := json.Marshal(prior[StepData])
	if err != nil {
		return nil, false, fmt.Errorf("marshal data result: %w", err)
	}
	var data dataResult
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return nil, false, fmt.Errorf("parse data bars: %w", err)
	}
	marks := marksFromBars(data.Bars)
	if len(marks) == 0 {
		return nil, false, fmt.Errorf("no marks from data bars")
	}

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

	marks = r.mergeBrokerMarks(ctx, marks)

	state, err := r.portfolioState(ctx, account.ID, marks)
	if err != nil {
		return nil, false, err
	}

	pendingApprovals := false
	anyFill := false
	for i := range proposals {
		p := &proposals[i]
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

// fillNotionalAndCashImpact recomputes gate inputs from qty × fill price (EOD close).
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

func (r *Runner) agentURL(step string) (string, error) {
	if r.Agents == nil {
		return "", fmt.Errorf("agents client is nil")
	}
	switch step {
	case StepData:
		return r.Agents.DataURL, nil
	case StepResearch:
		return r.Agents.ResearchURL, nil
	case StepDecision:
		return r.Agents.DecisionURL, nil
	case StepPortfolio:
		return r.Agents.PortfolioURL, nil
	case StepRisk:
		return r.Agents.RiskURL, nil
	default:
		return "", fmt.Errorf("unknown agent step %q", step)
	}
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

func (r *Runner) portfolioState(ctx context.Context, accountID uint, marks map[string]float64) (risk.PortfolioState, error) {
	if r.Broker != nil {
		if state, err := r.portfolioStateFromBroker(ctx, marks); err == nil {
			return state, nil
		}
		// Fall through to DB if broker account sync fails (unit tests / transient errors).
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

func marksFromBars(bars []dataBar) map[string]float64 {
	marks := make(map[string]float64, len(bars))
	for _, b := range bars {
		if b.Symbol == "" {
			continue
		}
		marks[b.Symbol] = b.Close
	}
	return marks
}

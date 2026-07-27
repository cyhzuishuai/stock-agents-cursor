package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/risk"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Runner struct {
	DB     *gorm.DB
	Agents *agentsclient.Client
	Ledger *ledger.Service
	Risk   risk.Engine
	Redis  redis.Cmdable
}

type agentRunRequest struct {
	RunID             string                 `json:"run_id"`
	TradeDate         string                 `json:"trade_date"`
	Watchlist         []string               `json:"watchlist"`
	AccountSnapshot   ledger.AccountSnapshot `json:"account_snapshot"`
	PriorStepOutputs  map[string]any         `json:"prior_step_outputs"`
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

func (r *Runner) RunEOD(ctx context.Context, tradeDate string) (runID uint, err error) {
	unlock, err := AcquireEODLock(ctx, r.Redis, tradeDate, DefaultLockTTL)
	if err != nil {
		return 0, err
	}
	defer unlock()

	run := models.WorkflowRun{
		TradeDate: tradeDate,
		Status:    StatusCreated,
	}
	if err := r.DB.WithContext(ctx).Create(&run).Error; err != nil {
		return 0, fmt.Errorf("create workflow run: %w", err)
	}

	if err := r.runEOD(ctx, &run); err != nil {
		_ = r.failRun(ctx, &run, err)
		return run.ID, err
	}
	return run.ID, nil
}

func (r *Runner) runEOD(ctx context.Context, run *models.WorkflowRun) error {
	account, err := r.loadAccount(ctx)
	if err != nil {
		return err
	}
	watchlist, err := r.loadWatchlist(ctx)
	if err != nil {
		return err
	}
	snap, err := r.Ledger.AccountSnapshot(ctx, account.ID)
	if err != nil {
		return fmt.Errorf("account snapshot: %w", err)
	}

	prior := map[string]any{}
	for _, step := range AgentChain() {
		if err := r.setRunStatus(ctx, run, step.Name); err != nil {
			return err
		}
		baseURL, err := r.agentURL(step.Name)
		if err != nil {
			return err
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
			return fmt.Errorf("agent %s: %w", step.Name, err)
		}
		if err := r.persistStep(ctx, run.ID, step.Name, StepStatusOK, string(raw)); err != nil {
			return err
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return fmt.Errorf("decode %s payload: %w", step.Name, err)
		}
		prior[step.Name] = decoded
	}

	dataRaw, err := json.Marshal(prior[StepData])
	if err != nil {
		return fmt.Errorf("marshal data result: %w", err)
	}
	var data dataResult
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return fmt.Errorf("parse data bars: %w", err)
	}
	marks := marksFromBars(data.Bars)
	if len(marks) == 0 {
		return fmt.Errorf("no marks from data bars")
	}

	portRaw, err := json.Marshal(prior[StepPortfolio])
	if err != nil {
		return fmt.Errorf("marshal portfolio result: %w", err)
	}
	var port portfolioResult
	if err := json.Unmarshal(portRaw, &port); err != nil {
		return fmt.Errorf("parse portfolio proposals: %w", err)
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
			return fmt.Errorf("insert proposals: %w", err)
		}
	}

	state, err := r.portfolioState(ctx, account.ID, marks)
	if err != nil {
		return err
	}

	pendingApprovals := false
	for i := range proposals {
		p := &proposals[i]
		fillPrice, ok := marks[p.Symbol]
		if !ok || fillPrice <= 0 {
			return fmt.Errorf("missing close for symbol %s", p.Symbol)
		}
		decision := r.Risk.Evaluate(state, risk.Proposal{
			Symbol:              p.Symbol,
			Side:                p.Side,
			Qty:                 p.Qty,
			EstimatedNotional:   p.EstimatedNotional,
			EstimatedCashImpact: p.EstimatedCashImpact,
			FillPrice:           fillPrice,
		})
		if decision.AutoExecute {
			runID := run.ID
			if _, err := r.Ledger.ApplyFill(ctx, ledger.FillRequest{
				AccountID:  account.ID,
				RunID:      &runID,
				Symbol:     p.Symbol,
				Side:       p.Side,
				Qty:        p.Qty,
				FillPrice:  fillPrice,
				TradeDate:  run.TradeDate,
				StopLoss:   p.StopLoss,
				TakeProfit: p.TakeProfit,
			}); err != nil {
				return fmt.Errorf("apply fill %s: %w", p.Symbol, err)
			}
			p.Status = ProposalFilled
			if err := r.DB.WithContext(ctx).Model(p).Update("status", ProposalFilled).Error; err != nil {
				return err
			}
			// Refresh state for subsequent proposals in the same run.
			state, err = r.portfolioState(ctx, account.ID, marks)
			if err != nil {
				return err
			}
			continue
		}

		reasons, err := json.Marshal(decision.BreachReasons)
		if err != nil {
			return err
		}
		approval := models.Approval{
			ProposalID:        p.ID,
			Status:            ApprovalPending,
			BreachReasonsJSON: string(reasons),
		}
		if err := r.DB.WithContext(ctx).Create(&approval).Error; err != nil {
			return fmt.Errorf("create approval: %w", err)
		}
		p.Status = ProposalAwaitingApproval
		if err := r.DB.WithContext(ctx).Model(p).Update("status", ProposalAwaitingApproval).Error; err != nil {
			return err
		}
		pendingApprovals = true
	}

	finalStatus := StatusExecuted
	if pendingApprovals {
		finalStatus = StatusAwaitingApproval
	}
	if err := r.setRunStatus(ctx, run, finalStatus); err != nil {
		return err
	}

	if _, err := r.Ledger.UpsertNAV(ctx, run.TradeDate, marks); err != nil {
		return fmt.Errorf("upsert nav: %w", err)
	}
	return nil
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

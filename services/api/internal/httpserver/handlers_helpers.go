package httpserver

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// EODRunner triggers an end-of-day workflow run.
type EODRunner interface {
	RunEOD(ctx context.Context, params workflow.RunParams) (uint, error)
}

// SchedulerReloader hot-reloads scheduler jobs from the active strategy.
type SchedulerReloader interface {
	Reload(ctx context.Context) error
}

// NoopSchedulerReloader satisfies SchedulerReloader without side effects.
type NoopSchedulerReloader struct{}

func (NoopSchedulerReloader) Reload(context.Context) error { return nil }

// RouterDeps are dependencies for NewRouter.
type RouterDeps struct {
	DB         *gorm.DB
	JWTSecret  string
	Runner     EODRunner
	Approvals  *approvals.Service
	Ledger     *ledger.Service
	Config     *config.Config
	Strategies *strategy.Service
	Scheduler  SchedulerReloader
	HTTPClient *http.Client
}

// API holds shared handler dependencies.
type API struct {
	DB         *gorm.DB
	JWTSecret  string
	Runner     EODRunner
	Approvals  *approvals.Service
	Ledger     *ledger.Service
	Config     *config.Config
	Strategies *strategy.Service
	Scheduler  SchedulerReloader
	HTTPClient *http.Client
}

func (h *API) loadAccount(c *gin.Context) (models.Account, error) {
	var account models.Account
	err := h.DB.WithContext(c.Request.Context()).First(&account).Error
	return account, err
}

func (h *API) latestMarks(ctx context.Context) map[string]float64 {
	marks := map[string]float64{}
	var run models.WorkflowRun
	if err := h.DB.WithContext(ctx).Order("id DESC").First(&run).Error; err != nil {
		return marks
	}
	var step models.WorkflowStepResult
	if err := h.DB.WithContext(ctx).
		Where("run_id = ? AND step = ?", run.ID, workflow.StepData).
		First(&step).Error; err != nil {
		return marks
	}
	var data struct {
		Bars []struct {
			Symbol string  `json:"symbol"`
			Close  float64 `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal([]byte(step.PayloadJSON), &data); err != nil {
		return marks
	}
	for _, b := range data.Bars {
		if b.Symbol != "" {
			marks[b.Symbol] = b.Close
		}
	}
	return marks
}

func markOrCost(marks map[string]float64, p models.Position) float64 {
	if m, ok := marks[p.Symbol]; ok && m > 0 {
		return m
	}
	return p.AvgCost
}

func (h *API) triggerEOD(c *gin.Context, tradeDate string, force bool) {
	if h.Runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "eod runner not configured"})
		return
	}
	params := workflow.RunParams{
		TradeDate: tradeDate,
		Force:     force,
		Trigger:   workflow.TriggerManual,
	}
	if h.Strategies != nil {
		st, err := h.Strategies.Active(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if st != nil {
			params.StrategyID = &st.ID
			// ExecutionMode left empty so runner resolves from strategy row.
		}
	}
	runID, err := h.Runner.RunEOD(c.Request.Context(), params)
	if err != nil {
		resp := gin.H{"error": err.Error()}
		if runID != 0 {
			resp["run_id"] = runID
		}
		c.JSON(http.StatusInternalServerError, resp)
		return
	}
	c.JSON(http.StatusOK, gin.H{"run_id": runID})
}

package httpserver

import (
	"context"
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/stream"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// EODRunner triggers a strategy workflow run (manual or scheduled). Name is historical.
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
	Broker     broker.Client
	Stream     *stream.Hub
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
	Broker     broker.Client
	Stream     *stream.Hub
}

func (h *API) requireBroker(c *gin.Context) bool {
	if h.Broker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alpaca not configured"})
		return false
	}
	return true
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

package httpserver

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/stream"
	"github.com/gin-gonic/gin"
)

// Overridable in tests; production default matches the Task 10 brief.
var sseHeartbeatInterval = 15 * time.Second

// SetSSEHeartbeatIntervalForTest swaps the SSE heartbeat interval; returns the previous value.
func SetSSEHeartbeatIntervalForTest(d time.Duration) time.Duration {
	prev := sseHeartbeatInterval
	sseHeartbeatInterval = d
	return prev
}

// StreamMarket serves GET /api/v1/stream/market as Server-Sent Events.
func (h *API) StreamMarket(c *gin.Context) {
	h.serveSSE(c, "quote", stream.ChannelMarket)
}

// StreamAccount serves GET /api/v1/stream/account as Server-Sent Events.
func (h *API) StreamAccount(c *gin.Context) {
	h.serveSSE(c, "account", stream.ChannelAccount)
}

func (h *API) serveSSE(c *gin.Context, eventName string, kind stream.ChannelKind) {
	if h.Stream == nil || !h.Stream.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "streaming disabled"})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher.Flush()

	ch := make(chan []byte, 32)
	unsub := h.Stream.Subscribe(kind, ch)
	defer unsub()

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventName, msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

package stream

import (
	"context"
	"strings"
	"sync"
	"time"
)

const defaultQuoteThrottle = time.Second

// Hub fans out quote payloads to SSE subscribers with per-symbol throttling.
// Upstream Alpaca WS connections are optional and wired by later tasks;
// PublishQuote is the injection point for both real pumps and tests.
type Hub struct {
	enabled  bool
	throttle time.Duration
	now      func() time.Time

	mu          sync.Mutex
	subs        map[chan []byte]struct{}
	lastPublish map[string]time.Time
}

// NewHub creates a stream hub. Enabled() is true only when streaming is
// configured on and both Alpaca credentials are present.
func NewHub(streamEnabled bool, apiKey, apiSecret string) *Hub {
	enabled := streamEnabled &&
		strings.TrimSpace(apiKey) != "" &&
		strings.TrimSpace(apiSecret) != ""

	return &Hub{
		enabled:     enabled,
		throttle:    defaultQuoteThrottle,
		now:         time.Now,
		subs:        make(map[chan []byte]struct{}),
		lastPublish: make(map[string]time.Time),
	}
}

// Enabled reports whether the hub should accept SSE clients.
func (h *Hub) Enabled() bool {
	return h != nil && h.enabled
}

// Start prepares the hub for serving SSE clients. Upstream Alpaca WebSocket
// pumps are optional and may be wired later; PublishQuote remains the
// injection point for tests and future pumps.
func (h *Hub) Start(ctx context.Context) error {
	if !h.Enabled() {
		return nil
	}
	_ = ctx
	return nil
}

// Subscribe registers ch for fan-out. The returned unsubscribe removes it.
// Callers should use a buffered channel; slow consumers drop updates.
func (h *Hub) Subscribe(ch chan []byte) (unsubscribe func()) {
	if h == nil || ch == nil {
		return func() {}
	}

	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
		})
	}
}

// PublishQuote fans out payload to subscribers, throttled to at most one
// update per symbol per throttle interval (≥1s by default).
func (h *Hub) PublishQuote(symbol string, payload []byte) {
	if h == nil || !h.enabled {
		return
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || len(payload) == 0 {
		return
	}

	now := h.now()

	h.mu.Lock()
	defer h.mu.Unlock()

	if last, ok := h.lastPublish[symbol]; ok && now.Sub(last) < h.throttle {
		return
	}
	h.lastPublish[symbol] = now

	msg := append([]byte(nil), payload...)
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
			// Drop when subscriber buffer is full.
		}
	}
}

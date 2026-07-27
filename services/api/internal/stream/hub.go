package stream

import (
	"context"
	"strings"
	"sync"
	"time"
)

const defaultQuoteThrottle = time.Second

// ChannelKind selects which fan-out set a subscriber joins.
type ChannelKind string

const (
	ChannelMarket  ChannelKind = "market"
	ChannelAccount ChannelKind = "account"
)

// Hub fans out payloads to SSE subscribers. Quotes are throttled per symbol;
// account events are unthrottled. Market and account subscribers are separate
// so quote traffic is not labeled as account (and vice versa).
type Hub struct {
	enabled  bool
	throttle time.Duration
	now      func() time.Time

	mu          sync.Mutex
	marketSubs  map[chan []byte]struct{}
	accountSubs map[chan []byte]struct{}
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
		marketSubs:  make(map[chan []byte]struct{}),
		accountSubs: make(map[chan []byte]struct{}),
		lastPublish: make(map[string]time.Time),
	}
}

// Enabled reports whether the hub should accept SSE clients.
func (h *Hub) Enabled() bool {
	return h != nil && h.enabled
}

// Start prepares the hub for serving SSE clients. Upstream Alpaca WebSocket
// pumps are optional and may be wired later; PublishQuote / PublishAccount
// remain the injection points for tests and future pumps.
func (h *Hub) Start(ctx context.Context) error {
	if !h.Enabled() {
		return nil
	}
	_ = ctx
	return nil
}

// Subscribe registers ch on the given channel kind. The returned unsubscribe
// removes it. Callers should use a buffered channel; slow consumers drop updates.
func (h *Hub) Subscribe(kind ChannelKind, ch chan []byte) (unsubscribe func()) {
	if h == nil || ch == nil {
		return func() {}
	}

	h.mu.Lock()
	subs := h.subsFor(kind)
	if subs != nil {
		subs[ch] = struct{}{}
	}
	h.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			if s := h.subsFor(kind); s != nil {
				delete(s, ch)
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) subsFor(kind ChannelKind) map[chan []byte]struct{} {
	switch kind {
	case ChannelMarket:
		return h.marketSubs
	case ChannelAccount:
		return h.accountSubs
	default:
		return nil
	}
}

// PublishQuote fans out payload to market subscribers, throttled to at most
// one update per symbol per throttle interval (≥1s by default).
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

	fanOut(h.marketSubs, payload)
}

// PublishAccount fans out payload to account subscribers (trade updates, etc.).
func (h *Hub) PublishAccount(payload []byte) {
	if h == nil || !h.enabled || len(payload) == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	fanOut(h.accountSubs, payload)
}

func fanOut(subs map[chan []byte]struct{}, payload []byte) {
	msg := append([]byte(nil), payload...)
	for ch := range subs {
		select {
		case ch <- msg:
		default:
			// Drop when subscriber buffer is full.
		}
	}
}

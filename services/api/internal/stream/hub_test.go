package stream

import (
	"testing"
	"time"
)

func TestHubEnabledRequiresFlagAndCreds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		enabled bool
		key     string
		secret  string
		want    bool
	}{
		{name: "all set", enabled: true, key: "k", secret: "s", want: true},
		{name: "flag off", enabled: false, key: "k", secret: "s", want: false},
		{name: "no key", enabled: true, key: "", secret: "s", want: false},
		{name: "no secret", enabled: true, key: "k", secret: "", want: false},
		{name: "whitespace creds", enabled: true, key: "  ", secret: "s", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := NewHub(tc.enabled, tc.key, tc.secret)
			if got := h.Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}

	if (*Hub)(nil).Enabled() {
		t.Fatal("nil Hub should not be enabled")
	}
}

func TestHubFakePumpThrottlesPerSymbol(t *testing.T) {
	t.Parallel()

	h := NewHub(true, "key", "secret")
	h.throttle = 100 * time.Millisecond

	var clock time.Time
	h.now = func() time.Time { return clock }

	ch := make(chan []byte, 8)
	unsub := h.Subscribe(ChannelMarket, ch)
	defer unsub()

	// Fake upstream pump injects rapid quotes.
	pump := func(symbol string, payload string) {
		h.PublishQuote(symbol, []byte(payload))
	}

	clock = time.Unix(0, 0)
	pump("AAPL", `{"symbol":"AAPL","p":1}`)
	pump("AAPL", `{"symbol":"AAPL","p":2}`) // throttled
	pump("MSFT", `{"symbol":"MSFT","p":3}`) // other symbol ok

	got := drain(ch)
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2 (AAPL + MSFT): %v", len(got), got)
	}
	if string(got[0]) != `{"symbol":"AAPL","p":1}` {
		t.Fatalf("first message = %s", got[0])
	}
	if string(got[1]) != `{"symbol":"MSFT","p":3}` {
		t.Fatalf("second message = %s", got[1])
	}

	clock = clock.Add(99 * time.Millisecond)
	pump("AAPL", `{"symbol":"AAPL","p":4}`) // still within throttle
	if extra := drain(ch); len(extra) != 0 {
		t.Fatalf("unexpected messages before throttle elapsed: %v", extra)
	}

	clock = clock.Add(time.Millisecond)
	pump("AAPL", `{"symbol":"AAPL","p":5}`)
	got = drain(ch)
	if len(got) != 1 || string(got[0]) != `{"symbol":"AAPL","p":5}` {
		t.Fatalf("after throttle: got %v", got)
	}
}

func TestHubPublishNoopWhenDisabled(t *testing.T) {
	t.Parallel()

	h := NewHub(false, "key", "secret")
	ch := make(chan []byte, 2)
	unsub := h.Subscribe(ChannelMarket, ch)
	defer unsub()

	h.PublishQuote("AAPL", []byte(`{"p":1}`))
	if got := drain(ch); len(got) != 0 {
		t.Fatalf("disabled hub should not publish, got %v", got)
	}
}

func TestHubUnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()

	h := NewHub(true, "key", "secret")
	ch := make(chan []byte, 2)
	unsub := h.Subscribe(ChannelMarket, ch)
	unsub()

	h.PublishQuote("AAPL", []byte(`{"p":1}`))
	if got := drain(ch); len(got) != 0 {
		t.Fatalf("unsubscribed channel should not receive, got %v", got)
	}
}

func TestHubMarketAndAccountChannelsIsolated(t *testing.T) {
	t.Parallel()

	h := NewHub(true, "key", "secret")
	market := make(chan []byte, 2)
	account := make(chan []byte, 2)
	unsubM := h.Subscribe(ChannelMarket, market)
	unsubA := h.Subscribe(ChannelAccount, account)
	defer unsubM()
	defer unsubA()

	h.PublishQuote("AAPL", []byte(`{"symbol":"AAPL","p":1}`))
	h.PublishAccount([]byte(`{"event":"trade_update"}`))

	gotM := drain(market)
	gotA := drain(account)
	if len(gotM) != 1 || string(gotM[0]) != `{"symbol":"AAPL","p":1}` {
		t.Fatalf("market got %v", gotM)
	}
	if len(gotA) != 1 || string(gotA[0]) != `{"event":"trade_update"}` {
		t.Fatalf("account got %v", gotA)
	}
}

func drain(ch <-chan []byte) [][]byte {
	var out [][]byte
	for {
		select {
		case msg := <-ch:
			out = append(out, msg)
		default:
			return out
		}
	}
}

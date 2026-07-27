package symbolsearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/config"
)

func TestMatchAssetsPrefersPrefixThenName(t *testing.T) {
	assets := []Asset{
		{Symbol: "MSFT", Name: "Microsoft Corporation"},
		{Symbol: "AAP", Name: "Advance Auto Parts"},
		{Symbol: "AAPL", Name: "Apple Inc."},
		{Symbol: "AAAA", Name: "Amplius Aggressive Asset Allocation ETF"},
		{Symbol: "XYZ", Name: "Contains AAPL somewhere"},
	}
	got := matchAssets(assets, "aap", 10)
	if len(got) < 2 {
		t.Fatalf("expected prefix hits, got %+v", got)
	}
	if got[0].Symbol != "AAP" {
		t.Fatalf("shortest prefix first: got %s", got[0].Symbol)
	}
	if got[1].Symbol != "AAPL" {
		t.Fatalf("next prefix: got %s", got[1].Symbol)
	}
	foundName := false
	for _, a := range got {
		if a.Symbol == "XYZ" {
			foundName = true
		}
	}
	if !foundName {
		t.Fatalf("expected name match XYZ, got %+v", got)
	}
}

func TestMatchAssetsCapsLimit(t *testing.T) {
	assets := make([]Asset, 20)
	for i := range assets {
		assets[i] = Asset{Symbol: "A" + strings.Repeat("X", i), Name: "n"}
	}
	got := matchAssets(assets, "A", 10)
	if len(got) != 10 {
		t.Fatalf("len=%d want 10", len(got))
	}
}

func TestNewFromConfigNilWithoutKeys(t *testing.T) {
	if NewFromConfig(&config.Config{}, nil) != nil {
		t.Fatal("expected nil without keys")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSearchEnrichesSnapshots(t *testing.T) {
	assetsJSON := `[{"symbol":"AAAA","name":"Amplius ETF","class":"us_equity","status":"active"}]`
	snapsJSON := `{"AAAA":{"latestTrade":{"p":29.86},"prevDailyBar":{"c":29.866}}}`

	client := &Client{
		TradeBaseURL: "https://paper.test",
		DataBaseURL:  "https://data.test",
		Key:          "k",
		Secret:       "s",
		TTL:          time.Hour,
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body string
			switch {
			case strings.Contains(req.URL.Path, "/v2/assets"):
				body = assetsJSON
			case strings.Contains(req.URL.Path, "/v2/stocks/snapshots"):
				body = snapsJSON
			default:
				t.Fatalf("unexpected url %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	results, err := client.Search(context.Background(), "AAAA")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len=%d", len(results))
	}
	r := results[0]
	if r.Symbol != "AAAA" || r.Name != "Amplius ETF" {
		t.Fatalf("identity: %+v", r)
	}
	if r.Price == nil || *r.Price != 29.86 {
		t.Fatalf("price: %+v", r.Price)
	}
	if r.Change == nil || r.ChangePct == nil {
		t.Fatalf("change fields missing: %+v", r)
	}
}

func TestSearchReturnsWithoutQuotesOnSnapshotFailure(t *testing.T) {
	assetsJSON := `[{"symbol":"AAPL","name":"Apple Inc.","class":"us_equity","status":"active"}]`
	client := &Client{
		TradeBaseURL: "https://paper.test",
		DataBaseURL:  "https://data.test",
		Key:          "k",
		Secret:       "s",
		TTL:          time.Hour,
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/v2/assets") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(assetsJSON)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("boom")),
				Header:     make(http.Header),
			}, nil
		})},
	}

	results, err := client.Search(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Symbol != "AAPL" {
		t.Fatalf("got %+v", results)
	}
	if results[0].Price != nil {
		t.Fatalf("expected nil price on snapshot failure")
	}
}

func TestSearchAssetFetchFailure(t *testing.T) {
	client := &Client{
		TradeBaseURL: "https://paper.test",
		DataBaseURL:  "https://data.test",
		Key:          "k",
		Secret:       "s",
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		})},
	}
	_, err := client.Search(context.Background(), "AAPL")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplySnapshotJSONRoundTrip(t *testing.T) {
	r := Result{Symbol: "X", Name: "Y"}
	raw := []byte(`{"latestTrade":{"p":10},"prevDailyBar":{"c":8}}`)
	var snap alpacaSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	applySnapshot(&r, snap)
	if r.Price == nil || *r.Price != 10 {
		t.Fatalf("price %+v", r.Price)
	}
	if r.Change == nil || *r.Change != 2 {
		t.Fatalf("change %+v", r.Change)
	}
}

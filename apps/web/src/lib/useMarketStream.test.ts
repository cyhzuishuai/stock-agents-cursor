// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyQuoteToOverview,
  applyQuoteToPortfolio,
  parseQuotePayload,
  reconnectBackoffMs,
  shouldStopMarketStreamQuietly,
  useMarketStream,
} from "./useMarketStream";
import type { OverviewResponse, PortfolioResponse } from "./types";

describe("shouldStopMarketStreamQuietly", () => {
  it("stops on auth failure and stream disabled", () => {
    expect(shouldStopMarketStreamQuietly(401)).toBe(true);
    expect(shouldStopMarketStreamQuietly(403)).toBe(true);
    expect(shouldStopMarketStreamQuietly(503)).toBe(true);
  });

  it("allows reconnect for other HTTP errors", () => {
    expect(shouldStopMarketStreamQuietly(500)).toBe(false);
    expect(shouldStopMarketStreamQuietly(502)).toBe(false);
    expect(shouldStopMarketStreamQuietly(404)).toBe(false);
  });
});

describe("reconnectBackoffMs", () => {
  it("doubles from base until max", () => {
    expect(reconnectBackoffMs(0, { baseMs: 1000, maxMs: 30_000 })).toBe(1_000);
    expect(reconnectBackoffMs(1, { baseMs: 1000, maxMs: 30_000 })).toBe(2_000);
    expect(reconnectBackoffMs(2, { baseMs: 1000, maxMs: 30_000 })).toBe(4_000);
    expect(reconnectBackoffMs(3, { baseMs: 1000, maxMs: 30_000 })).toBe(8_000);
    expect(reconnectBackoffMs(4, { baseMs: 1000, maxMs: 30_000 })).toBe(16_000);
    expect(reconnectBackoffMs(5, { baseMs: 1000, maxMs: 30_000 })).toBe(30_000);
    expect(reconnectBackoffMs(10, { baseMs: 1000, maxMs: 30_000 })).toBe(30_000);
  });

  it("floors negative attempts to zero", () => {
    expect(reconnectBackoffMs(-3, { baseMs: 500, maxMs: 8_000 })).toBe(500);
  });
});

describe("parseQuotePayload", () => {
  it("accepts symbol + p", () => {
    expect(parseQuotePayload(`{"symbol":"AAPL","p":1.23}`)).toEqual({
      symbol: "AAPL",
      price: 1.23,
    });
  });

  it("accepts symbol + price", () => {
    expect(parseQuotePayload(`{"symbol":"MSFT","price":400}`)).toEqual({
      symbol: "MSFT",
      price: 400,
    });
  });

  it("rejects invalid payloads", () => {
    expect(parseQuotePayload(`{}`)).toBeNull();
    expect(parseQuotePayload(`not-json`)).toBeNull();
    expect(parseQuotePayload(`{"symbol":"AAPL","p":-1}`)).toBeNull();
  });
});

describe("applyQuote merges", () => {
  it("updates overview market_value, equity, nav, weights", () => {
    const data: OverviewResponse = {
      cash: 1000,
      equity: 200,
      nav: 1200,
      pending_approvals_count: 0,
      latest_run: null,
      positions_summary: [
        { symbol: "AAPL", qty: 2, market_value: 200, weight: 200 / 1200 },
        { symbol: "MSFT", qty: 1, market_value: 0, weight: 0 },
      ],
      nav_series: [],
    };
    const next = applyQuoteToOverview(data, { symbol: "AAPL", price: 150 });
    expect(next.positions_summary[0].market_value).toBe(300);
    expect(next.equity).toBe(300);
    expect(next.nav).toBe(1300);
    expect(next.positions_summary[0].weight).toBeCloseTo(300 / 1300);
  });

  it("updates portfolio market_price, pnl, weights", () => {
    const data: PortfolioResponse = {
      cash: 500,
      positions: [
        {
          symbol: "AAPL",
          qty: 10,
          avg_cost: 100,
          stop_loss: null,
          take_profit: null,
          market_price: 100,
          unrealized_pnl: 0,
          weight: 0.5,
        },
      ],
    };
    const next = applyQuoteToPortfolio(data, { symbol: "AAPL", price: 110 });
    expect(next.positions[0].market_price).toBe(110);
    expect(next.positions[0].unrealized_pnl).toBe(100);
    expect(next.positions[0].weight).toBeCloseTo(1100 / 1600);
  });
});

describe("useMarketStream", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 500 })),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it.each([401, 403, 503])(
    "does not reconnect after HTTP %i",
    async (status) => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status }));
      const onQuote = vi.fn();

      renderHook(() => useMarketStream(true, onQuote));
      await act(async () => {
        await Promise.resolve();
      });
      expect(fetch).toHaveBeenCalledTimes(1);

      act(() => {
        vi.advanceTimersByTime(60_000);
      });
      expect(fetch).toHaveBeenCalledTimes(1);
      expect(onQuote).not.toHaveBeenCalled();
    },
  );

  it("reconnects after transient HTTP errors", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 500 }));
    const onQuote = vi.fn();

    renderHook(() => useMarketStream(true, onQuote));
    await act(async () => {
      await Promise.resolve();
    });
    expect(fetch).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});

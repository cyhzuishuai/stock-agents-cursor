"use client";

import { useEffect, useRef } from "react";
import { getApiBaseUrl, getToken } from "./api";
import type { OverviewResponse, PortfolioResponse } from "./types";

export type QuoteUpdate = {
  symbol: string;
  price: number;
};

const DEFAULT_BASE_MS = 1_000;
const DEFAULT_MAX_MS = 30_000;

/** Exponential backoff delay for SSE reconnect attempts (pure). */
export function reconnectBackoffMs(
  attempt: number,
  opts?: { baseMs?: number; maxMs?: number },
): number {
  const baseMs = opts?.baseMs ?? DEFAULT_BASE_MS;
  const maxMs = opts?.maxMs ?? DEFAULT_MAX_MS;
  const n = Math.max(0, Math.floor(attempt));
  const delay = baseMs * 2 ** n;
  return Math.min(maxMs, delay);
}

/** Parse hub quote JSON (`symbol` + `p` or `price`). */
export function parseQuotePayload(raw: string): QuoteUpdate | null {
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const symbol =
      typeof parsed.symbol === "string" ? parsed.symbol.trim() : "";
    const priceRaw =
      typeof parsed.p === "number"
        ? parsed.p
        : typeof parsed.price === "number"
          ? parsed.price
          : NaN;
    if (!symbol || !Number.isFinite(priceRaw) || priceRaw < 0) {
      return null;
    }
    return { symbol, price: priceRaw };
  } catch {
    return null;
  }
}

export function applyQuoteToOverview(
  data: OverviewResponse,
  quote: QuoteUpdate,
): OverviewResponse {
  const positions_summary = data.positions_summary.map((row) => {
    if (row.symbol !== quote.symbol) return row;
    return { ...row, market_value: row.qty * quote.price };
  });
  const equity = positions_summary.reduce((sum, row) => sum + row.market_value, 0);
  const nav = data.cash + equity;
  return {
    ...data,
    equity,
    nav,
    positions_summary: positions_summary.map((row) => ({
      ...row,
      weight: nav > 0 ? row.market_value / nav : 0,
    })),
  };
}

export function applyQuoteToPortfolio(
  data: PortfolioResponse,
  quote: QuoteUpdate,
): PortfolioResponse {
  const positions = data.positions.map((row) => {
    if (row.symbol !== quote.symbol) return row;
    const market_price = quote.price;
    const unrealized_pnl = (market_price - row.avg_cost) * row.qty;
    return { ...row, market_price, unrealized_pnl };
  });
  const equity = positions.reduce((sum, row) => sum + row.qty * row.market_price, 0);
  const nav = data.cash + equity;
  return {
    ...data,
    positions: positions.map((row) => ({
      ...row,
      weight: nav > 0 ? (row.qty * row.market_price) / nav : 0,
    })),
  };
}

type SseFrame = { event: string; data: string };

function parseSseChunk(
  buffer: string,
): { frames: SseFrame[]; rest: string } {
  const frames: SseFrame[] = [];
  const parts = buffer.split("\n\n");
  const rest = parts.pop() ?? "";
  for (const block of parts) {
    let event = "message";
    const dataLines: string[] = [];
    for (const rawLine of block.split("\n")) {
      const line = rawLine.replace(/\r$/, "");
      if (!line || line.startsWith(":")) continue;
      if (line.startsWith("event:")) {
        event = line.slice(6).trim();
      } else if (line.startsWith("data:")) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
    if (dataLines.length > 0) {
      frames.push({ event, data: dataLines.join("\n") });
    }
  }
  return { frames, rest };
}

/**
 * Authenticated market quote SSE via fetch + ReadableStream (not EventSource).
 * On 503/disabled, stops quietly so tiered REST polling remains the source of truth.
 * Transient failures reconnect with exponential backoff.
 */
export function useMarketStream(
  enabled: boolean,
  onQuote: (quote: QuoteUpdate) => void,
): void {
  const onQuoteRef = useRef(onQuote);
  onQuoteRef.current = onQuote;

  useEffect(() => {
    if (!enabled) return;

    const ac = new AbortController();
    let attempt = 0;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const clearTimer = () => {
      if (timer != null) {
        clearTimeout(timer);
        timer = null;
      }
    };

    const scheduleReconnect = () => {
      if (ac.signal.aborted) return;
      const delay = reconnectBackoffMs(attempt);
      attempt += 1;
      timer = setTimeout(() => {
        void run();
      }, delay);
    };

    const run = async () => {
      if (ac.signal.aborted) return;

      const token = getToken();
      const headers: Record<string, string> = {
        Accept: "text/event-stream",
      };
      if (token) {
        headers.Authorization = `Bearer ${token}`;
      }

      const url = `${getApiBaseUrl().replace(/\/$/, "")}/api/v1/stream/market`;

      let res: Response;
      try {
        res = await fetch(url, {
          method: "GET",
          headers,
          signal: ac.signal,
          cache: "no-store",
        });
      } catch {
        if (ac.signal.aborted) return;
        scheduleReconnect();
        return;
      }

      // Stream disabled / unavailable — silent fallback to REST polling only.
      if (res.status === 503) {
        return;
      }

      if (!res.ok || !res.body) {
        scheduleReconnect();
        return;
      }

      attempt = 0;
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      try {
        while (!ac.signal.aborted) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const parsed = parseSseChunk(buffer);
          buffer = parsed.rest;
          for (const frame of parsed.frames) {
            if (frame.event !== "quote") continue;
            const quote = parseQuotePayload(frame.data);
            if (quote) {
              onQuoteRef.current(quote);
            }
          }
        }
      } catch {
        // aborted or network drop
      } finally {
        try {
          reader.releaseLock();
        } catch {
          // ignore
        }
      }

      if (!ac.signal.aborted) {
        scheduleReconnect();
      }
    };

    void run();

    return () => {
      ac.abort();
      clearTimer();
    };
  }, [enabled]);
}

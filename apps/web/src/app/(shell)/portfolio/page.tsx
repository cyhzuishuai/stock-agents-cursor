"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { PortfolioPosition, PortfolioResponse } from "@/lib/types";

const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const currencyWhole = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});

const percent = new Intl.NumberFormat("en-US", {
  style: "percent",
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});

function formatStop(value: number | null): string {
  return value == null ? "—" : currency.format(value);
}

function pnlClass(pnl: number): string {
  if (pnl > 0) return "portfolio__pnl--positive";
  if (pnl < 0) return "portfolio__pnl--negative";
  return "";
}

export default function PortfolioPage() {
  const [data, setData] = useState<PortfolioResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<PortfolioResponse>("/api/v1/portfolio");
        if (!cancelled) {
          setData(response);
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load portfolio",
          );
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  if (loading) {
    return <p>Loading portfolio…</p>;
  }

  if (error) {
    return <p role="alert">{error}</p>;
  }

  if (!data) {
    return <p role="alert">Portfolio data unavailable</p>;
  }

  const equity = data.positions.reduce(
    (sum, p) => sum + p.qty * p.market_price,
    0,
  );
  const nav = data.cash + equity;

  return (
    <div className="portfolio">
      <h1 className="portfolio__title">Portfolio</h1>

      <section className="portfolio__stats" aria-label="Account summary">
        <div className="portfolio__stat">
          <span className="portfolio__stat-label">Cash</span>
          <span className="portfolio__stat-value">
            {currencyWhole.format(data.cash)}
          </span>
        </div>
        <div className="portfolio__stat">
          <span className="portfolio__stat-label">Equity</span>
          <span className="portfolio__stat-value">
            {currencyWhole.format(equity)}
          </span>
        </div>
        <div className="portfolio__stat">
          <span className="portfolio__stat-label">NAV</span>
          <span className="portfolio__stat-value">
            {currencyWhole.format(nav)}
          </span>
        </div>
      </section>

      <section className="portfolio__panel">
        <h2 className="portfolio__panel-title">Positions</h2>
        {data.positions.length === 0 ? (
          <p>No open positions</p>
        ) : (
          <table className="portfolio__table">
            <thead>
              <tr>
                <th scope="col">Symbol</th>
                <th scope="col">Qty</th>
                <th scope="col">Avg cost</th>
                <th scope="col">Market</th>
                <th scope="col">Weight</th>
                <th scope="col">Unrealized P&amp;L</th>
                <th scope="col">Stop loss</th>
                <th scope="col">Take profit</th>
              </tr>
            </thead>
            <tbody>
              {data.positions.map((row: PortfolioPosition) => (
                <tr key={row.symbol}>
                  <td>{row.symbol}</td>
                  <td>{row.qty}</td>
                  <td>{currency.format(row.avg_cost)}</td>
                  <td>{currency.format(row.market_price)}</td>
                  <td>{percent.format(row.weight)}</td>
                  <td className={pnlClass(row.unrealized_pnl)}>
                    {currency.format(row.unrealized_pnl)}
                  </td>
                  <td>{formatStop(row.stop_loss)}</td>
                  <td>{formatStop(row.take_profit)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

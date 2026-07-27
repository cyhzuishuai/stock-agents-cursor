"use client";

import Link from "next/link";
import { NavHistoryPanel } from "@/components/NavHistoryPanel";
import { RunStatusBadge } from "@/components/RunStatusBadge";
import { UsMarketClock } from "@/components/UsMarketClock";
import type { OverviewResponse } from "@/lib/types";

const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});

const percent = new Intl.NumberFormat("en-US", {
  style: "percent",
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});

export function OverviewDashboard({
  data,
  onRefresh,
  refreshing = false,
}: {
  data: OverviewResponse;
  onRefresh?: () => void;
  refreshing?: boolean;
}) {
  return (
    <div className="overview">
      <header className="page-header">
        <p className="page-header__eyebrow">Trading desk</p>
        <div className="runs__header">
          <h1 className="page-header__title">Overview</h1>
          {onRefresh ? (
            <div className="runs__actions">
              <button
                type="button"
                className="btn"
                onClick={onRefresh}
                disabled={refreshing}
              >
                {refreshing ? "Refreshing…" : "Refresh"}
              </button>
            </div>
          ) : null}
        </div>
      </header>

      <UsMarketClock />

      <section className="stat-grid" aria-label="Account summary">
        <div className="stat stat--emphasis">
          <span className="stat__label">NAV</span>
          <span className="stat__value">{currency.format(data.nav)}</span>
        </div>
        <div className="stat">
          <span className="stat__label">Equity</span>
          <span className="stat__value">{currency.format(data.equity)}</span>
        </div>
        <div className="stat">
          <span className="stat__label">Cash</span>
          <span className="stat__value">{currency.format(data.cash)}</span>
        </div>
      </section>

      <section
        className={
          data.pending_approvals_count > 0
            ? "pending-strip"
            : "pending-strip pending-strip--clear"
        }
        aria-label="Pending approvals"
      >
        {data.pending_approvals_count === 0 ? (
          <p className="empty-state">None pending</p>
        ) : (
          <p>
            <Link href="/approvals">
              {data.pending_approvals_count} pending — review
            </Link>
          </p>
        )}
      </section>

      <section className="panel">
        <h2 className="panel__title">Latest run</h2>
        {data.latest_run ? (
          <p className="overview__run">
            <Link href={`/runs/${data.latest_run.id}`}>
              Run #{data.latest_run.id}
            </Link>
            <span className="overview__run-meta">
              {data.latest_run.trade_date}
            </span>
            <RunStatusBadge status={data.latest_run.status} />
          </p>
        ) : (
          <p className="empty-state">No runs yet</p>
        )}
      </section>

      <NavHistoryPanel anchorNav={data.nav} />

      <section className="panel">
        <h2 className="panel__title">Positions</h2>
        {data.positions_summary.length === 0 ? (
          <p className="empty-state">No open positions</p>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Symbol</th>
                <th scope="col">Qty</th>
                <th scope="col">Market value</th>
                <th scope="col">Weight</th>
              </tr>
            </thead>
            <tbody>
              {data.positions_summary.map((row) => (
                <tr key={row.symbol}>
                  <td>{row.symbol}</td>
                  <td>{row.qty}</td>
                  <td>{currency.format(row.market_value)}</td>
                  <td>{percent.format(row.weight)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

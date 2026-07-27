import Link from "next/link";
import { NavSparkline } from "@/components/NavSparkline";
import { RunStatusBadge } from "@/components/RunStatusBadge";
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

export function OverviewDashboard({ data }: { data: OverviewResponse }) {
  return (
    <div className="overview">
      <header className="page-header">
        <p className="page-header__eyebrow">EOD desk</p>
        <h1 className="page-header__title">Overview</h1>
      </header>

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

      <div className="overview__split">
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

        <section className="panel">
          <h2 className="panel__title">NAV history</h2>
          <NavSparkline series={data.nav_series} />
        </section>
      </div>
    </div>
  );
}

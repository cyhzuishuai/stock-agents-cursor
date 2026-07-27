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
      <h1 className="overview__title">Overview</h1>

      <section className="overview__stats" aria-label="Account summary">
        <div className="overview__stat">
          <span className="overview__stat-label">Cash</span>
          <span className="overview__stat-value">{currency.format(data.cash)}</span>
        </div>
        <div className="overview__stat">
          <span className="overview__stat-label">Equity</span>
          <span className="overview__stat-value">
            {currency.format(data.equity)}
          </span>
        </div>
        <div className="overview__stat">
          <span className="overview__stat-label">NAV</span>
          <span className="overview__stat-value">{currency.format(data.nav)}</span>
        </div>
      </section>

      <section className="overview__panel">
        <h2 className="overview__panel-title">Pending approvals</h2>
        <p>
          {data.pending_approvals_count === 0 ? (
            "None pending"
          ) : (
            <Link href="/approvals">
              {data.pending_approvals_count} pending — review
            </Link>
          )}
        </p>
      </section>

      <section className="overview__panel">
        <h2 className="overview__panel-title">Latest run</h2>
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
          <p>No runs yet</p>
        )}
      </section>

      <section className="overview__panel">
        <h2 className="overview__panel-title">Positions</h2>
        {data.positions_summary.length === 0 ? (
          <p>No open positions</p>
        ) : (
          <table className="overview__table">
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

      <section className="overview__panel">
        <h2 className="overview__panel-title">NAV history</h2>
        <NavSparkline series={data.nav_series} />
      </section>
    </div>
  );
}

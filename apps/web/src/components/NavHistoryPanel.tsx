"use client";

import { useMemo, useState } from "react";
import { NavHistoryChart } from "@/components/NavHistoryChart";
import {
  buildMockNavHistory,
  sliceNavHistoryByRange,
  type NavHistoryRange,
} from "@/lib/mockNavHistory";

const RANGES: NavHistoryRange[] = ["1H", "1D", "1W", "1M"];

const navCurrency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

function formatAsOf(ts: string): string {
  return new Intl.DateTimeFormat("en-US", {
    month: "long",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZoneName: "short",
  }).format(new Date(ts));
}

export function NavHistoryPanel({ anchorNav }: { anchorNav: number }) {
  const [range, setRange] = useState<NavHistoryRange>("1D");
  const [tick, setTick] = useState(0);

  const series = useMemo(() => {
    void tick;
    const full = buildMockNavHistory(anchorNav, new Date());
    return sliceNavHistoryByRange(full, range);
  }, [anchorNav, range, tick]);

  const last = series[series.length - 1];
  const first = series[0];
  const change = last && first ? last.nav - first.nav : 0;
  const changePositive = change > 0;
  const changeNegative = change < 0;

  return (
    <section className="nav-portfolio" aria-label="NAV history">
      <div className="nav-portfolio__toolbar">
        <h2 className="nav-portfolio__title">Your NAV</h2>
        <div className="nav-portfolio__controls">
          <div
            className="nav-history-range"
            role="group"
            aria-label="NAV history range"
          >
            {RANGES.map((r) => (
              <button
                key={r}
                type="button"
                className={
                  range === r
                    ? "nav-history-range__btn is-active"
                    : "nav-history-range__btn"
                }
                aria-pressed={range === r}
                onClick={() => setRange(r)}
              >
                {r}
              </button>
            ))}
          </div>
          <button
            type="button"
            className="nav-portfolio__refresh"
            aria-label="Refresh NAV chart"
            onClick={() => setTick((n) => n + 1)}
          >
            <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
              <path
                fill="currentColor"
                d="M13.5 2.5a.75.75 0 0 0-1.5 0v1.1A5.75 5.75 0 1 0 13.6 10a.75.75 0 1 0-1.4-.55 4.25 4.25 0 1 1-.9-4.7H9.25a.75.75 0 0 0 0 1.5h3.5A.75.75 0 0 0 13.5 5.5v-3z"
              />
            </svg>
          </button>
        </div>
      </div>

      <div className="nav-portfolio__hero">
        <p className="nav-portfolio__value" data-testid="nav-history-value">
          {navCurrency.format(anchorNav).replace("$", "$ ")}
        </p>
        <span
          className={
            changePositive
              ? "nav-portfolio__change nav-portfolio__change--up"
              : changeNegative
                ? "nav-portfolio__change nav-portfolio__change--down"
                : "nav-portfolio__change"
          }
          data-testid="nav-history-change"
          aria-label={
            changePositive
              ? "Up in range"
              : changeNegative
                ? "Down in range"
                : "Unchanged in range"
          }
        >
          {changePositive ? "+" : changeNegative ? "−" : "·"}
        </span>
      </div>

      <p className="nav-portfolio__asof" data-testid="nav-history-asof">
        {last ? formatAsOf(last.ts) : "—"}
      </p>

      <NavHistoryChart series={series} range={range} />
    </section>
  );
}

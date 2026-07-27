// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { OverviewDashboard } from "./OverviewDashboard";
import type { OverviewResponse } from "@/lib/types";

const fixture: OverviewResponse = {
  cash: 50000,
  equity: 150000,
  nav: 200000,
  pending_approvals_count: 2,
  latest_run: {
    id: 42,
    trade_date: "2026-07-25",
    status: "awaiting_approval",
  },
  positions_summary: [
    { symbol: "AAPL", qty: 100, market_value: 18000, weight: 0.09 },
    { symbol: "MSFT", qty: 50, market_value: 22000, weight: 0.11 },
  ],
  nav_series: [
    { trade_date: "2026-07-23", nav: 195000 },
    { trade_date: "2026-07-24", nav: 198000 },
    { trade_date: "2026-07-25", nav: 200000 },
  ],
};

describe("OverviewDashboard", () => {
  it("renders cash, nav, approvals link, run status, positions, sparkline", () => {
    render(<OverviewDashboard data={fixture} />);

    expect(screen.getByRole("heading", { name: /overview/i })).toBeTruthy();
    expect(screen.getByText("$50,000")).toBeTruthy();
    expect(screen.getByText("$200,000")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /2 pending — review/i }).getAttribute("href"),
    ).toBe("/approvals");
    expect(
      screen.getByRole("link", { name: /run #42/i }).getAttribute("href"),
    ).toBe("/runs/42");
    expect(screen.getByText("Awaiting approval")).toBeTruthy();
    expect(screen.getByText("AAPL")).toBeTruthy();
    expect(screen.getByText("MSFT")).toBeTruthy();
    expect(
      screen.getByRole("img", { name: /nav history sparkline/i }),
    ).toBeTruthy();
  });
});

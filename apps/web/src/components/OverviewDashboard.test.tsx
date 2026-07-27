// @vitest-environment jsdom

import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { OverviewDashboard } from "./OverviewDashboard";
import type { OverviewResponse } from "@/lib/types";

afterEach(() => {
  cleanup();
});

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
  it("renders cash, nav, market clock, approvals link, run status, positions, chart", () => {
    render(<OverviewDashboard data={fixture} onRefresh={() => undefined} />);

    expect(screen.getByRole("heading", { name: /overview/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refresh NAV chart" })).toBeTruthy();
    expect(screen.getByLabelText("US market clock")).toBeTruthy();
    expect(screen.getByText(/trading desk/i)).toBeTruthy();
    expect(screen.getByText("NAV")).toBeTruthy();
    expect(screen.getByText("$50,000")).toBeTruthy();
    expect(screen.getAllByText(/\$\s*200,000/).length).toBeGreaterThan(0);
    expect(
      screen.getByRole("link", { name: /2 pending — review/i }).getAttribute("href"),
    ).toBe("/approvals");
    expect(
      screen.getByRole("link", { name: /run #42/i }).getAttribute("href"),
    ).toBe("/runs/42");
    expect(screen.getByText("Awaiting approval")).toBeTruthy();
    expect(screen.getByText("AAPL")).toBeTruthy();
    expect(screen.getByText("MSFT")).toBeTruthy();
    expect(screen.getByRole("heading", { name: /your nav/i })).toBeTruthy();
    expect(
      screen.getByRole("img", { name: /nav history chart/i }),
    ).toBeTruthy();
  });

  it("shows NAV history range controls with 1D selected by default", () => {
    render(<OverviewDashboard data={fixture} />);

    const range = screen.getByRole("group", { name: /nav history range/i });
    expect(within(range).getByRole("button", { name: "1H" }).getAttribute("aria-pressed")).toBe(
      "false",
    );
    expect(within(range).getByRole("button", { name: "1D" }).getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(within(range).getByRole("button", { name: "1W" }).getAttribute("aria-pressed")).toBe(
      "false",
    );
    expect(within(range).getByRole("button", { name: "1M" }).getAttribute("aria-pressed")).toBe(
      "false",
    );
  });

  it("switches NAV history range on click", async () => {
    const user = userEvent.setup();
    render(<OverviewDashboard data={fixture} />);

    const range = screen.getByRole("group", { name: /nav history range/i });
    await user.click(within(range).getByRole("button", { name: "1W" }));

    expect(within(range).getByRole("button", { name: "1W" }).getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(within(range).getByRole("button", { name: "1D" }).getAttribute("aria-pressed")).toBe(
      "false",
    );
    expect(
      screen.getByRole("img", { name: /nav history chart/i }),
    ).toBeTruthy();
  });
});

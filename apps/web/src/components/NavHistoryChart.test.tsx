// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { NavHistoryChart } from "./NavHistoryChart";
import type { NavHistoryPoint } from "@/lib/mockNavHistory";

afterEach(() => cleanup());

const series: NavHistoryPoint[] = [
  { ts: "2026-07-27T08:00:00.000Z", nav: 199000 },
  { ts: "2026-07-27T12:00:00.000Z", nav: 200500 },
  { ts: "2026-07-27T16:00:00.000Z", nav: 200000 },
];

describe("NavHistoryChart", () => {
  it("renders area chart with y-axis and x-axis labels", () => {
    render(<NavHistoryChart series={series} range="1D" />);

    expect(
      screen.getByRole("img", { name: /nav history chart/i }),
    ).toBeTruthy();
    expect(screen.getByTestId("nav-chart-area")).toBeTruthy();
    expect(screen.getByTestId("nav-chart-line")).toBeTruthy();
    // Compact y labels present
    expect(screen.getAllByText(/\$\d/).length).toBeGreaterThan(0);
  });

  it("shows empty state when no points", () => {
    render(<NavHistoryChart series={[]} range="1D" />);
    expect(screen.getByText(/no data/i)).toBeTruthy();
  });
});

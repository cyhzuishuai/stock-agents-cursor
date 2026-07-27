// @vitest-environment jsdom

import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { NavHistoryPanel } from "./NavHistoryPanel";

afterEach(() => cleanup());

describe("NavHistoryPanel", () => {
  it("shows title, big NAV, change, timestamp, ranges, and chart", () => {
    render(<NavHistoryPanel anchorNav={200000} />);

    expect(screen.getByRole("heading", { name: /your nav/i })).toBeTruthy();
    expect(screen.getByTestId("nav-history-value").textContent).toMatch(
      /\$\s*200,000/,
    );
    expect(screen.getByTestId("nav-history-change")).toBeTruthy();
    expect(screen.getByTestId("nav-history-asof")).toBeTruthy();
    expect(
      screen.getByRole("img", { name: /nav history chart/i }),
    ).toBeTruthy();

    const range = screen.getByRole("group", { name: /nav history range/i });
    expect(within(range).getByRole("button", { name: "1D" }).getAttribute("aria-pressed")).toBe(
      "true",
    );
  });

  it("switches range on click", async () => {
    const user = userEvent.setup();
    render(<NavHistoryPanel anchorNav={200000} />);

    const range = screen.getByRole("group", { name: /nav history range/i });
    await user.click(within(range).getByRole("button", { name: "1W" }));
    expect(within(range).getByRole("button", { name: "1W" }).getAttribute("aria-pressed")).toBe(
      "true",
    );
  });
});

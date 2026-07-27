// @vitest-environment jsdom

import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SettingsPage from "./page";
import type { SettingsResponse, Strategy, StrategyWriteBody } from "@/lib/types";

const settingsFixture: SettingsResponse = {
  watchlist: ["AAPL"],
  risk_rules: { max_order_notional: 10000 },
  market_data_provider: "stub",
};

const strategiesFixture: Strategy[] = [
  {
    id: 1,
    name: "整体策略1",
    description: "Default overall strategy",
    is_system_default: true,
    is_active: true,
    pre_open_minutes: 10,
    intraday_every_minutes: 60,
    intraday_start_et: "10:00",
    intraday_end_et: "15:00",
    execution_mode: "auto_reject_breaches",
  },
];

describe("SettingsPage strategies", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input, init) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.includes("/api/v1/strategies") && method === "POST") {
          const body = JSON.parse(String(init?.body)) as StrategyWriteBody;
          return new Response(
            JSON.stringify({ id: 2, ...body, is_system_default: false, is_active: false }),
            {
              status: 201,
              headers: { "Content-Type": "application/json" },
            },
          );
        }
        if (url.includes("/api/v1/strategies")) {
          return new Response(JSON.stringify(strategiesFixture), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url.includes("/api/v1/settings")) {
          return new Response(JSON.stringify(settingsFixture), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        return new Response("not found", { status: 404 });
      }),
    );
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("renders Strategies heading and seeded name when API mocked", async () => {
    render(<SettingsPage />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Strategies" })).toBeTruthy();
      expect(screen.getByText("整体策略1")).toBeTruthy();
    });
    expect(
      screen.getByText("Pre-open 10m · every 60m 10:00–15:00 ET"),
    ).toBeTruthy();
  });

  it("allows intraday_every_minutes 0 and rejects values between 1 and 14", async () => {
    const user = userEvent.setup();
    render(<SettingsPage />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Strategies" })).toBeTruthy();
    });

    const strategiesSection = screen
      .getByRole("heading", { name: "Strategies" })
      .closest("section");
    expect(strategiesSection).toBeTruthy();

    await user.click(
      within(strategiesSection!).getByRole("button", { name: "Create" }),
    );

    const everyInput = screen.getByLabelText("Intraday every minutes");
    expect(everyInput.getAttribute("min")).toBe("0");
    expect(
      screen.getByText(
        "0 disables intraday runs; otherwise use 15 or more minutes.",
      ),
    ).toBeTruthy();

    await user.type(screen.getByLabelText("Name"), "No intraday");
    await user.clear(everyInput);
    await user.type(everyInput, "10");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(
        screen.getByText("intraday_every_minutes must be 0 or >= 15"),
      ).toBeTruthy();
    });
    expect(
      vi.mocked(fetch).mock.calls.filter(
        ([, init]) => init?.method === "POST",
      ),
    ).toHaveLength(0);

    await user.clear(everyInput);
    await user.type(everyInput, "0");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/strategies"),
        expect.objectContaining({
          method: "POST",
          body: expect.stringContaining('"intraday_every_minutes":0'),
        }),
      );
    });
  });
});

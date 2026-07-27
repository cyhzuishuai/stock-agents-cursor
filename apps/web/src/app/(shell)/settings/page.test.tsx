// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SettingsPage from "./page";
import type { SettingsResponse, Strategy } from "@/lib/types";

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
      vi.fn(async (input) => {
        const url = String(input);
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
});

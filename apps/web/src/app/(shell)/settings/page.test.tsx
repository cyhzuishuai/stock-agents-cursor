// @vitest-environment jsdom

import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SettingsPage from "./page";
import type {
  SettingsResponse,
  Strategy,
  StrategyWriteBody,
  WatchlistItem,
} from "@/lib/types";

let settingsFixture: SettingsResponse = {
  watchlist: [{ symbol: "AAPL", can_hold: true }],
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

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("SettingsPage strategies", () => {
  beforeEach(() => {
    settingsFixture = {
      watchlist: [{ symbol: "AAPL", can_hold: true }],
      risk_rules: { max_order_notional: 10000 },
      market_data_provider: "stub",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input, init) => {
        const url = String(input);
        const method = init?.method ?? "GET";
        if (url.includes("/api/v1/strategies") && method === "POST") {
          const body = JSON.parse(String(init?.body)) as StrategyWriteBody;
          return jsonResponse(
            { id: 2, ...body, is_system_default: false, is_active: false },
            201,
          );
        }
        if (url.includes("/api/v1/strategies")) {
          return jsonResponse(strategiesFixture);
        }
        if (url.includes("/api/v1/settings")) {
          return jsonResponse(settingsFixture);
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
    await user.click(
      within(strategiesSection!).getByRole("button", { name: "Save" }),
    );

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
    await user.click(
      within(strategiesSection!).getByRole("button", { name: "Save" }),
    );

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

describe("SettingsPage watchlist and risk", () => {
  beforeEach(() => {
    settingsFixture = {
      watchlist: [{ symbol: "AAPL", can_hold: true }],
      risk_rules: { max_order_notional: 10000 },
      market_data_provider: "stub",
    };
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input, init) => {
        const url = String(input);
        const method = init?.method ?? "GET";

        if (url.includes("/api/v1/symbols/search")) {
          return jsonResponse([
            {
              symbol: "MSFT",
              name: "Microsoft Corporation",
              price: 420.1,
              change: 1.2,
              change_pct: 0.29,
              asset_class: "us_equity",
            },
          ]);
        }
        if (url.includes("/api/v1/settings/watchlist") && method === "POST") {
          const body = JSON.parse(String(init?.body)) as WatchlistItem;
          settingsFixture = {
            ...settingsFixture,
            watchlist: [
              ...settingsFixture.watchlist,
              { symbol: body.symbol, can_hold: body.can_hold ?? true },
            ],
          };
          return jsonResponse(
            { symbol: body.symbol, can_hold: body.can_hold ?? true },
            201,
          );
        }
        if (url.includes("/api/v1/settings/watchlist/") && method === "PATCH") {
          const body = JSON.parse(String(init?.body)) as { can_hold: boolean };
          const symbol = url.split("/").pop()!;
          settingsFixture = {
            ...settingsFixture,
            watchlist: settingsFixture.watchlist.map((item) =>
              item.symbol === symbol
                ? { ...item, can_hold: body.can_hold }
                : item,
            ),
          };
          return jsonResponse({ symbol, can_hold: body.can_hold });
        }
        if (url.includes("/api/v1/settings/risk/") && method === "PATCH") {
          const body = JSON.parse(String(init?.body)) as { value: number };
          const key = url.split("/").pop()!;
          settingsFixture = {
            ...settingsFixture,
            risk_rules: { ...settingsFixture.risk_rules, [key]: body.value },
          };
          return jsonResponse({ key, value: body.value });
        }
        if (url.includes("/api/v1/strategies")) {
          return jsonResponse(strategiesFixture);
        }
        if (url.includes("/api/v1/settings")) {
          return jsonResponse(settingsFixture);
        }
        return new Response("not found", { status: 404 });
      }),
    );
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("toggles 可持仓 via PATCH", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPage />);

    const checkbox = await screen.findByRole("checkbox", {
      name: "可持仓 AAPL",
    });
    expect((checkbox as HTMLInputElement).checked).toBe(true);

    await user.click(checkbox);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/settings/watchlist/AAPL"),
        expect.objectContaining({
          method: "PATCH",
          body: expect.stringContaining('"can_hold":false'),
        }),
      );
    });
  });

  it("adds a searched symbol via POST", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPage />);

    await screen.findByRole("heading", { name: "Watchlist" });
    const search = screen.getByPlaceholderText("e.g. AAPL");
    await user.type(search, "msf");
    await vi.advanceTimersByTimeAsync(350);

    const option = await screen.findByRole("button", {
      name: /MSFT · Microsoft Corporation/,
    });
    await user.click(option);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/settings/watchlist"),
        expect.objectContaining({
          method: "POST",
          body: expect.stringContaining('"symbol":"MSFT"'),
        }),
      );
    });
  });

  it("saves risk rule via PATCH", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPage />);

    const valueInput = await screen.findByLabelText(
      "Risk value max_order_notional",
    );
    await user.clear(valueInput);
    await user.type(valueInput, "12345");
    const riskSection = screen
      .getByRole("heading", { name: "Risk rules" })
      .closest("section");
    await user.click(
      within(riskSection!).getByRole("button", { name: "Save" }),
    );

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/settings/risk/max_order_notional"),
        expect.objectContaining({
          method: "PATCH",
          body: expect.stringContaining('"value":12345'),
        }),
      );
    });
  });
});

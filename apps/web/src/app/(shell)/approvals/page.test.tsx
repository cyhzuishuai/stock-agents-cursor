// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ApprovalsPage from "./page";
import type { ApprovalItem } from "@/lib/types";

const fixture: ApprovalItem[] = [
  {
    id: 7,
    proposal_id: 3,
    symbol: "AAPL",
    side: "buy",
    qty: 100,
    breach_reasons: ["max_order_notional"],
    created_at: "2026-07-25T00:00:00Z",
  },
];

describe("ApprovalsPage", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input, init) => {
        const url = String(input);
        if (url.includes("/decide") && init?.method === "POST") {
          return new Response(JSON.stringify({ ok: true }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        return new Response(JSON.stringify(fixture), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("decide posts correct body", async () => {
    const user = userEvent.setup();
    render(<ApprovalsPage />);

    await waitFor(() => {
      expect(screen.getByText("AAPL")).toBeTruthy();
    });

    await user.type(
      screen.getByLabelText(/note for approval #7/i),
      "looks ok",
    );
    await user.click(screen.getByRole("button", { name: /approve #7/i }));

    await waitFor(() => {
      const decideCall = vi.mocked(fetch).mock.calls.find(
        ([url, init]) =>
          String(url).includes("/api/v1/approvals/7/decide") &&
          init?.method === "POST",
      );
      expect(decideCall).toBeTruthy();
      const [, init] = decideCall!;
      expect(init?.body).toBe(
        JSON.stringify({ decision: "approved", note: "looks ok" }),
      );
    });
  });
});

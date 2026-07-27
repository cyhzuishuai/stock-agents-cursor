// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import RunDetailPage from "./page";
import type { RunDetail } from "@/lib/types";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "1" }),
}));

vi.mock("next/link", () => ({
  default: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
}));

const fixture: RunDetail = {
  id: 1,
  trade_date: "2026-07-28",
  status: "completed",
  strategy_id: 1,
  strategy_name: "整体策略1",
  trigger: "manual",
  steps: [
    {
      id: 10,
      run_id: 1,
      step: "research",
      status: "ok",
      payload_json: '{"thesis":"up"}',
    },
  ],
  proposals: [],
  orders: [],
};

describe("RunDetailPage", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input) => {
        const url = String(input);
        if (url.includes("/api/v1/runs/1")) {
          return new Response(JSON.stringify(fixture), {
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

  it("shows pretty JSON payload after expanding", async () => {
    const user = userEvent.setup();
    render(<RunDetailPage />);

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /show payload/i }),
      ).toBeTruthy();
    });

    await user.click(screen.getByRole("button", { name: /show payload/i }));

    expect(screen.getByText(/thesis/)).toBeTruthy();
    expect(screen.getByText(/manual/)).toBeTruthy();
    expect(screen.getByText(/整体策略1/)).toBeTruthy();
  });
});

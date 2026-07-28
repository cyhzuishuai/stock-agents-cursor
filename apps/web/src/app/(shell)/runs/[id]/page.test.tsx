// @vitest-environment jsdom

import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import RunDetailPage from "./page";
import { formatStartedAt } from "@/lib/datetime";
import type { RunDetail } from "@/lib/types";
import analystEnvelope from "../../../../../../../packages/contracts/fixtures/agent_run_response.valid.json";

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

const legacyFixture: RunDetail = {
  id: 1,
  trade_date: "2026-07-28",
  status: "completed",
  created_at: "2026-07-28T13:45:00Z",
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

const envelopeFixture: RunDetail = {
  id: 1,
  trade_date: "2026-07-28",
  status: "completed",
  created_at: "2026-07-28T13:45:00Z",
  strategy_id: 1,
  strategy_name: "整体策略1",
  trigger: "manual",
  steps: [
    {
      id: 20,
      run_id: 1,
      step: "analyst",
      status: "ok",
      payload_json: JSON.stringify(analystEnvelope),
    },
  ],
  proposals: [],
  orders: [],
};

function mockRunFetch(run: RunDetail) {
  vi.unstubAllGlobals();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input) => {
      const url = String(input);
      if (url.includes(`/api/v1/runs/${run.id}`)) {
        return new Response(JSON.stringify(run), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("not found", { status: 404 });
    }),
  );
}

describe("RunDetailPage", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("shows pretty JSON payload after expanding legacy step", async () => {
    mockRunFetch(legacyFixture);
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

  it("renders analyst result summary and tool trace for envelope payloads", async () => {
    mockRunFetch(envelopeFixture);
    const user = userEvent.setup();
    render(<RunDetailPage />);

    await waitFor(() => {
      expect(screen.getByText("Analyst")).toBeTruthy();
      expect(screen.getByText("AAPL")).toBeTruthy();
      expect(screen.getByText("buy")).toBeTruthy();
      expect(screen.getByText("bull")).toBeTruthy();
      expect(screen.getByText(/stop: final/i)).toBeTruthy();
    });

    expect(
      screen.getByRole("button", { name: /show tool trace/i }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /show raw payload/i }),
    ).toBeTruthy();

    await user.click(screen.getByRole("button", { name: /show tool trace/i }));

    expect(screen.getByText(/Round 1/i)).toBeTruthy();
    expect(screen.getByText(/get_account_view/i)).toBeTruthy();
    expect(screen.getByText(/get_daily_bars/i)).toBeTruthy();

    const roundOne = screen.getByText(/Round 1/i).closest("li");
    expect(roundOne).toBeTruthy();
    expect(within(roundOne!).getByText(/820 ms/)).toBeTruthy();
    expect(within(roundOne!).getByText(/args: \{\}/)).toBeTruthy();
    expect(within(roundOne!).getByText(/100000/)).toBeTruthy();

    const roundTwo = screen.getByText(/Round 2/i).closest("li");
    expect(roundTwo).toBeTruthy();
    expect(within(roundTwo!).getByText(/lookback_days/i)).toBeTruthy();
  });

  it("shows agent timeline and handoff for envelopes with events", async () => {
    mockRunFetch(envelopeFixture);
    const user = userEvent.setup();
    render(<RunDetailPage />);

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /show agent timeline/i }),
      ).toBeTruthy();
    });

    expect(screen.getByLabelText(/handoff summary/i)).toBeTruthy();
    expect(screen.getByText(/2 symbols/i)).toBeTruthy();
    expect(
      screen.getByText(/Does MSFT need reassessment after earnings\?/),
    ).toBeTruthy();
    expect(screen.queryByRole("link", { name: /open in langsmith/i })).toBeNull();

    await user.click(
      screen.getByRole("button", { name: /show agent timeline/i }),
    );

    const timeline = screen.getByLabelText(/agent timeline/i);
    expect(within(timeline).getByText("plan")).toBeTruthy();
    expect(within(timeline).getByText("reflect")).toBeTruthy();
    expect(within(timeline).getByText("finalize")).toBeTruthy();
  });

  it("omits agent timeline when envelope has no events", async () => {
    const withoutEvents = structuredClone(analystEnvelope) as Record<
      string,
      unknown
    >;
    delete (withoutEvents.trace as Record<string, unknown>).events;
    delete withoutEvents.handoff;

    mockRunFetch({
      ...envelopeFixture,
      steps: [
        {
          ...envelopeFixture.steps[0],
          payload_json: JSON.stringify(withoutEvents),
        },
      ],
    });
    render(<RunDetailPage />);

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /show tool trace/i }),
      ).toBeTruthy();
    });

    expect(
      screen.queryByRole("button", { name: /show agent timeline/i }),
    ).toBeNull();
    expect(screen.queryByLabelText(/handoff summary/i)).toBeNull();
  });

  it("shows Open in LangSmith when langsmith_run_url is present", async () => {
    const withLangsmith = structuredClone(analystEnvelope) as {
      trace: Record<string, unknown>;
    };
    withLangsmith.trace.langsmith_run_url =
      "https://smith.langchain.com/o/demo/projects/p/run/abc";

    mockRunFetch({
      ...envelopeFixture,
      steps: [
        {
          ...envelopeFixture.steps[0],
          payload_json: JSON.stringify(withLangsmith),
        },
      ],
    });
    render(<RunDetailPage />);

    await waitFor(() => {
      expect(
        screen.getByRole("link", { name: /open in langsmith/i }),
      ).toBeTruthy();
    });

    const link = screen.getByRole("link", { name: /open in langsmith/i });
    expect(link.getAttribute("href")).toBe(
      "https://smith.langchain.com/o/demo/projects/p/run/abc",
    );
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("shows formatted Started in meta when created_at is set", async () => {
    mockRunFetch(legacyFixture);
    const { container } = render(<RunDetailPage />);

    const expected = formatStartedAt("2026-07-28T13:45:00Z");
    await waitFor(() => {
      const meta = container.querySelector(".runs__meta");
      expect(meta?.textContent).toContain(expected);
    });
  });

  it("shows em dash in meta when created_at is empty", async () => {
    mockRunFetch({ ...legacyFixture, created_at: "" });
    const { container } = render(<RunDetailPage />);

    await waitFor(() => {
      const meta = container.querySelector(".runs__meta");
      expect(meta?.textContent).toMatch(/2026-07-28.*—/);
      expect(meta?.textContent).not.toMatch(/\d{2}:\d{2}/);
    });
  });
});

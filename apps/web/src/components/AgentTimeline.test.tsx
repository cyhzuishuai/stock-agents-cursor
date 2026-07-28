// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AgentTimeline } from "./AgentTimeline";
import { HandoffSummary } from "./HandoffSummary";
import type { AgentTraceEvent } from "@/lib/types";

afterEach(() => {
  cleanup();
});

const sampleEvents: AgentTraceEvent[] = [
  {
    type: "plan",
    at: "2026-07-28T13:00:00Z",
    current_step_id: "gather",
    plan: [{ id: "gather", title: "Gather data", status: "in_progress" }],
  },
  {
    type: "llm",
    at: "2026-07-28T13:00:01Z",
    phase: "act",
    model: "Doubao-Smart-Router",
    fallback_used: false,
  },
  {
    type: "tool",
    at: "2026-07-28T13:00:02Z",
    name: "get_bars",
    ok: true,
    latency_ms: 42,
  },
  {
    type: "reflect",
    at: "2026-07-28T13:00:03Z",
    decision: "finalize",
    reason: "ready",
  },
];

describe("AgentTimeline", () => {
  it("renders plan and reflect type badges for sample events", () => {
    render(<AgentTimeline events={sampleEvents} />);

    expect(screen.getByText("plan")).toBeTruthy();
    expect(screen.getByText("reflect")).toBeTruthy();
    expect(screen.getByText("llm")).toBeTruthy();
    expect(screen.getByText("tool")).toBeTruthy();
  });

  it("shows one-line summaries for tool name, reflect decision, and llm model", () => {
    render(<AgentTimeline events={sampleEvents} />);

    expect(screen.getByText(/get_bars/i)).toBeTruthy();
    expect(screen.getByText(/finalize/i)).toBeTruthy();
    expect(screen.getByText(/Doubao-Smart-Router/i)).toBeTruthy();
  });

  it("returns null when events are empty", () => {
    const { container } = render(<AgentTimeline events={[]} />);
    expect(container.firstChild).toBeNull();
  });
});

describe("HandoffSummary", () => {
  it("shows thesis count, open questions, and confidence notes when present", () => {
    render(
      <HandoffSummary
        handoff={{
          thesis_by_symbol: {
            AAPL: { summary: "Bullish", bias: "bull", confidence: 0.8 },
            MSFT: { summary: "Neutral", bias: "neutral", confidence: 0.5 },
          },
          open_questions: ["Earnings surprise?", "Volume dry-up?"],
          confidence_notes: "High conviction on AAPL only",
        }}
      />,
    );

    expect(screen.getByText(/2 symbols/i)).toBeTruthy();
    expect(screen.getByText(/Earnings surprise\?/)).toBeTruthy();
    expect(screen.getByText(/High conviction on AAPL only/)).toBeTruthy();
  });

  it("returns null when handoff is missing or empty", () => {
    const { container: empty } = render(<HandoffSummary handoff={undefined} />);
    expect(empty.firstChild).toBeNull();

    const { container: blank } = render(<HandoffSummary handoff={{}} />);
    expect(blank.firstChild).toBeNull();
  });
});

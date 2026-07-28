"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { RunStatusBadge } from "@/components/RunStatusBadge";
import { api } from "@/lib/api";
import { formatStartedAt } from "@/lib/datetime";
import type {
  AgentRunEnvelope,
  AgentToolCall,
  AgentToolResult,
  AgentTrace,
  AgentTraceRound,
  AnalystResult,
  Order,
  PortfolioResult,
  RunDetail,
  TradeProposal,
  WorkflowStep,
} from "@/lib/types";

const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const STEP_LABELS: Record<string, string> = {
  created: "Created",
  analyst: "Analyst",
  portfolio: "Portfolio",
  data: "Data",
  research: "Research",
  decision: "Decision",
  risk: "Risk",
};

function stepLabel(step: string): string {
  return STEP_LABELS[step] ?? step;
}

function stepStatusClass(status: string): string {
  if (status === "ok") return "runs__step-status runs__step-status--ok";
  if (status === "failed") return "runs__step-status runs__step-status--failed";
  return "runs__step-status";
}

function proposalStatus(proposal: TradeProposal): string {
  return typeof proposal.status === "string" ? proposal.status : "—";
}

function orderFillPrice(order: Order): number | null {
  return typeof order.fill_price === "number" ? order.fill_price : null;
}

function orderNotional(order: Order): number | null {
  return typeof order.notional === "number" ? order.notional : null;
}

function prettyJson(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

function parseEnvelope(raw: string): AgentRunEnvelope | null {
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (
      parsed &&
      typeof parsed === "object" &&
      "result" in parsed &&
      "trace" in parsed
    ) {
      const trace = (parsed as AgentRunEnvelope).trace;
      if (trace && typeof trace === "object" && Array.isArray(trace.rounds)) {
        return parsed as AgentRunEnvelope;
      }
    }
  } catch {
    /* legacy payload */
  }
  return null;
}

function isAnalystResult(
  result: AgentRunEnvelope["result"],
): result is AnalystResult {
  return (
    result &&
    typeof result === "object" &&
    Array.isArray((result as AnalystResult).items)
  );
}

function isPortfolioResult(
  result: AgentRunEnvelope["result"],
): result is PortfolioResult {
  return (
    result &&
    typeof result === "object" &&
    Array.isArray((result as PortfolioResult).proposals)
  );
}

function toolArgs(
  tool: AgentToolResult,
  round: AgentTraceRound,
): Record<string, unknown> | undefined {
  const calls = round.assistant?.tool_calls ?? [];
  const match = calls.find(
    (call: AgentToolCall) =>
      (tool.id && call.id === tool.id) || call.name === tool.name,
  );
  return match?.args;
}

function StepResultSummary({ envelope }: { envelope: AgentRunEnvelope }) {
  const { result, trace } = envelope;

  if (trace.agent === "analyst" && isAnalystResult(result)) {
    if (result.items.length === 0) {
      return <p className="empty-state">No analyst items</p>;
    }
    return (
      <table className="data-table">
        <thead>
          <tr>
            <th scope="col">Symbol</th>
            <th scope="col">Side</th>
            <th scope="col">Bias</th>
            <th scope="col">Confidence</th>
          </tr>
        </thead>
        <tbody>
          {result.items.map((item) => (
            <tr key={item.symbol}>
              <td>{item.symbol}</td>
              <td>{item.side}</td>
              <td>{item.bias}</td>
              <td>
                {typeof item.confidence === "number"
                  ? item.confidence.toFixed(2)
                  : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  }

  if (trace.agent === "portfolio" && isPortfolioResult(result)) {
    if (result.proposals.length === 0) {
      return <p className="empty-state">No proposals</p>;
    }
    return (
      <table className="data-table">
        <thead>
          <tr>
            <th scope="col">Symbol</th>
            <th scope="col">Side</th>
            <th scope="col">Qty</th>
            <th scope="col">Notional</th>
          </tr>
        </thead>
        <tbody>
          {result.proposals.map((proposal) => (
            <tr key={`${proposal.symbol}-${proposal.side}`}>
              <td>{proposal.symbol}</td>
              <td>{proposal.side}</td>
              <td>{proposal.qty}</td>
              <td>
                {typeof proposal.estimated_notional === "number"
                  ? currency.format(proposal.estimated_notional)
                  : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  }

  if (isAnalystResult(result)) {
    return (
      <table className="data-table">
        <thead>
          <tr>
            <th scope="col">Symbol</th>
            <th scope="col">Side</th>
            <th scope="col">Bias</th>
          </tr>
        </thead>
        <tbody>
          {result.items.map((item) => (
            <tr key={item.symbol}>
              <td>{item.symbol}</td>
              <td>{item.side}</td>
              <td>{item.bias}</td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  }

  if (isPortfolioResult(result)) {
    return (
      <table className="data-table">
        <thead>
          <tr>
            <th scope="col">Symbol</th>
            <th scope="col">Side</th>
            <th scope="col">Qty</th>
          </tr>
        </thead>
        <tbody>
          {result.proposals.map((proposal) => (
            <tr key={`${proposal.symbol}-${proposal.side}`}>
              <td>{proposal.symbol}</td>
              <td>{proposal.side}</td>
              <td>{proposal.qty}</td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  }

  return null;
}

function StepToolTrace({ trace }: { trace: AgentTrace }) {
  if (trace.rounds.length === 0) {
    return <p className="empty-state">No tool rounds recorded</p>;
  }

  return (
    <ol className="runs__trace">
      {trace.rounds.map((round, index) => {
        const roundIndex = round.i ?? index + 1;
        const llmLatency =
          typeof round.llm?.latency_ms === "number"
            ? `${round.llm.latency_ms} ms`
            : null;

        return (
          <li key={roundIndex} className="runs__trace-round">
            <p className="runs__trace-round-title">
              Round {roundIndex}
              {llmLatency ? ` · LLM ${llmLatency}` : null}
            </p>
            {(round.tools ?? []).length === 0 ? (
              <p className="empty-state">No tools in this round</p>
            ) : (
              <ul className="runs__trace-tools">
                {(round.tools ?? []).map((tool) => {
                  const args = toolArgs(tool, round);
                  const argsText =
                    args && Object.keys(args).length > 0
                      ? JSON.stringify(args)
                      : "{}";
                  const status = tool.ok ? "ok" : "failed";
                  const latency =
                    typeof tool.latency_ms === "number"
                      ? `${tool.latency_ms} ms`
                      : "—";
                  const preview = tool.ok
                    ? (tool.result_preview ?? "—")
                    : (tool.error ?? tool.result_preview ?? "—");

                  return (
                    <li key={`${roundIndex}-${tool.id ?? tool.name}`}>
                      <p>
                        <strong>{tool.name}</strong> · {status} · {latency}
                      </p>
                      <p className="runs__trace-meta">args: {argsText}</p>
                      <pre className="runs__payload">{preview}</pre>
                    </li>
                  );
                })}
              </ul>
            )}
          </li>
        );
      })}
    </ol>
  );
}

function StepPayload({
  raw,
  envelope,
}: {
  raw: string;
  envelope: AgentRunEnvelope | null;
}) {
  const [traceOpen, setTraceOpen] = useState(false);
  const [rawOpen, setRawOpen] = useState(false);
  const [legacyOpen, setLegacyOpen] = useState(false);
  const pretty = prettyJson(raw);

  if (!envelope) {
    return (
      <div>
        <button
          type="button"
          className="btn btn--ghost"
          onClick={() => setLegacyOpen((v) => !v)}
        >
          {legacyOpen ? "Hide payload" : "Show payload"}
        </button>
        {legacyOpen ? <pre className="runs__payload">{pretty}</pre> : null}
      </div>
    );
  }

  return (
    <div className="runs__step-payload">
      <StepResultSummary envelope={envelope} />
      <div className="runs__step-actions">
        <button
          type="button"
          className="btn btn--ghost"
          onClick={() => setTraceOpen((v) => !v)}
        >
          {traceOpen ? "Hide tool trace" : "Show tool trace"}
        </button>
        <button
          type="button"
          className="btn btn--ghost"
          onClick={() => setRawOpen((v) => !v)}
        >
          {rawOpen ? "Hide raw payload" : "Show raw payload"}
        </button>
      </div>
      {traceOpen ? <StepToolTrace trace={envelope.trace} /> : null}
      {rawOpen ? <pre className="runs__payload">{pretty}</pre> : null}
    </div>
  );
}

function stepStopReason(step: WorkflowStep): string | null {
  if (!step.payload_json) return null;
  const envelope = parseEnvelope(step.payload_json);
  return envelope?.trace.stop_reason ?? null;
}

function stepUsageLabel(step: WorkflowStep): string | null {
  if (!step.payload_json) return null;
  const envelope = parseEnvelope(step.payload_json);
  const usage = envelope?.trace.usage;
  if (!usage) return null;
  const prompt = usage.prompt_tokens;
  const completion = usage.completion_tokens;
  if (typeof prompt !== "number" && typeof completion !== "number") return null;
  const parts: string[] = [];
  if (typeof prompt === "number") parts.push(`${prompt} prompt`);
  if (typeof completion === "number") parts.push(`${completion} completion`);
  return parts.join(" · ");
}

export default function RunDetailPage() {
  const params = useParams<{ id: string }>();
  const runId = params.id;

  const [data, setData] = useState<RunDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<RunDetail>(`/api/v1/runs/${runId}`);
        if (!cancelled) {
          setData(response);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load run");
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [runId]);

  if (loading) {
    return <p className="empty-state">Loading run…</p>;
  }

  if (error) {
    return <p className="alert" role="alert">{error}</p>;
  }

  if (!data) {
    return <p className="alert" role="alert">Run data unavailable</p>;
  }

  return (
    <div className="runs">
      <p className="runs__back">
        <Link href="/runs">← All runs</Link>
      </p>

      <header className="page-header">
        <p className="page-header__eyebrow">Trading desk</p>
        <h1 className="page-header__title">Run #{data.id}</h1>
        <p className="runs__meta">
          <span>{data.trade_date}</span>
          <span>{formatStartedAt(data.created_at)}</span>
          <RunStatusBadge status={data.status} />
          {data.trigger ? <span>{data.trigger}</span> : null}
          {data.strategy_name ? <span>{data.strategy_name}</span> : null}
        </p>
      </header>

      <section className="panel" aria-label="Workflow steps">
        <h2 className="panel__title">Steps</h2>
        {data.steps.length === 0 ? (
          <p className="empty-state">No steps recorded</p>
        ) : (
          <ol className="runs__timeline">
            {data.steps.map((step: WorkflowStep) => {
              const envelope = step.payload_json
                ? parseEnvelope(step.payload_json)
                : null;
              const stopReason = stepStopReason(step);
              const usageLabel = stepUsageLabel(step);

              return (
                <li key={step.id} className="runs__timeline-item">
                  <span className="runs__step-name">{stepLabel(step.step)}</span>
                  <span className={stepStatusClass(step.status)}>{step.status}</span>
                  {stopReason ? (
                    <span className="runs__meta">stop: {stopReason}</span>
                  ) : null}
                  {usageLabel ? (
                    <span className="runs__meta">{usageLabel}</span>
                  ) : null}
                  {step.payload_json ? (
                    <StepPayload raw={step.payload_json} envelope={envelope} />
                  ) : null}
                </li>
              );
            })}
          </ol>
        )}
      </section>

      <section className="panel" aria-label="Trade proposals">
        <h2 className="panel__title">Proposals</h2>
        {data.proposals.length === 0 ? (
          <p className="empty-state">No proposals</p>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Symbol</th>
                <th scope="col">Side</th>
                <th scope="col">Qty</th>
                <th scope="col">Notional</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {data.proposals.map((proposal) => (
                <tr key={proposal.id}>
                  <td>{proposal.symbol}</td>
                  <td>{proposal.side}</td>
                  <td>{proposal.qty}</td>
                  <td>
                    {typeof proposal.estimated_notional === "number"
                      ? currency.format(proposal.estimated_notional)
                      : "—"}
                  </td>
                  <td>{proposalStatus(proposal)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="panel" aria-label="Orders">
        <h2 className="panel__title">Orders</h2>
        {data.orders.length === 0 ? (
          <p className="empty-state">No orders</p>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Symbol</th>
                <th scope="col">Side</th>
                <th scope="col">Qty</th>
                <th scope="col">Fill price</th>
                <th scope="col">Notional</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {data.orders.map((order) => (
                <tr key={order.id}>
                  <td>{order.symbol}</td>
                  <td>{order.side}</td>
                  <td>{order.qty}</td>
                  <td>
                    {orderFillPrice(order) != null
                      ? currency.format(orderFillPrice(order)!)
                      : "—"}
                  </td>
                  <td>
                    {orderNotional(order) != null
                      ? currency.format(orderNotional(order)!)
                      : "—"}
                  </td>
                  <td>{typeof order.status === "string" ? order.status : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

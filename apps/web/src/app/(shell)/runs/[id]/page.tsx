"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { RunStatusBadge } from "@/components/RunStatusBadge";
import { api } from "@/lib/api";
import type { Order, RunDetail, TradeProposal, WorkflowStep } from "@/lib/types";

const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const STEP_LABELS: Record<string, string> = {
  created: "Created",
  data: "Data",
  research: "Research",
  decision: "Decision",
  portfolio: "Portfolio",
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
    return <p>Loading run…</p>;
  }

  if (error) {
    return <p role="alert">{error}</p>;
  }

  if (!data) {
    return <p role="alert">Run data unavailable</p>;
  }

  return (
    <div className="runs">
      <p className="runs__back">
        <Link href="/runs">← All runs</Link>
      </p>

      <header className="runs__detail-header">
        <h1 className="runs__title">Run #{data.id}</h1>
        <p className="runs__meta">
          <span>{data.trade_date}</span>
          <RunStatusBadge status={data.status} />
        </p>
      </header>

      <section className="runs__panel" aria-label="Workflow steps">
        <h2 className="runs__panel-title">Steps</h2>
        {data.steps.length === 0 ? (
          <p>No steps recorded</p>
        ) : (
          <ol className="runs__timeline">
            {data.steps.map((step: WorkflowStep) => (
              <li key={step.id} className="runs__timeline-item">
                <span className="runs__step-name">{stepLabel(step.step)}</span>
                <span className={stepStatusClass(step.status)}>{step.status}</span>
              </li>
            ))}
          </ol>
        )}
      </section>

      <section className="runs__panel" aria-label="Trade proposals">
        <h2 className="runs__panel-title">Proposals</h2>
        {data.proposals.length === 0 ? (
          <p>No proposals</p>
        ) : (
          <table className="runs__table">
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

      <section className="runs__panel" aria-label="Orders">
        <h2 className="runs__panel-title">Orders</h2>
        {data.orders.length === 0 ? (
          <p>No orders</p>
        ) : (
          <table className="runs__table">
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

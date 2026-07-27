"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ApprovalDecideRequest, ApprovalDecision, ApprovalItem } from "@/lib/types";

export default function ApprovalsPage() {
  const [approvals, setApprovals] = useState<ApprovalItem[]>([]);
  const [notes, setNotes] = useState<Record<number, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [decidingId, setDecidingId] = useState<number | null>(null);

  const loadApprovals = useCallback(async () => {
    const response = await api.get<ApprovalItem[]>("/api/v1/approvals", {
      status: "pending",
    });
    setApprovals(response);
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<ApprovalItem[]>("/api/v1/approvals", {
          status: "pending",
        });
        if (!cancelled) {
          setApprovals(response);
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load approvals",
          );
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
  }, []);

  async function handleDecide(id: number, decision: ApprovalDecision) {
    setDecidingId(id);
    setError(null);
    try {
      const body: ApprovalDecideRequest = {
        decision,
        note: notes[id]?.trim() || undefined,
      };
      await api.post(`/api/v1/approvals/${id}/decide`, body);
      await loadApprovals();
      setNotes((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to submit decision",
      );
    } finally {
      setDecidingId(null);
    }
  }

  if (loading) {
    return <p className="empty-state">Loading approvals…</p>;
  }

  return (
    <div className="runs">
      <header className="page-header">
        <p className="page-header__eyebrow">EOD desk</p>
        <div className="runs__header">
          <h1 className="page-header__title">Approvals</h1>
          <div className="runs__actions">
            <button
              type="button"
              className="btn"
              onClick={() => void loadApprovals()}
            >
              Refresh
            </button>
          </div>
        </div>
      </header>

      {error ? <p className="alert" role="alert">{error}</p> : null}

      <section className="panel">
        {approvals.length === 0 ? (
          <p className="empty-state">
            No pending approvals. Wait for the next EOD run.
          </p>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Symbol</th>
                <th scope="col">Side</th>
                <th scope="col">Qty</th>
                <th scope="col">Breach reasons</th>
                <th scope="col">Note</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {approvals.map((item) => {
                const busy = decidingId === item.id;
                return (
                  <tr key={item.id}>
                    <td>{item.symbol}</td>
                    <td>{item.side}</td>
                    <td>{item.qty}</td>
                    <td>
                      {item.breach_reasons.length === 0
                        ? "—"
                        : item.breach_reasons.join(", ")}
                    </td>
                    <td>
                      <textarea
                        aria-label={`Note for approval #${item.id}`}
                        rows={2}
                        value={notes[item.id] ?? ""}
                        disabled={busy}
                        onChange={(e) =>
                          setNotes((prev) => ({
                            ...prev,
                            [item.id]: e.target.value,
                          }))
                        }
                      />
                    </td>
                    <td>
                      <div className="runs__actions">
                        <button
                          type="button"
                          className="btn btn--primary"
                          disabled={busy}
                          onClick={() => void handleDecide(item.id, "approved")}
                        >
                          Approve #{item.id}
                        </button>
                        <button
                          type="button"
                          className="btn btn--danger"
                          disabled={busy}
                          onClick={() => void handleDecide(item.id, "rejected")}
                        >
                          Reject #{item.id}
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

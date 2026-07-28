"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { RunStatusBadge } from "@/components/RunStatusBadge";
import { api } from "@/lib/api";
import type { RunListItem, RunTriggerResponse } from "@/lib/types";

export default function RunsPage() {
  const router = useRouter();
  const [runs, setRuns] = useState<RunListItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [runTriggerLoading, setRunTriggerLoading] = useState(false);
  const [runTriggerError, setRunTriggerError] = useState<string | null>(null);

  const loadRuns = useCallback(async () => {
    const response = await api.get<RunListItem[]>("/api/v1/runs");
    setRuns(response);
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<RunListItem[]>("/api/v1/runs");
        if (!cancelled) {
          setRuns(response);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load runs");
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

  async function handleRunTrigger() {
    setRunTriggerError(null);
    setRunTriggerLoading(true);
    try {
      const response = await api.post<RunTriggerResponse>("/api/v1/runs/trigger", {});
      router.push(`/runs/${response.run_id}`);
    } catch (err) {
      setRunTriggerError(err instanceof Error ? err.message : "Failed to start run");
    } finally {
      setRunTriggerLoading(false);
    }
  }

  async function handleRefresh() {
    setError(null);
    try {
      await loadRuns();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load runs");
    }
  }

  if (loading) {
    return <p className="empty-state">Loading runs…</p>;
  }

  return (
    <div className="runs">
      <header className="page-header">
        <p className="page-header__eyebrow">Trading desk</p>
        <div className="runs__header">
          <h1 className="page-header__title">Runs</h1>
          <div className="runs__actions">
            <button
              type="button"
              className="btn btn--primary"
              disabled={runTriggerLoading}
              onClick={() => void handleRunTrigger()}
            >
              {runTriggerLoading ? "Starting…" : "Run now"}
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => void handleRefresh()}
            >
              Refresh
            </button>
          </div>
        </div>
      </header>

      {error ? <p className="alert" role="alert">{error}</p> : null}
      {runTriggerError ? <p className="alert" role="alert">{runTriggerError}</p> : null}

      <section className="panel">
        {runs.length === 0 ? (
          <p className="empty-state">No runs yet</p>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Run</th>
                <th scope="col">Trade date</th>
                <th scope="col">Trigger</th>
                <th scope="col">Strategy</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id}>
                  <td>
                    <Link href={`/runs/${run.id}`}>#{run.id}</Link>
                  </td>
                  <td>{run.trade_date}</td>
                  <td>{run.trigger || "—"}</td>
                  <td>{run.strategy_name || "—"}</td>
                  <td>
                    <RunStatusBadge status={run.status} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { RunStatusBadge } from "@/components/RunStatusBadge";
import { api } from "@/lib/api";
import type { EodRunResponse, RunListItem } from "@/lib/types";

export default function RunsPage() {
  const router = useRouter();
  const [runs, setRuns] = useState<RunListItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [eodLoading, setEodLoading] = useState(false);
  const [eodError, setEodError] = useState<string | null>(null);

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

  async function handleRunEod() {
    setEodError(null);
    setEodLoading(true);
    try {
      const response = await api.post<EodRunResponse>("/api/v1/runs/eod", {});
      router.push(`/runs/${response.run_id}`);
    } catch (err) {
      setEodError(err instanceof Error ? err.message : "Failed to start EOD run");
    } finally {
      setEodLoading(false);
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
    return <p>Loading runs…</p>;
  }

  return (
    <div className="runs">
      <div className="runs__header">
        <h1 className="runs__title">Runs</h1>
        <div className="runs__actions">
          <button type="button" disabled={eodLoading} onClick={() => void handleRunEod()}>
            {eodLoading ? "Starting…" : "Run EOD now"}
          </button>
          <button type="button" onClick={() => void handleRefresh()}>
            Refresh
          </button>
        </div>
      </div>

      {error ? <p role="alert">{error}</p> : null}
      {eodError ? <p role="alert">{eodError}</p> : null}

      <section className="runs__panel">
        {runs.length === 0 ? (
          <p>No runs yet</p>
        ) : (
          <table className="runs__table">
            <thead>
              <tr>
                <th scope="col">Run</th>
                <th scope="col">Trade date</th>
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

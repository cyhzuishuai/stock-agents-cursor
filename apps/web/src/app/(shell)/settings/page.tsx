"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { SettingsResponse } from "@/lib/types";

function formatRiskValue(value: unknown): string {
  if (typeof value === "number") {
    return value.toLocaleString("en-US", { maximumFractionDigits: 4 });
  }
  return String(value);
}

export default function SettingsPage() {
  const [data, setData] = useState<SettingsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<SettingsResponse>("/api/v1/settings");
        if (!cancelled) {
          setData(response);
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load settings",
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

  if (loading) {
    return <p className="empty-state">Loading settings…</p>;
  }

  if (error) {
    return <p className="alert" role="alert">{error}</p>;
  }

  if (!data) {
    return <p className="alert" role="alert">Settings unavailable</p>;
  }

  const riskEntries = Object.entries(data.risk_rules).sort(([a], [b]) =>
    a.localeCompare(b),
  );

  return (
    <div className="settings">
      <header className="page-header">
        <p className="page-header__eyebrow">EOD desk</p>
        <h1 className="page-header__title">Settings</h1>
      </header>

      <div className="settings__sections">
        <section className="panel">
          <h2 className="panel__title">Market data provider</h2>
          <p>{data.market_data_provider || "—"}</p>
        </section>

        <section className="panel">
          <h2 className="panel__title">Watchlist</h2>
          {data.watchlist.length === 0 ? (
            <p className="empty-state">No symbols configured</p>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th scope="col">Symbol</th>
                </tr>
              </thead>
              <tbody>
                {data.watchlist.map((symbol) => (
                  <tr key={symbol}>
                    <td>{symbol}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>

        <section className="panel">
          <h2 className="panel__title">Risk rules</h2>
          {riskEntries.length === 0 ? (
            <p className="empty-state">No risk rules configured</p>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th scope="col">Rule</th>
                  <th scope="col">Value</th>
                </tr>
              </thead>
              <tbody>
                {riskEntries.map(([key, value]) => (
                  <tr key={key}>
                    <td>{key}</td>
                    <td>{formatRiskValue(value)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </div>
    </div>
  );
}

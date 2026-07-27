"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import type {
  ExecutionMode,
  SettingsResponse,
  Strategy,
  StrategyWriteBody,
} from "@/lib/types";

function formatRiskValue(value: unknown): string {
  if (typeof value === "number") {
    return value.toLocaleString("en-US", { maximumFractionDigits: 4 });
  }
  return String(value);
}

function formatScheduleSummary(strategy: Strategy): string {
  return `Pre-open ${strategy.pre_open_minutes}m · every ${strategy.intraday_every_minutes}m ${strategy.intraday_start_et}–${strategy.intraday_end_et} ET`;
}

const emptyForm = (): StrategyWriteBody => ({
  name: "",
  description: "",
  pre_open_minutes: 10,
  intraday_every_minutes: 60,
  intraday_start_et: "10:00",
  intraday_end_et: "15:00",
  execution_mode: "auto_reject_breaches",
});

function formFromStrategy(strategy: Strategy): StrategyWriteBody {
  return {
    name: strategy.name,
    description: strategy.description,
    pre_open_minutes: strategy.pre_open_minutes,
    intraday_every_minutes: strategy.intraday_every_minutes,
    intraday_start_et: strategy.intraday_start_et,
    intraday_end_et: strategy.intraday_end_et,
    execution_mode: strategy.execution_mode,
  };
}

function validateIntradayEveryMinutes(value: number): string | null {
  if (value < 0) {
    return "intraday_every_minutes must be >= 0";
  }
  if (value > 0 && value < 15) {
    return "intraday_every_minutes must be 0 or >= 15";
  }
  return null;
}

function StrategiesPanel() {
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState<StrategyWriteBody>(emptyForm);
  const [saving, setSaving] = useState(false);

  const loadStrategies = useCallback(async () => {
    const response = await api.get<Strategy[]>("/api/v1/strategies");
    setStrategies(response);
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<Strategy[]>("/api/v1/strategies");
        if (!cancelled) {
          setStrategies(response);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load strategies",
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

  function openCreate() {
    setEditingId(null);
    setForm(emptyForm());
    setShowCreate(true);
    setError(null);
  }

  function openEdit(strategy: Strategy) {
    setShowCreate(false);
    setEditingId(strategy.id);
    setForm(formFromStrategy(strategy));
    setError(null);
  }

  function closeForm() {
    setShowCreate(false);
    setEditingId(null);
    setForm(emptyForm());
  }

  function updateField<K extends keyof StrategyWriteBody>(
    key: K,
    value: StrategyWriteBody[K],
  ) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    const body: StrategyWriteBody = {
      name: form.name.trim(),
      description: form.description,
      pre_open_minutes: Number(form.pre_open_minutes),
      intraday_every_minutes: Number(form.intraday_every_minutes),
      intraday_start_et: form.intraday_start_et.trim(),
      intraday_end_et: form.intraday_end_et.trim(),
      execution_mode: form.execution_mode,
    };
    const intradayError = validateIntradayEveryMinutes(body.intraday_every_minutes);
    if (intradayError) {
      setError(intradayError);
      setSaving(false);
      return;
    }
    try {
      if (editingId !== null) {
        await api.patch<Strategy>(`/api/v1/strategies/${editingId}`, body);
      } else {
        await api.post<Strategy>("/api/v1/strategies", body);
      }
      await loadStrategies();
      closeForm();
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to save strategy",
      );
    } finally {
      setSaving(false);
    }
  }

  async function handleActivate(id: number) {
    setError(null);
    try {
      await api.post<Strategy>(`/api/v1/strategies/${id}/activate`);
      await loadStrategies();
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to activate strategy",
      );
    }
  }

  async function handleDelete(strategy: Strategy) {
    if (strategy.is_system_default || strategy.is_active) return;
    if (!window.confirm(`Delete strategy "${strategy.name}"?`)) return;
    setError(null);
    try {
      await api.delete(`/api/v1/strategies/${strategy.id}`);
      await loadStrategies();
      if (editingId === strategy.id) {
        closeForm();
      }
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to delete strategy",
      );
    }
  }

  const formVisible = showCreate || editingId !== null;

  return (
    <section className="panel">
      <div className="settings__panel-header">
        <h2 className="panel__title">Strategies</h2>
        <button type="button" className="btn" onClick={openCreate}>
          Create
        </button>
      </div>

      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}

      {loading ? (
        <p className="empty-state">Loading strategies…</p>
      ) : strategies.length === 0 ? (
        <p className="empty-state">No strategies configured</p>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Schedule</th>
              <th scope="col">Mode</th>
              <th scope="col">Active</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {strategies.map((strategy) => {
              const canDelete =
                !strategy.is_system_default && !strategy.is_active;
              return (
                <tr key={strategy.id}>
                  <td>
                    {strategy.name}
                    {strategy.is_system_default ? (
                      <span className="settings__badge"> system</span>
                    ) : null}
                  </td>
                  <td>{formatScheduleSummary(strategy)}</td>
                  <td>{strategy.execution_mode}</td>
                  <td>{strategy.is_active ? "Yes" : "No"}</td>
                  <td>
                    <div className="settings__row-actions">
                      {!strategy.is_active ? (
                        <button
                          type="button"
                          className="btn btn--primary"
                          onClick={() => void handleActivate(strategy.id)}
                        >
                          Activate
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="btn"
                        onClick={() => openEdit(strategy)}
                      >
                        Edit
                      </button>
                      {canDelete ? (
                        <button
                          type="button"
                          className="btn btn--danger"
                          onClick={() => void handleDelete(strategy)}
                        >
                          Delete
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

      {formVisible ? (
        <form className="settings__form" onSubmit={handleSubmit}>
          <h3 className="settings__form-title">
            {editingId !== null ? "Edit strategy" : "Create strategy"}
          </h3>
          <div className="settings__form-grid">
            <div className="settings__field">
              <label htmlFor="strategy-name">Name</label>
              <input
                id="strategy-name"
                name="name"
                type="text"
                value={form.name}
                onChange={(e) => updateField("name", e.target.value)}
                required
              />
            </div>
            <div className="settings__field settings__field--wide">
              <label htmlFor="strategy-description">Description</label>
              <input
                id="strategy-description"
                name="description"
                type="text"
                value={form.description}
                onChange={(e) => updateField("description", e.target.value)}
              />
            </div>
            <div className="settings__field">
              <label htmlFor="strategy-pre-open">Pre-open minutes</label>
              <input
                id="strategy-pre-open"
                name="pre_open_minutes"
                type="number"
                min={0}
                value={form.pre_open_minutes}
                onChange={(e) =>
                  updateField("pre_open_minutes", Number(e.target.value))
                }
                required
              />
            </div>
            <div className="settings__field">
              <label htmlFor="strategy-every">Intraday every minutes</label>
              <input
                id="strategy-every"
                name="intraday_every_minutes"
                type="number"
                min={0}
                step={1}
                value={form.intraday_every_minutes}
                onChange={(e) =>
                  updateField("intraday_every_minutes", Number(e.target.value))
                }
                required
              />
              <p className="settings__field-hint">
                0 disables intraday runs; otherwise use 15 or more minutes.
              </p>
            </div>
            <div className="settings__field">
              <label htmlFor="strategy-start">Intraday start (ET)</label>
              <input
                id="strategy-start"
                name="intraday_start_et"
                type="text"
                placeholder="10:00"
                value={form.intraday_start_et}
                onChange={(e) =>
                  updateField("intraday_start_et", e.target.value)
                }
                required
              />
            </div>
            <div className="settings__field">
              <label htmlFor="strategy-end">Intraday end (ET)</label>
              <input
                id="strategy-end"
                name="intraday_end_et"
                type="text"
                placeholder="15:00"
                value={form.intraday_end_et}
                onChange={(e) => updateField("intraday_end_et", e.target.value)}
                required
              />
            </div>
            <div className="settings__field">
              <label htmlFor="strategy-mode">Execution mode</label>
              <select
                id="strategy-mode"
                name="execution_mode"
                value={form.execution_mode}
                onChange={(e) =>
                  updateField(
                    "execution_mode",
                    e.target.value as ExecutionMode,
                  )
                }
              >
                <option value="auto_reject_breaches">
                  auto_reject_breaches
                </option>
                <option value="require_approval">require_approval</option>
              </select>
            </div>
          </div>
          <div className="settings__row-actions">
            <button
              type="submit"
              className="btn btn--primary"
              disabled={saving}
            >
              {saving ? "Saving…" : "Save"}
            </button>
            <button
              type="button"
              className="btn btn--ghost"
              onClick={closeForm}
              disabled={saving}
            >
              Cancel
            </button>
          </div>
        </form>
      ) : null}
    </section>
  );
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
    return (
      <p className="alert" role="alert">
        {error}
      </p>
    );
  }

  if (!data) {
    return (
      <p className="alert" role="alert">
        Settings unavailable
      </p>
    );
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

        <StrategiesPanel />
      </div>
    </div>
  );
}

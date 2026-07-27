"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import type {
  ExecutionMode,
  SettingsResponse,
  Strategy,
  StrategyWriteBody,
  WatchlistItem,
  SymbolSearchResult,
} from "@/lib/types";

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
                <option value="bypass_risk">
                  bypass_risk (skip Go risk)
                </option>
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

  const reloadSettings = useCallback(async () => {
    const response = await api.get<SettingsResponse>("/api/v1/settings");
    setData(response);
  }, []);

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

        <WatchlistPanel
          items={data.watchlist}
          onChanged={reloadSettings}
        />

        <RiskPanel
          riskRules={data.risk_rules}
          onChanged={reloadSettings}
        />

        <StrategiesPanel />
      </div>
    </div>
  );
}

function WatchlistPanel({
  items,
  onChanged,
}: {
  items: WatchlistItem[];
  onChanged: () => Promise<void>;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SymbolSearchResult[]>([]);
  const [searchOpen, setSearchOpen] = useState(false);
  const [panelError, setPanelError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setResults([]);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      try {
        const found = await api.get<SymbolSearchResult[]>(
          `/api/v1/symbols/search?q=${encodeURIComponent(q)}`,
        );
        if (!cancelled) {
          setResults(found);
          setSearchOpen(true);
          setPanelError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setPanelError(
            err instanceof Error ? err.message : "Symbol search failed",
          );
        }
      }
    }, 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query]);

  async function addSymbol(symbol: string) {
    if (items.some((item) => item.symbol === symbol)) {
      setPanelError(`${symbol} is already on the watchlist`);
      setSearchOpen(false);
      return;
    }
    setBusy(true);
    setPanelError(null);
    try {
      await api.post("/api/v1/settings/watchlist", {
        symbol,
        can_hold: true,
      });
      setQuery("");
      setResults([]);
      setSearchOpen(false);
      await onChanged();
    } catch (err) {
      setPanelError(err instanceof Error ? err.message : "Failed to add symbol");
    } finally {
      setBusy(false);
    }
  }

  async function toggleCanHold(symbol: string, canHold: boolean) {
    setBusy(true);
    setPanelError(null);
    try {
      await api.patch(`/api/v1/settings/watchlist/${encodeURIComponent(symbol)}`, {
        can_hold: canHold,
      });
      await onChanged();
    } catch (err) {
      setPanelError(
        err instanceof Error ? err.message : "Failed to update can_hold",
      );
    } finally {
      setBusy(false);
    }
  }

  async function removeSymbol(symbol: string) {
    if (!window.confirm(`Remove ${symbol} from watchlist?`)) {
      return;
    }
    setBusy(true);
    setPanelError(null);
    try {
      await api.delete(`/api/v1/settings/watchlist/${encodeURIComponent(symbol)}`);
      await onChanged();
    } catch (err) {
      setPanelError(
        err instanceof Error ? err.message : "Failed to remove symbol",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel">
      <h2 className="panel__title">Watchlist</h2>
      {panelError ? (
        <p className="alert" role="alert">
          {panelError}
        </p>
      ) : null}
      <div className="settings__search">
        <label className="settings__field">
          <span>Search symbols</span>
          <input
            type="search"
            value={query}
            disabled={busy}
            placeholder="e.g. AAPL"
            onChange={(e) => setQuery(e.target.value)}
            onFocus={() => {
              if (results.length > 0) setSearchOpen(true);
            }}
          />
        </label>
        {searchOpen && results.length > 0 ? (
          <ul className="settings__search-results" role="listbox">
            {results.map((item) => (
              <li key={item.symbol}>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void addSymbol(item.symbol)}
                >
                  {item.symbol}
                  {item.name ? ` · ${item.name}` : ""}
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      {items.length === 0 ? (
        <p className="empty-state">No symbols configured</p>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Symbol</th>
              <th scope="col">可持仓</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.symbol}>
                <td>{item.symbol}</td>
                <td>
                  <input
                    type="checkbox"
                    checked={item.can_hold}
                    disabled={busy}
                    aria-label={`可持仓 ${item.symbol}`}
                    onChange={(e) =>
                      void toggleCanHold(item.symbol, e.target.checked)
                    }
                  />
                </td>
                <td>
                  <div className="settings__row-actions">
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => void removeSymbol(item.symbol)}
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function RiskPanel({
  riskRules,
  onChanged,
}: {
  riskRules: Record<string, unknown>;
  onChanged: () => Promise<void>;
}) {
  const entries = Object.entries(riskRules).sort(([a], [b]) =>
    a.localeCompare(b),
  );
  const [drafts, setDrafts] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      entries.map(([key, value]) => [key, String(value ?? "")]),
    ),
  );
  const [panelError, setPanelError] = useState<string | null>(null);
  const [savingKey, setSavingKey] = useState<string | null>(null);

  useEffect(() => {
    setDrafts(
      Object.fromEntries(
        Object.entries(riskRules)
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([key, value]) => [key, String(value ?? "")]),
      ),
    );
  }, [riskRules]);

  async function saveKey(key: string) {
    const raw = drafts[key] ?? "";
    const value = Number(raw);
    if (!Number.isFinite(value)) {
      setPanelError(`${key}: value must be a finite number`);
      return;
    }
    setSavingKey(key);
    setPanelError(null);
    try {
      await api.patch(`/api/v1/settings/risk/${encodeURIComponent(key)}`, {
        value,
      });
      await onChanged();
    } catch (err) {
      setPanelError(err instanceof Error ? err.message : "Failed to save risk");
    } finally {
      setSavingKey(null);
    }
  }

  return (
    <section className="panel">
      <h2 className="panel__title">Risk rules</h2>
      {panelError ? (
        <p className="alert" role="alert">
          {panelError}
        </p>
      ) : null}
      {entries.length === 0 ? (
        <p className="empty-state">No risk rules configured</p>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Rule</th>
              <th scope="col">Value</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {entries.map(([key]) => (
              <tr key={key}>
                <td>{key}</td>
                <td>
                  <input
                    type="number"
                    step="any"
                    value={drafts[key] ?? ""}
                    aria-label={`Risk value ${key}`}
                    disabled={savingKey === key}
                    onChange={(e) =>
                      setDrafts((prev) => ({ ...prev, [key]: e.target.value }))
                    }
                  />
                </td>
                <td>
                  <div className="settings__row-actions">
                    <button
                      type="button"
                      disabled={savingKey === key}
                      onClick={() => void saveKey(key)}
                    >
                      Save
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

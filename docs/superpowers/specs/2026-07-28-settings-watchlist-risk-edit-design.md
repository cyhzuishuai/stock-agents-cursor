# Settings Watchlist & Risk Edit Design

**Date:** 2026-07-28  
**Status:** Approved for implementation planning  
**Approach:** Settings write APIs + `can_hold` + Alpaca symbol search + buy-side workflow gate

## Goal

Make the Settings page able to:

1. **Watchlist** — search US stock symbols (via Alpaca asset search), add/remove symbols, and toggle **可持仓 (`can_hold`)** per symbol with a checkbox.
2. **Risk** — edit values of **existing** risk rules only (no create/delete of rule keys).
3. Enforce `can_hold` in the EOD workflow so unchecked symbols cannot receive new buy fills.

## Non-goals

- Creating or deleting risk rule keys
- Changing Strategies CRUD
- Changing agent `watchlist: string[]` contract shape
- Splitting observe vs tradable into separate tables

## Current state

- `GET /api/v1/settings` is read-only: `watchlist: string[]`, `risk_rules: object`
- `WatchlistSymbol` has only `ID` + `Symbol`
- No write endpoints for watchlist/risk; no symbol search API
- EOD runner loads all watchlist symbols for agents; no holdability gate on buys

## Data model

### `WatchlistSymbol`

| Field | Type | Notes |
|-------|------|--------|
| `id` | uint | PK |
| `symbol` | string | unique index, uppercase ticker |
| `can_hold` | bool | default `true`; seed rows remain holdable |

GORM AutoMigrate adds the column; existing rows get default `true`.

### `RiskRuleConfig`

Unchanged. Updates only mutate `value_float` for an existing `key`.

## API

All endpoints below require JWT (`Authorization: Bearer`).

### Settings read (breaking shape change)

`GET /api/v1/settings`

```json
{
  "watchlist": [
    { "symbol": "AAPL", "can_hold": true }
  ],
  "risk_rules": {
    "max_order_notional": 10000,
    "max_single_name_weight": 0.2,
    "min_cash_ratio": 0.1
  },
  "market_data_provider": "free"
}
```

Update `packages/contracts/api_dto.md` and frontend `SettingsResponse` accordingly.

### Watchlist writes

| Method | Path | Body | Behavior |
|--------|------|------|----------|
| `POST` | `/api/v1/settings/watchlist` | `{ "symbol": "AAPL", "can_hold"?: true }` | Normalize symbol to uppercase; create; missing `can_hold` defaults `true`; duplicate → `409` |
| `PATCH` | `/api/v1/settings/watchlist/:symbol` | `{ "can_hold": false }` | Update flag; unknown symbol → `404` |
| `DELETE` | `/api/v1/settings/watchlist/:symbol` | — | Remove row; unknown → `404` |

### Risk write

| Method | Path | Body | Behavior |
|--------|------|------|----------|
| `PATCH` | `/api/v1/settings/risk/:key` | `{ "value": 0.15 }` | Update existing key only; unknown key → `404` (never insert); `value` must be a finite number |

### Symbol search

| Method | Path | Behavior |
|--------|------|----------|
| `GET` | `/api/v1/symbols/search?q=` | Alpaca `us_equity` asset search + stock snapshots; return `[{ symbol, name, price?, change?, change_pct?, asset_class? }]` |

Rules:

- Empty or whitespace `q` → `200` with `[]`
- Debounce is client-side; server caps results at 10 (symbol prefix, then name contains)
- Missing Alpaca credentials → `503` (`alpaca not configured`)
- Asset list fetch failure → `502`; snapshot failure still returns symbol/name with null quotes
- Superseded Yahoo proxy; see `2026-07-28-alpaca-symbol-search-design.md`

## Frontend (Settings page)

File: `apps/web/src/app/(shell)/settings/page.tsx` (and tests / types / CSS as needed).

### Watchlist panel

- Search input → debounced `GET /api/v1/symbols/search?q=`
- Result cards: avatar initials, ticker, price, colored change, name; click → `POST` add (default `can_hold: true`); if already present, show inline message
- Table columns: Symbol | 可持仓 (checkbox) | Delete
- Checkbox change → immediate `PATCH`
- Delete → `window.confirm` then `DELETE`
- Panel-local loading/error states

### Risk panel

- Table: Rule | Value (number input) | Save
- No “add rule” UI
- Save → `PATCH /api/v1/settings/risk/:key`
- Client validation: finite number before submit

### Unchanged

- Market data provider: read-only
- Strategies panel: unchanged

## Workflow enforcement

File: `services/api/internal/workflow/runner.go` (and tests).

1. Agents continue to receive the **full** watchlist as `string[]` (all symbols, including `can_hold=false`) so research/observe still runs.
2. After portfolio proposals are parsed and before auto-execute / approval path, apply a holdability gate:
   - Load `can_hold` map for proposal symbols
   - If `side == "buy"` and `can_hold` is false (or symbol not on watchlist as holdable): set proposal status to rejected with breach reason `not_holdable`; do not fill; do not create approval
   - If `side == "sell"`: do **not** block (allow reducing/exiting existing positions)
3. Prefer applying the gate in the same loop that evaluates risk / applies fills so rejected buys are persisted consistently with other rejections.

Agent run request schema stays `watchlist: string[]` — no agent contract change in this work.

## Error handling

| Case | Response |
|------|----------|
| Duplicate watchlist symbol | `409` |
| Unknown watchlist symbol on PATCH/DELETE | `404` |
| Unknown risk key | `404` |
| Invalid risk value (non-number / NaN / Inf) | `400` |
| Symbol search upstream failure | `502` |

## Testing

- **API:** add/remove/patch watchlist; risk patch found/not found; search with mocked HTTP
- **Workflow:** buy on `can_hold=false` → rejected `not_holdable`; sell still executes
- **Frontend:** settings tests for search-add, checkbox toggle, risk edit (existing vitest patterns)

## Files likely touched

- `services/api/internal/models/watchlist.go`
- `services/api/internal/db/seed.go` (ensure `can_hold` default)
- `services/api/internal/httpserver/` (settings handlers, search, router)
- `services/api/internal/workflow/runner.go` (+ tests)
- `packages/contracts/api_dto.md`
- `apps/web/src/lib/types.ts`
- `apps/web/src/app/(shell)/settings/page.tsx` (+ `page.test.tsx`)
- `apps/web/src/app/globals.css` (minimal search dropdown styles if needed)

## Success criteria

- User can search and add symbols to watchlist from Settings
- User can toggle 可持仓 via checkbox; value persists and reloads correctly
- User can edit existing risk rule values; cannot create new keys via UI or API
- EOD buy proposals for non-holdable symbols are rejected with `not_holdable`
- Existing sell path for positions remains available

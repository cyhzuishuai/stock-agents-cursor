# Alpaca Symbol Search + Settings Cards — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation  
**Related:** `2026-07-28-settings-watchlist-risk-edit-design.md` (Yahoo search superseded for this endpoint), `2026-07-28-alpaca-paper-authority-design.md`

## 1. Goal

Fix Settings watchlist symbol search (`GET /api/v1/symbols/search`) which currently fails with **502** because Yahoo Finance search returns **403**. Replace upstream with **Alpaca**, return quote fields, and render Yahoo-Finance–like result cards in Settings only.

### Success criteria

- Searching `AAAA` (or other active US equity/ETF) returns results when Alpaca keys are configured (no Yahoo dependency).
- Each result includes `symbol`, `name`, optional `price` / `change` / `change_pct`.
- Settings UI shows card rows: avatar initials, ticker, price, colored change, full name; footer “End of results”.
- Clicking a card still adds the symbol to the watchlist.

### Out of scope

- Standalone global search page
- Real logo images (use 2-letter initials)
- Changing watchlist CRUD semantics

## 2. Decisions (locked)

| Topic | Choice |
|-------|--------|
| Upstream | Alpaca (not Yahoo) |
| UI surface | Settings watchlist search only |
| Asset types | Alpaca `us_equity` active assets (stocks + ETFs) |
| Match | Symbol prefix first, then name substring; max 10 |
| Quotes | Alpaca stock snapshots; nulls OK if snapshot missing |
| Missing keys | `503` with clear `alpaca not configured` (or equivalent) |

## 3. Backend

### 3.1 Cache

- Lazy-load `GET {ALPACA_BASE_URL}/v2/assets?status=active&asset_class=us_equity` into memory.
- Refresh TTL ≈ **1 hour** (or on first search after expiry).
- Thread-safe read; singleflight refresh.

### 3.2 Search handler

`GET /api/v1/symbols/search?q=`

1. Trim `q`; empty → `[]`.
2. If Alpaca credentials missing → `503`.
3. Ensure asset cache warm.
4. Filter/rank matches (case-insensitive):
   - Prefer `strings.HasPrefix(symbol, q)`
   - Then `strings.Contains(name, q)`
   - Cap 10.
5. Batch `GET {ALPACA_DATA_BASE_URL}/v2/stocks/snapshots?symbols=...` (comma-separated).
6. Map latest price and daily change vs previous close / daily open as available from snapshot payload.
7. Return JSON array:

```json
[
  {
    "symbol": "AAAA",
    "name": "Amplius Aggressive Asset Allocation ETF",
    "price": 29.86,
    "change": -0.006,
    "change_pct": -0.02,
    "asset_class": "us_equity"
  }
]
```

Remove Yahoo client path from this handler.

### 3.3 Tests

- Mock HTTP for assets + snapshots.
- Empty `q`, match ranking, missing creds → 503, snapshot partial failure still returns symbol/name.

## 4. Frontend (Settings)

- Extend `SymbolSearchResult` with optional `price`, `change`, `change_pct`.
- Replace plain list buttons with result cards (layout per §2 of brainstorming).
- Format currency USD; change red if `< 0`, green if `> 0`.
- Show “End of results” when `results.length > 0`.
- Keep 300ms debounce and add-on-click behavior.

## 5. Docs

- Note in settings design / deploy README: symbol search uses Alpaca, not Yahoo.
- Update any “Yahoo search proxy” wording that is now wrong for this endpoint.

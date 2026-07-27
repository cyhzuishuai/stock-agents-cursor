# Settings Watchlist & Risk Edit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Settings page can search/add watchlist symbols, toggle `can_hold`, edit existing risk values; EOD rejects buys for non-holdable symbols.

**Architecture:** Extend `WatchlistSymbol` with `can_hold`; add JWT write APIs under `/api/v1/settings/*` plus Yahoo-proxied `GET /api/v1/symbols/search`; update Settings GET shape to object array; gate buys in the workflow proposal loop with breach `not_holdable`; wire editable Watchlist/Risk panels on the Settings page.

**Tech Stack:** Go (Gin, Gorm), Yahoo Finance search HTTP, Next.js App Router, Vitest, SQLite API tests.

**Spec:** `docs/superpowers/specs/2026-07-28-settings-watchlist-risk-edit-design.md`

## Global Constraints

- Risk: PATCH existing keys only — never insert new keys
- Agents still receive full watchlist as `string[]` (no agent schema change)
- Buy + `can_hold=false` (or not holdable) → reject with `not_holdable`; sell never blocked by this gate
- Symbol search uses Yahoo (free path); Alpaca search out of scope
- Do not commit unless the user explicitly asks (Commit steps are optional gates)
- Run relevant Go tests / `cd apps/web && npm test` before claiming a task done

---

## File map

| File | Responsibility |
|------|----------------|
| `services/api/internal/models/watchlist.go` | Add `CanHold` |
| `services/api/internal/db/seed.go` | Seed with `CanHold: true` |
| `services/api/internal/httpserver/handlers_settings.go` | GET shape + watchlist/risk writes |
| `services/api/internal/httpserver/handlers_symbols.go` | Yahoo search proxy |
| `services/api/internal/httpserver/router.go` | Register new routes |
| `services/api/internal/httpserver/api_smoke_test.go` | Assert new watchlist shape |
| `services/api/internal/httpserver/handlers_settings_write_test.go` | Watchlist/risk write tests |
| `services/api/internal/httpserver/handlers_symbols_test.go` | Search with mocked transport |
| `services/api/internal/workflow/runner.go` | Holdability gate on buy |
| `services/api/internal/workflow/runner_test.go` | `not_holdable` buy / sell-ok tests |
| `packages/contracts/api_dto.md` | Document writable settings + search |
| `apps/web/src/lib/types.ts` | `WatchlistItem`, search types |
| `apps/web/src/app/(shell)/settings/page.tsx` | Editable panels |
| `apps/web/src/app/(shell)/settings/page.test.tsx` | UI tests |
| `apps/web/src/app/globals.css` | Search dropdown styles |

---

### Task 1: Model `can_hold` + Settings GET shape

**Files:**
- Modify: `services/api/internal/models/watchlist.go`
- Modify: `services/api/internal/db/seed.go`
- Modify: `services/api/internal/httpserver/handlers_settings.go`
- Modify: `services/api/internal/httpserver/api_smoke_test.go`
- Test: `go test ./internal/httpserver/ ./internal/db/ -count=1`

**Interfaces:**
- Produces: `models.WatchlistSymbol.CanHold bool` with `gorm:"not null;default:true" json:"can_hold"`
- Produces: `GET /api/v1/settings` → `watchlist: [{symbol, can_hold}, ...]`

- [ ] **Step 1: Write failing smoke assertion for object-shaped watchlist**

In `api_smoke_test.go` `t.Run("settings", ...)`, replace the length-only check with:

```go
wl, ok := resp["watchlist"].([]any)
if !ok || len(wl) != 2 {
	t.Fatalf("watchlist: got %v", resp["watchlist"])
}
first, ok := wl[0].(map[string]any)
if !ok {
	t.Fatalf("watchlist[0] want object, got %T %v", wl[0], wl[0])
}
if _, ok := first["symbol"].(string); !ok {
	t.Fatalf("watchlist[0].symbol: %v", first["symbol"])
}
if _, ok := first["can_hold"].(bool); !ok {
	t.Fatalf("watchlist[0].can_hold: %v", first["can_hold"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/api && go test ./internal/httpserver/ -run TestOverviewPortfolioRunsSettingsSmoke -count=1`

Expected: FAIL — `watchlist[0] want object` (currently string)

- [ ] **Step 3: Add CanHold to model + seed**

```go
// services/api/internal/models/watchlist.go
package models

type WatchlistSymbol struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Symbol  string `gorm:"uniqueIndex" json:"symbol"`
	CanHold bool   `gorm:"not null;default:true" json:"can_hold"`
}
```

In `seed.go`, when creating watchlist rows, set `CanHold: true` (FirstOrCreate on Symbol still works; default column covers existing DBs after AutoMigrate).

- [ ] **Step 4: Update Settings handler**

```go
watchlist := make([]gin.H, 0, len(symbols))
for _, s := range symbols {
	watchlist = append(watchlist, gin.H{
		"symbol":   s.Symbol,
		"can_hold": s.CanHold,
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd services/api && go test ./internal/httpserver/ ./internal/db/ -count=1`

Expected: PASS

- [ ] **Step 6: Commit (optional — only if user asked)**

```bash
git add services/api/internal/models/watchlist.go services/api/internal/db/seed.go services/api/internal/httpserver/handlers_settings.go services/api/internal/httpserver/api_smoke_test.go
git commit -m "feat: return watchlist items with can_hold on settings GET"
```

---

### Task 2: Watchlist write APIs

**Files:**
- Modify: `services/api/internal/httpserver/handlers_settings.go`
- Modify: `services/api/internal/httpserver/router.go`
- Create: `services/api/internal/httpserver/handlers_settings_write_test.go`
- Test: `go test ./internal/httpserver/ -run 'Watchlist|SettingsWrite' -count=1`

**Interfaces:**
- Produces: `POST /api/v1/settings/watchlist` → `201` `{symbol, can_hold}`
- Produces: `PATCH /api/v1/settings/watchlist/:symbol` → `200` `{symbol, can_hold}`
- Produces: `DELETE /api/v1/settings/watchlist/:symbol` → `200` `{ok: true}`
- Consumes: Task 1 model

- [ ] **Step 1: Write failing tests**

```go
// handlers_settings_write_test.go
package httpserver_test

func TestWatchlistCRUD(t *testing.T) {
	router, _, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, nil) // use same helper as smoke; pass gormDB if required

	// POST MSFT
	body := bytes.NewBufferString(`{"symbol":"msft","can_hold":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/watchlist", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status: got %d body=%s", w.Code, w.Body.String())
	}

	// duplicate → 409
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/settings/watchlist", bytes.NewBufferString(`{"symbol":"MSFT"}`))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate: got %d", w2.Code)
	}

	// PATCH can_hold false
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/watchlist/MSFT", bytes.NewBufferString(`{"can_hold":false}`))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("PATCH: got %d body=%s", w3.Code, w3.Body.String())
	}

	// DELETE
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/watchlist/MSFT", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("DELETE: got %d", w4.Code)
	}

	// DELETE missing → 404
	w5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodDelete, "/api/v1/settings/watchlist/MSFT", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w5, req5)
	if w5.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing: got %d", w5.Code)
	}
}
```

Adapt `bearerToken` signature to match existing smoke helper (`bearerToken(t, secret, gormDB)`).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/api && go test ./internal/httpserver/ -run TestWatchlistCRUD -count=1`

Expected: FAIL — 404 (routes missing)

- [ ] **Step 3: Implement handlers + routes**

```go
// handlers_settings.go additions
type watchlistCreateRequest struct {
	Symbol  string `json:"symbol"`
	CanHold *bool  `json:"can_hold"`
}

type watchlistPatchRequest struct {
	CanHold *bool `json:"can_hold"`
}

func normalizeSymbol(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func (h *API) AddWatchlistSymbol(c *gin.Context) {
	var req watchlistCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	symbol := normalizeSymbol(req.Symbol)
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}
	canHold := true
	if req.CanHold != nil {
		canHold = *req.CanHold
	}
	row := models.WatchlistSymbol{Symbol: symbol, CanHold: canHold}
	err := h.DB.WithContext(c.Request.Context()).Create(&row).Error
	if err != nil {
		// unique violation → 409 (use errors.Is / sqlite message or First check)
		var existing models.WatchlistSymbol
		if h.DB.Where("symbol = ?", symbol).First(&existing).Error == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "symbol already on watchlist"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"symbol": row.Symbol, "can_hold": row.CanHold})
}

func (h *API) PatchWatchlistSymbol(c *gin.Context) {
	symbol := normalizeSymbol(c.Param("symbol"))
	var req watchlistPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.CanHold == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can_hold required"})
		return
	}
	var row models.WatchlistSymbol
	if err := h.DB.WithContext(c.Request.Context()).Where("symbol = ?", symbol).First(&row).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "symbol not found"})
		return
	}
	row.CanHold = *req.CanHold
	if err := h.DB.WithContext(c.Request.Context()).Save(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbol": row.Symbol, "can_hold": row.CanHold})
}

func (h *API) DeleteWatchlistSymbol(c *gin.Context) {
	symbol := normalizeSymbol(c.Param("symbol"))
	res := h.DB.WithContext(c.Request.Context()).Where("symbol = ?", symbol).Delete(&models.WatchlistSymbol{})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "symbol not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

Router (inside `authed`):

```go
authed.POST("/settings/watchlist", api.AddWatchlistSymbol)
authed.PATCH("/settings/watchlist/:symbol", api.PatchWatchlistSymbol)
authed.DELETE("/settings/watchlist/:symbol", api.DeleteWatchlistSymbol)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd services/api && go test ./internal/httpserver/ -run TestWatchlistCRUD -count=1`

Expected: PASS

- [ ] **Step 5: Commit (optional)**

```bash
git add services/api/internal/httpserver/handlers_settings.go services/api/internal/httpserver/router.go services/api/internal/httpserver/handlers_settings_write_test.go
git commit -m "feat: add watchlist create/patch/delete settings APIs"
```

---

### Task 3: Risk PATCH API

**Files:**
- Modify: `services/api/internal/httpserver/handlers_settings.go`
- Modify: `services/api/internal/httpserver/router.go`
- Modify: `services/api/internal/httpserver/handlers_settings_write_test.go`
- Test: `go test ./internal/httpserver/ -run TestRiskPatch -count=1`

**Interfaces:**
- Produces: `PATCH /api/v1/settings/risk/:key` → `200` `{key, value}`; unknown key → `404`; never inserts

- [ ] **Step 1: Write failing test**

```go
func TestRiskPatch(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/risk/max_order_notional",
		bytes.NewBufferString(`{"value":12345}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH existing: got %d body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/risk/does_not_exist",
		bytes.NewBufferString(`{"value":1}`))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("PATCH missing: got %d", w2.Code)
	}

	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/risk/max_order_notional",
		bytes.NewBufferString(`{"value":"nope"}`))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("PATCH bad value: got %d", w3.Code)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL (404 route)**

Run: `cd services/api && go test ./internal/httpserver/ -run TestRiskPatch -count=1`

- [ ] **Step 3: Implement handler**

```go
type riskPatchRequest struct {
	Value *float64 `json:"value"`
}

func (h *API) PatchRiskRule(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	var req riskPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value required as number"})
		return
	}
	v := *req.Value
	if math.IsNaN(v) || math.IsInf(v, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value must be finite"})
		return
	}
	var row models.RiskRuleConfig
	if err := h.DB.WithContext(c.Request.Context()).Where("key = ?", key).First(&row).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "risk key not found"})
		return
	}
	row.ValueFloat = v
	if err := h.DB.WithContext(c.Request.Context()).Save(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": row.Key, "value": row.ValueFloat})
}
```

Router:

```go
authed.PATCH("/settings/risk/:key", api.PatchRiskRule)
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `cd services/api && go test ./internal/httpserver/ -run 'TestRiskPatch|TestWatchlistCRUD|TestOverviewPortfolioRunsSettingsSmoke' -count=1`

- [ ] **Step 5: Commit (optional)**

```bash
git commit -m "feat: allow patching existing risk rule values"
```

---

### Task 4: Symbol search (Yahoo proxy)

**Files:**
- Create: `services/api/internal/httpserver/handlers_symbols.go`
- Create: `services/api/internal/httpserver/handlers_symbols_test.go`
- Modify: `services/api/internal/httpserver/router.go`
- Modify: `services/api/internal/httpserver/api.go` (or wherever `API` struct lives) — add optional `HTTPClient *http.Client` if needed for injection; otherwise use package-level var for test override
- Test: `go test ./internal/httpserver/ -run TestSymbolSearch -count=1`

**Interfaces:**
- Produces: `GET /api/v1/symbols/search?q=` → `[{symbol, name}]` capped at 10; empty `q` → `[]`

- [ ] **Step 1: Write failing test with mocked RoundTripper**

```go
func TestSymbolSearch(t *testing.T) {
	// Install mock transport that returns Yahoo-like JSON for q=aap
	// Assert handler returns [{symbol:AAPL, name:...}]
	// Assert q= empty → []
}
```

Yahoo search URL used by implementation:

`https://query1.finance.yahoo.com/v1/finance/search?q=<q>&quotesCount=10&newsCount=0`

Mock body shape:

```json
{"quotes":[{"symbol":"AAPL","shortname":"Apple Inc.","quoteType":"EQUITY","exchange":"NMS"}]}
```

Filter to prefer `quoteType == "EQUITY"` when present; skip empty symbols.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

```go
// handlers_symbols.go
package httpserver

type SymbolSearchResult struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

func (h *API) SearchSymbols(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusOK, []SymbolSearchResult{})
		return
	}
	client := h.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	// GET Yahoo URL with context, decode quotes, map to results, cap 10
	// on network/decode error → 502
}
```

Ensure `API` struct has `HTTPClient *http.Client` (set in tests via router deps or mutate after `setupAPI` if exposed). Prefer adding field on `API` and setting it in `NewRouter` from deps only if deps already exist; simplest path: package var `var yahooHTTPClient = http.DefaultClient` swapped in tests.

Router:

```go
authed.GET("/symbols/search", api.SearchSymbols)
```

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit (optional)**

```bash
git commit -m "feat: proxy Yahoo symbol search for settings watchlist"
```

---

### Task 5: Workflow `can_hold` buy gate

**Files:**
- Modify: `services/api/internal/workflow/runner.go`
- Modify: `services/api/internal/workflow/runner_test.go`
- Test: `go test ./internal/workflow/ -run 'Holdable|CanHold|not_holdable' -count=1` (and full package)

**Interfaces:**
- Consumes: `WatchlistSymbol.CanHold`
- Produces: buy proposals rejected with `breach_reasons_json` containing `not_holdable`

- [ ] **Step 1: Write failing workflow test**

Pattern after existing runner tests: seed account, watchlist with `AAPL` `CanHold: false`, mock agents returning a buy proposal for AAPL, run EOD, assert proposal status `rejected` and breach JSON includes `not_holdable`.

Second case: `CanHold: false`, proposal `side: sell` with existing position → still fills (or at least not rejected for `not_holdable`).

- [ ] **Step 2: Run — expect FAIL** (buy currently fills)

- [ ] **Step 3: Implement gate**

Add helper:

```go
func (r *Runner) loadCanHoldMap(ctx context.Context) (map[string]bool, error) {
	var rows []models.WatchlistSymbol
	if err := r.DB.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.Symbol] = row.CanHold
	}
	return out, nil
}
```

In `runEODThroughFills`, after proposals are inserted and before the evaluate/fill loop (or at the start of each iteration), load the map once:

```go
canHold, err := r.loadCanHoldMap(ctx)
// ...
for i := range proposals {
	p := &proposals[i]
	if strings.EqualFold(p.Side, "buy") && !canHold[p.Symbol] {
		reasons, _ := json.Marshal([]string{"not_holdable"})
		p.Status = ProposalRejected
		p.BreachReasonsJSON = string(reasons)
		_ = r.DB.WithContext(ctx).Model(p).Updates(map[string]any{
			"status":              ProposalRejected,
			"breach_reasons_json": string(reasons),
		}).Error
		continue
	}
	// existing risk evaluate / fill logic
}
```

Keep `loadWatchlist` returning all symbols for agents unchanged.

- [ ] **Step 4: Run workflow tests — expect PASS**

Run: `cd services/api && go test ./internal/workflow/ -count=1`

- [ ] **Step 5: Commit (optional)**

```bash
git commit -m "feat: reject non-holdable buy proposals in EOD workflow"
```

---

### Task 6: Contracts + frontend types

**Files:**
- Modify: `packages/contracts/api_dto.md`
- Modify: `apps/web/src/lib/types.ts`
- Modify: `apps/web/src/app/(shell)/settings/page.test.tsx` (fixture shape only so existing tests compile)

- [ ] **Step 1: Update api_dto.md**

Replace Settings section with:

```markdown
## Settings
GET /api/v1/settings -> {
  watchlist: [{ symbol, can_hold }],
  risk_rules: object,
  market_data_provider: string
}
POST /api/v1/settings/watchlist { symbol, can_hold? } -> { symbol, can_hold }
PATCH /api/v1/settings/watchlist/:symbol { can_hold } -> { symbol, can_hold }
DELETE /api/v1/settings/watchlist/:symbol -> { ok: true }
PATCH /api/v1/settings/risk/:key { value: number } -> { key, value }
GET /api/v1/symbols/search?q= -> [{ symbol, name }]
```

- [ ] **Step 2: Update types.ts**

```ts
export interface WatchlistItem {
  symbol: string;
  can_hold: boolean;
}

export interface SettingsResponse {
  watchlist: WatchlistItem[];
  risk_rules: Record<string, unknown>;
  market_data_provider: string;
}

export interface SymbolSearchResult {
  symbol: string;
  name: string;
}
```

- [ ] **Step 3: Fix settingsFixture in page.test.tsx**

```ts
const settingsFixture: SettingsResponse = {
  watchlist: [{ symbol: "AAPL", can_hold: true }],
  risk_rules: { max_order_notional: 10000 },
  market_data_provider: "stub",
};
```

- [ ] **Step 4: Typecheck / test compile**

Run: `cd apps/web && npx tsc --noEmit` (or `npm test` if tsc not standalone)

Expected: compile OK (UI still read-only until Task 7)

- [ ] **Step 5: Commit (optional)**

```bash
git commit -m "docs: document writable settings and symbol search DTOs"
```

---

### Task 7: Settings UI — Watchlist + Risk panels

**Files:**
- Modify: `apps/web/src/app/(shell)/settings/page.tsx`
- Modify: `apps/web/src/app/(shell)/settings/page.test.tsx`
- Modify: `apps/web/src/app/globals.css`
- Test: `cd apps/web && npm test -- src/app/(shell)/settings/page.test.tsx`

**Interfaces:**
- Consumes: Task 2–4 APIs + Task 6 types

- [ ] **Step 1: Write failing UI tests**

Add describe block `SettingsPage watchlist and risk`:

1. Renders checkbox for 可持仓; toggling calls `PATCH .../watchlist/AAPL` with `can_hold: false`
2. Typing in search + selecting result calls `POST .../watchlist`
3. Editing risk value + Save calls `PATCH .../risk/max_order_notional`

Extend fetch mock to handle these methods/paths.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement WatchlistPanel**

Extract or inline within `SettingsPage`:

- Local state copy of `data.watchlist` (or lift refresh via `reloadSettings`)
- Search input with ~300ms debounce → `api.get<SymbolSearchResult[]>(\`/api/v1/symbols/search?q=${encodeURIComponent(q)}\`)`
- Dropdown list; on click → POST; refresh list
- Table: symbol, checkbox (`aria-label={`可持仓 ${symbol}`}`), Delete button
- Checkbox onChange → PATCH; Delete → confirm → DELETE

- [ ] **Step 4: Implement RiskPanel**

- For each risk entry: number input + Save button
- On Save → `api.patch(\`/api/v1/settings/risk/${key}\`, { value: Number(input) })`
- Show panel error on failure

Use existing `api` helpers; if `api.patch`/`api.delete` missing, add thin wrappers in `apps/web/src/lib/api.ts` mirroring `post`.

- [ ] **Step 5: Minimal CSS**

```css
.settings__search { position: relative; }
.settings__search-results {
  position: absolute;
  z-index: 5;
  /* list under input; reuse panel colors */
}
```

- [ ] **Step 6: Run frontend tests — expect PASS**

Run: `cd apps/web && npm test -- src/app/(shell)/settings/page.test.tsx`

- [ ] **Step 7: Commit (optional)**

```bash
git commit -m "feat: editable watchlist and risk panels on settings page"
```

---

## Self-review checklist

| Spec requirement | Task |
|------------------|------|
| `can_hold` on model + GET shape | Task 1 |
| Watchlist POST/PATCH/DELETE | Task 2 |
| Risk PATCH existing only | Task 3 |
| Yahoo symbol search | Task 4 |
| Workflow buy gate / sell allowed | Task 5 |
| Contracts + TS types | Task 6 |
| Settings UI search/checkbox/risk edit | Task 7 |

No TBD placeholders. Type names: `WatchlistItem`, `SymbolSearchResult`, `CanHold`, breach `not_holdable` consistent across tasks.

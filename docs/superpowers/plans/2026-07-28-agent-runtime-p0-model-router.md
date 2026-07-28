# P0 Model Router (Doubao primary + MiniMax fallback) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a shared ModelRouter so live LLM calls use Volcengine `Doubao-Smart-Router` first and fall back to MiniMax only on HTTP/network failure, with router metadata visible on tool-loop traces.

**Architecture:** Introduce `stock_agents_common.model_router` that resolves primary/fallback provider configs from env, performs one OpenAI-compatible `POST {base}/chat/completions`, and on transport/HTTP failure retries once with fallback. `ToolLLMClient` and `LLMClient` delegate the HTTP post to the router; `loop.py` records `provider` / `model` / `fallback_used` from the response metadata. Mock mode is unchanged (no router calls).

**Tech Stack:** Python 3, httpx, pytest, existing `stock_agents_common`, Docker Compose env passthrough

**Spec:** `docs/superpowers/specs/2026-07-28-agent-runtime-plan-router-design.md` (§2 P0)

## Global Constraints

- Fallback trigger: HTTP/network failures only (timeouts, connection errors, status ≥ 400) — never switch provider on JSON/schema parse failure
- If `LLM_PRIMARY_*` unset, use existing `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` as single provider (no failover)
- Default primary base: `https://ark.cn-beijing.volces.com/api/v3`; default primary model: `Doubao-Smart-Router` (may be overridden with `ep-...`)
- Do not commit secrets; document placeholder env keys only
- `LLM_MODE=mock` path must keep working without primary/fallback keys
- No plan/act/reflect changes in this PR (P1)

## File map

| File | Responsibility |
|------|----------------|
| `services/agents/common/stock_agents_common/model_router.py` | Provider config, resolve env, POST with optional failover, return body + meta |
| `services/agents/common/tests/test_model_router.py` | Unit tests for resolve + failover |
| `services/agents/common/stock_agents_common/llm_tools.py` | `ToolLLMClient._complete_real` uses router |
| `services/agents/common/stock_agents_common/llm.py` | `LLMClient._complete_real` uses router |
| `services/agents/common/tests/test_llm_tools_mock.py` | Live path + failover assertions |
| `services/agents/common/tests/test_llm_mock.py` | Live path still works via legacy env |
| `services/agents/runtime/app/graphs/loop.py` | Trace `llm` fields include router meta |
| `deploy/docker-compose.yml` | Pass `LLM_PRIMARY_*` / `LLM_FALLBACK_*` |
| `deploy/.env` (local only, not committed if secrets) / README or product note | Document new vars — prefer updating `README.md` env section |

---

### Task 1: ModelRouter — env resolve + single POST

**Files:**
- Create: `services/agents/common/stock_agents_common/model_router.py`
- Create: `services/agents/common/tests/test_model_router.py`

**Interfaces:**
- Produces:
  - `@dataclass ProviderConfig`: `name: str`, `api_key: str`, `base_url: str`, `model: str`
  - `@dataclass RouterResult`: `response: httpx.Response`, `provider: str`, `model: str`, `fallback_used: bool`, `primary_error: str | None`
  - `resolve_providers() -> tuple[ProviderConfig, ProviderConfig | None]`
  - `chat_completions(*, payload: dict, http_client: httpx.Client | None = None, timeout: float = 180.0) -> RouterResult`

- [ ] **Step 1: Write failing tests for resolve + successful primary**

```python
"""Tests for ModelRouter env resolution and failover."""

from __future__ import annotations

import json

import httpx
import pytest

from stock_agents_common.model_router import chat_completions, resolve_providers


@pytest.fixture(autouse=True)
def _clear_llm_env(monkeypatch):
    for key in (
        "LLM_API_KEY",
        "LLM_BASE_URL",
        "LLM_MODEL",
        "LLM_PRIMARY_API_KEY",
        "LLM_PRIMARY_BASE_URL",
        "LLM_PRIMARY_MODEL",
        "LLM_FALLBACK_API_KEY",
        "LLM_FALLBACK_BASE_URL",
        "LLM_FALLBACK_MODEL",
    ):
        monkeypatch.delenv(key, raising=False)


def test_resolve_legacy_single_provider(monkeypatch):
    monkeypatch.setenv("LLM_API_KEY", "legacy-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://api.minimaxi.com/v1")
    monkeypatch.setenv("LLM_MODEL", "MiniMax-M3")
    primary, fallback = resolve_providers()
    assert primary.api_key == "legacy-key"
    assert primary.base_url == "https://api.minimaxi.com/v1"
    assert primary.model == "MiniMax-M3"
    assert fallback is None


def test_resolve_primary_and_fallback(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://api.minimaxi.com/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")
    primary, fallback = resolve_providers()
    assert primary.api_key == "ark-key"
    assert primary.base_url == "https://ark.cn-beijing.volces.com/api/v3"
    assert primary.model == "Doubao-Smart-Router"
    assert fallback is not None
    assert fallback.api_key == "mm-key"
    assert fallback.model == "MiniMax-M3"


def test_chat_completions_uses_primary(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://ark.example/api/v3/chat/completions"
        assert request.headers["authorization"] == "Bearer ark-key"
        body = json.loads(request.content)
        assert body["model"] == "Doubao-Smart-Router"
        return httpx.Response(200, json={"choices": [{"message": {"content": "{}"}}]})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = chat_completions(payload={"messages": []}, http_client=client)
    assert result.fallback_used is False
    assert result.provider == "primary"
    assert result.model == "Doubao-Smart-Router"
    assert result.response.status_code == 200
```

- [ ] **Step 2: Run tests — expect import / fail**

Run (from repo, with common package on `PYTHONPATH` as existing tests do):

```powershell
cd services/agents/common
python -m pytest tests/test_model_router.py -v
```

Expected: FAIL (`ModuleNotFoundError: model_router` or similar)

- [ ] **Step 3: Implement `model_router.py` (resolve + primary-only POST)**

```python
"""OpenAI-compatible LLM provider routing with optional HTTP failover."""

from __future__ import annotations

import os
from dataclasses import dataclass

import httpx

DEFAULT_PRIMARY_BASE = "https://ark.cn-beijing.volces.com/api/v3"
DEFAULT_PRIMARY_MODEL = "Doubao-Smart-Router"
DEFAULT_LEGACY_BASE = "https://api.openai.com/v1"
DEFAULT_LEGACY_MODEL = "gpt-4o-mini"


@dataclass(frozen=True)
class ProviderConfig:
    name: str
    api_key: str
    base_url: str
    model: str


@dataclass(frozen=True)
class RouterResult:
    response: httpx.Response
    provider: str
    model: str
    fallback_used: bool
    primary_error: str | None = None


def _strip(value: str | None) -> str:
    return (value or "").strip()


def resolve_providers() -> tuple[ProviderConfig, ProviderConfig | None]:
    primary_key = _strip(os.environ.get("LLM_PRIMARY_API_KEY"))
    if primary_key:
        primary = ProviderConfig(
            name="primary",
            api_key=primary_key,
            base_url=_strip(os.environ.get("LLM_PRIMARY_BASE_URL")) or DEFAULT_PRIMARY_BASE,
            model=_strip(os.environ.get("LLM_PRIMARY_MODEL")) or DEFAULT_PRIMARY_MODEL,
        )
        primary = ProviderConfig(
            name=primary.name,
            api_key=primary.api_key,
            base_url=primary.base_url.rstrip("/"),
            model=primary.model,
        )
        fb_key = _strip(os.environ.get("LLM_FALLBACK_API_KEY"))
        fallback: ProviderConfig | None = None
        if fb_key:
            fallback = ProviderConfig(
                name="fallback",
                api_key=fb_key,
                base_url=(_strip(os.environ.get("LLM_FALLBACK_BASE_URL")) or DEFAULT_LEGACY_BASE).rstrip("/"),
                model=_strip(os.environ.get("LLM_FALLBACK_MODEL")) or DEFAULT_LEGACY_MODEL,
            )
        return primary, fallback

    legacy_key = _strip(os.environ.get("LLM_API_KEY"))
    if not legacy_key:
        raise ValueError("LLM_API_KEY or LLM_PRIMARY_API_KEY is required when LLM_MODE is not mock")
    primary = ProviderConfig(
        name="primary",
        api_key=legacy_key,
        base_url=(_strip(os.environ.get("LLM_BASE_URL")) or DEFAULT_LEGACY_BASE).rstrip("/"),
        model=_strip(os.environ.get("LLM_MODEL")) or DEFAULT_LEGACY_MODEL,
    )
    return primary, None


def _post(
    provider: ProviderConfig,
    payload: dict,
    *,
    http_client: httpx.Client | None,
    timeout: float,
) -> httpx.Response:
    body = {**payload, "model": provider.model}
    headers = {"Authorization": f"Bearer {provider.api_key}"}
    url = f"{provider.base_url}/chat/completions"
    if http_client is not None:
        return http_client.post(url, headers=headers, json=body)
    with httpx.Client(timeout=timeout) as client:
        return client.post(url, headers=headers, json=body)


def _is_http_failure(exc: BaseException | None, response: httpx.Response | None) -> str | None:
    if exc is not None:
        return f"{type(exc).__name__}: {exc}"
    if response is not None and response.status_code >= 400:
        detail = (response.text or "")[:800]
        return f"LLM HTTP {response.status_code}: {detail}"
    return None


def chat_completions(
    *,
    payload: dict,
    http_client: httpx.Client | None = None,
    timeout: float = 180.0,
) -> RouterResult:
    primary, fallback = resolve_providers()
    primary_error: str | None = None
    response: httpx.Response | None = None
    exc: BaseException | None = None
    try:
        response = _post(primary, payload, http_client=http_client, timeout=timeout)
    except httpx.HTTPError as e:
        exc = e
    err = _is_http_failure(exc, response)
    if err is None and response is not None:
        return RouterResult(
            response=response,
            provider=primary.name,
            model=primary.model,
            fallback_used=False,
            primary_error=None,
        )
    primary_error = err or "primary failed"
    if fallback is None:
        raise ValueError(primary_error) from exc
    try:
        fb_response = _post(fallback, payload, http_client=http_client, timeout=timeout)
    except httpx.HTTPError as e:
        raise ValueError(f"primary failed ({primary_error}); fallback failed: {e}") from e
    fb_err = _is_http_failure(None, fb_response)
    if fb_err is not None:
        raise ValueError(f"primary failed ({primary_error}); fallback failed: {fb_err}")
    return RouterResult(
        response=fb_response,
        provider=fallback.name,
        model=fallback.model,
        fallback_used=True,
        primary_error=primary_error,
    )
```

- [ ] **Step 4: Re-run tests — primary path PASS; failover test not written yet**

```powershell
cd services/agents/common
python -m pytest tests/test_model_router.py -v
```

Expected: PASS for resolve + primary success tests

- [ ] **Step 5: Commit**

```powershell
git add services/agents/common/stock_agents_common/model_router.py services/agents/common/tests/test_model_router.py
git commit -m "feat(agents): add ModelRouter with primary/fallback env resolve"
```

---

### Task 2: ModelRouter — failover on HTTP ≥400 and transport error

**Files:**
- Modify: `services/agents/common/tests/test_model_router.py`
- Modify: `services/agents/common/stock_agents_common/model_router.py` (only if gaps)

**Interfaces:**
- Consumes: `chat_completions` from Task 1
- Produces: `fallback_used=True`, `primary_error` set, response from fallback

- [ ] **Step 1: Write failing failover tests**

```python
def test_chat_completions_falls_back_on_http_500(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")

    calls: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(str(request.url))
        if "ark.example" in str(request.url):
            return httpx.Response(500, text="upstream down")
        body = json.loads(request.content)
        assert body["model"] == "MiniMax-M3"
        assert request.headers["authorization"] == "Bearer mm-key"
        return httpx.Response(200, json={"choices": [{"message": {"content": "{}"}}]})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = chat_completions(payload={"messages": []}, http_client=client)
    assert result.fallback_used is True
    assert result.provider == "fallback"
    assert result.model == "MiniMax-M3"
    assert result.primary_error and "500" in result.primary_error
    assert len(calls) == 2


def test_chat_completions_falls_back_on_connect_error(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")

    def handler(request: httpx.Request) -> httpx.Response:
        if "ark.example" in str(request.url):
            raise httpx.ConnectError("boom", request=request)
        return httpx.Response(200, json={"choices": [{"message": {"content": "{}"}}]})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = chat_completions(payload={"messages": []}, http_client=client)
    assert result.fallback_used is True
    assert result.provider == "fallback"


def test_chat_completions_both_fail_raises(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, text="nope")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    with pytest.raises(ValueError, match="fallback failed"):
        chat_completions(payload={"messages": []}, http_client=client)
```

- [ ] **Step 2: Run tests**

```powershell
cd services/agents/common
python -m pytest tests/test_model_router.py -v
```

Expected: PASS if Task 1 implementation already includes failover; otherwise fix `chat_completions` until PASS

- [ ] **Step 3: Commit**

```powershell
git add services/agents/common/tests/test_model_router.py services/agents/common/stock_agents_common/model_router.py
git commit -m "test(agents): cover ModelRouter HTTP and connect failover"
```

---

### Task 3: Wire `ToolLLMClient` through ModelRouter

**Files:**
- Modify: `services/agents/common/stock_agents_common/llm_tools.py`
- Modify: `services/agents/common/tests/test_llm_tools_mock.py`

**Interfaces:**
- Consumes: `chat_completions` → `RouterResult`
- Produces: `complete_tools` return dict gains optional `router: {provider, model, fallback_used, primary_error}` plus existing `content`, `tool_calls`, `usage`, `latency_ms`

- [ ] **Step 1: Write failing test — primary failure falls back inside ToolLLMClient**

Add to `test_llm_tools_mock.py`:

```python
def test_live_tool_client_falls_back_on_primary_http_error(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")

    def handler(request: httpx.Request) -> httpx.Response:
        if "ark.example" in str(request.url):
            return httpx.Response(500, text="down")
        return httpx.Response(
            200,
            json={
                "choices": [
                    {
                        "message": {
                            "content": None,
                            "tool_calls": [
                                {
                                    "id": "1",
                                    "type": "function",
                                    "function": {"name": "get_news", "arguments": "{\"symbol\":\"AAPL\"}"},
                                }
                            ],
                        }
                    }
                ],
                "usage": {"prompt_tokens": 1, "completion_tokens": 1},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    out = client.complete_tools("sys", [{"role": "user", "content": "hi"}], [])
    assert out["router"]["fallback_used"] is True
    assert out["router"]["model"] == "MiniMax-M3"
    assert out["tool_calls"]
```

- [ ] **Step 2: Run test — expect fail (no router key / still uses LLM_API_KEY only)**

```powershell
cd services/agents/common
python -m pytest tests/test_llm_tools_mock.py::test_live_tool_client_falls_back_on_primary_http_error -v
```

Expected: FAIL

- [ ] **Step 3: Refactor `_complete_real` to use router**

In `llm_tools.py`, replace direct `httpx` post + env key reads with:

```python
from stock_agents_common.model_router import chat_completions

# inside _complete_real, after building payload (without relying on single LLM_MODEL for the wire model —
# router injects model per provider):
# Keep MiniMax thinking extras based on the *active* base URL after the call is awkward;
# instead: if either primary or fallback base contains "minimax", apply thinking flags when
# the resolved provider base contains minimax. Simpler approach for P0:
# apply MiniMax extras when "minimax" in (primary or legacy) base from env before post;
# for Volcengine primary, skip thinking/reasoning_split keys.

router = chat_completions(payload=payload, http_client=self._http_client, timeout=180.0)
response = router.response
# existing status/body parsing...
parsed = _parse_openai_message(message, usage, latency_ms)
parsed["router"] = {
    "provider": router.provider,
    "model": router.model,
    "fallback_used": router.fallback_used,
    "primary_error": router.primary_error,
}
return parsed
```

Remove duplicate `LLM_API_KEY` required check that only looks at `LLM_API_KEY` — let `resolve_providers` raise. Keep mock path unchanged.

**MiniMax payload extras:** Build `is_minimax` from `os.environ` bases: true if any of `LLM_PRIMARY_BASE_URL`, `LLM_FALLBACK_BASE_URL`, `LLM_BASE_URL` contains `"minimax"` **and** the intended first provider is MiniMax; for P0 simpler rule: add thinking extras only when `"minimax" in (LLM_PRIMARY_BASE_URL or LLM_BASE_URL or "").lower()` is false for Ark primary — i.e. **only add MiniMax extras when the selected provider base for the attempt includes minimax**. Implement by moving payload finalization into a small helper called from `_post` path... YAGNI: apply thinking extras if `"minimax" in provider.base_url.lower()` inside router by accepting an optional `mutate_payload(provider, payload)` callback, OR duplicate: ToolLLMClient builds base payload without thinking; after resolve, if primary is minimax-looking add extras before first post — messy with failover.

**P0 practical rule:** In `ToolLLMClient._complete_real`, after `resolve_providers()`, for each attempt the router already sets `model`. Add thinking fields to payload only when `"minimax" in provider.base_url.lower()`. Extend `chat_completions` with optional `prepare_payload: Callable[[ProviderConfig, dict], dict] | None = None` that returns payload for that provider.

```python
def prepare(provider: ProviderConfig, base_payload: dict) -> dict:
    p = copy.deepcopy(base_payload)
    if "minimax" in provider.base_url.lower():
        # existing thinking / reasoning_split logic
        ...
    elif not p.get("tools"):
        p["response_format"] = {"type": "json_object"}
    return p
```

Pass `prepare_payload=prepare` into `chat_completions`.

- [ ] **Step 4: Run related tests**

```powershell
cd services/agents/common
python -m pytest tests/test_llm_tools_mock.py tests/test_model_router.py -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```powershell
git add services/agents/common/stock_agents_common/llm_tools.py services/agents/common/stock_agents_common/model_router.py services/agents/common/tests/test_llm_tools_mock.py
git commit -m "feat(agents): route ToolLLMClient through ModelRouter"
```

---

### Task 4: Wire `LLMClient` through ModelRouter

**Files:**
- Modify: `services/agents/common/stock_agents_common/llm.py`
- Modify: `services/agents/common/tests/test_llm_mock.py`

**Interfaces:**
- Consumes: `chat_completions`
- Keeps `complete_json(...) -> dict` return shape (no router in return value required for legacy agents)

- [ ] **Step 1: Extend live test to use primary env and assert URL**

Update `test_real_mode_calls_openai_compatible_api` to still pass with legacy `LLM_*`. Add:

```python
def test_real_mode_uses_primary_env(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")

    payload = {
        "intents": [
            {"symbol": "AAPL", "side": "buy", "urgency": "normal", "rationale": "From LLM."}
        ],
        "warnings": [],
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://ark.example/api/v3/chat/completions"
        body = json.loads(request.content)
        assert body["model"] == "Doubao-Smart-Router"
        return httpx.Response(
            200,
            json={"choices": [{"message": {"content": json.dumps(payload)}}]},
        )

    client = LLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    result = client.complete_json("sys", "user", "decision_result")
    assert result == payload
```

- [ ] **Step 2: Run — expect fail until wired**

```powershell
cd services/agents/common
python -m pytest tests/test_llm_mock.py::test_real_mode_uses_primary_env -v
```

- [ ] **Step 3: Implement `LLMClient._complete_real` via router**

```python
from stock_agents_common.model_router import chat_completions

payload = {
    "messages": [
        {"role": "system", "content": system},
        {"role": "user", "content": user},
    ],
    "response_format": {"type": "json_object"},
}
router = chat_completions(payload=payload, http_client=self._http_client, timeout=120.0)
response = router.response
# if status already validated by router, parse content as today
```

Update `test_real_mode_requires_api_key` expected match to `LLM_API_KEY or LLM_PRIMARY_API_KEY`.

- [ ] **Step 4: Full common LLM tests PASS**

```powershell
cd services/agents/common
python -m pytest tests/test_llm_mock.py tests/test_llm_tools_mock.py tests/test_model_router.py -v
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/common/stock_agents_common/llm.py services/agents/common/tests/test_llm_mock.py
git commit -m "feat(agents): route LLMClient through ModelRouter"
```

---

### Task 5: Record router metadata on runtime tool-loop trace

**Files:**
- Modify: `services/agents/runtime/app/graphs/loop.py`
- Modify: `services/agents/runtime/tests/test_analyst_graph.py` (optional assertion if easy under mock — mock may omit router; add unit-style check only when live script injects router)

**Interfaces:**
- Consumes: `resp["router"]` from `ToolLLMClient.complete_tools`
- Produces: `trace.rounds[i].llm` includes `provider`, `model`, `fallback_used`, `primary_error` when present

- [ ] **Step 1: Adjust `call_model` llm meta**

Replace:

```python
model_name = (os.environ.get("LLM_MODEL") or "mock").strip() or "mock"
...
"llm": {"model": model_name, "latency_ms": int(resp.get("latency_ms") or 0)},
```

With:

```python
router_meta = resp.get("router") if isinstance(resp.get("router"), dict) else {}
model_name = (
    str(router_meta.get("model") or "").strip()
    or (os.environ.get("LLM_MODEL") or os.environ.get("LLM_PRIMARY_MODEL") or "mock").strip()
    or "mock"
)
llm_meta: dict[str, Any] = {
    "model": model_name,
    "latency_ms": int(resp.get("latency_ms") or 0),
}
if router_meta:
    llm_meta["provider"] = router_meta.get("provider")
    llm_meta["fallback_used"] = bool(router_meta.get("fallback_used"))
    if router_meta.get("primary_error"):
        llm_meta["primary_error"] = router_meta.get("primary_error")
```

Ensure mock path: `ToolLLMClient._complete_mock` may omit `router`; loop still works.

- [ ] **Step 2: Run runtime tests**

```powershell
cd services/agents/runtime
python -m pytest tests/ -v
```

Expected: PASS

- [ ] **Step 3: Commit**

```powershell
git add services/agents/runtime/app/graphs/loop.py
git commit -m "feat(runtime): record ModelRouter metadata on trace rounds"
```

---

### Task 6: Deploy env wiring + docs

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: `README.md` (env table / LLM section) and/or `docs/product-overview.md` one-line live model note
- Local: user updates `deploy/.env` with real keys (**do not commit secrets**)

**Interfaces:**
- Produces: compose passes through:

```yaml
LLM_PRIMARY_API_KEY: ${LLM_PRIMARY_API_KEY:-}
LLM_PRIMARY_BASE_URL: ${LLM_PRIMARY_BASE_URL:-}
LLM_PRIMARY_MODEL: ${LLM_PRIMARY_MODEL:-}
LLM_FALLBACK_API_KEY: ${LLM_FALLBACK_API_KEY:-}
LLM_FALLBACK_BASE_URL: ${LLM_FALLBACK_BASE_URL:-}
LLM_FALLBACK_MODEL: ${LLM_FALLBACK_MODEL:-}
```

Keep existing `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` for compatibility.

- [ ] **Step 1: Update `deploy/docker-compose.yml` `agent-runtime.environment`**

- [ ] **Step 2: Document in README**

Example block (placeholders only):

```text
# Primary: Volcengine Ark Doubao-Smart-Router (or ep-xxxxxxxx)
LLM_PRIMARY_API_KEY=
LLM_PRIMARY_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
LLM_PRIMARY_MODEL=Doubao-Smart-Router

# Fallback: MiniMax
LLM_FALLBACK_API_KEY=
LLM_FALLBACK_BASE_URL=https://api.minimaxi.com/v1
LLM_FALLBACK_MODEL=MiniMax-M3
```

- [ ] **Step 3: Commit docs + compose only**

```powershell
git add deploy/docker-compose.yml README.md docs/product-overview.md
git commit -m "docs(deploy): wire LLM primary/fallback env for ModelRouter"
```

---

## P0 Self-review checklist

1. **Spec coverage (§2):** resolve env, failover once, ToolLLMClient + LLMClient, trace meta, compose/docs — Tasks 1–6.
2. **No parse-triggered failover** — not implemented; left for P1 repair path.
3. **Types:** `RouterResult` / `ProviderConfig` names consistent across tasks.
4. **Follow-ups (separate plans):** P1 plan/act/reflect, P2 events+LangSmith+UI, P3 handoff+Go memory — see spec §6; write `2026-07-28-agent-runtime-p1-*.md` after P0 merges.

## Execution handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-28-agent-runtime-p0-model-router.md`.**

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks  
2. **Inline Execution** — execute tasks in this session with executing-plans checkpoints  

Which approach?

"""OpenAI-compatible LLM provider routing with optional HTTP failover."""

from __future__ import annotations

import os
from collections.abc import Callable
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
    prepare_payload: Callable[[ProviderConfig, dict], dict] | None = None,
) -> RouterResult:
    primary, fallback = resolve_providers()

    def _payload_for(provider: ProviderConfig) -> dict:
        if prepare_payload is not None:
            return prepare_payload(provider, payload)
        return payload

    primary_error: str | None = None
    response: httpx.Response | None = None
    exc: BaseException | None = None
    try:
        response = _post(primary, _payload_for(primary), http_client=http_client, timeout=timeout)
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
        fb_response = _post(
            fallback, _payload_for(fallback), http_client=http_client, timeout=timeout
        )
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

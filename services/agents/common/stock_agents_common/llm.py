"""Shared LLM client with mock fixture mode and OpenAI-compatible API."""

from __future__ import annotations

import json
import os
from pathlib import Path

import httpx

from stock_agents_common.model_router import chat_completions
from stock_agents_common.schemas import validate


def _find_repo_root() -> Path:
    current = Path(__file__).resolve().parent
    for parent in [current, *current.parents]:
        if (parent / "packages" / "contracts").is_dir():
            return parent
    raise FileNotFoundError("Could not locate repo root (packages/contracts)")


def _fixture_path(schema_name: str) -> Path:
    path = _find_repo_root() / "packages" / "contracts" / "fixtures" / f"{schema_name}.valid.json"
    if not path.is_file():
        raise FileNotFoundError(f"Fixture not found: {path}")
    return path


def _is_mock_mode() -> bool:
    return os.environ.get("LLM_MODE", "").strip().lower() == "mock"


class LLMClient:
    def __init__(self, *, http_client: httpx.Client | None = None) -> None:
        self._http_client = http_client

    def complete_json(self, system: str, user: str, schema_name: str) -> dict:
        """When LLM_MODE=mock, return fixture-driven dict; else call OpenAI-compatible API."""
        if _is_mock_mode():
            return self._complete_mock(schema_name)
        return self._complete_real(system, user, schema_name)

    def _complete_mock(self, schema_name: str) -> dict:
        data = json.loads(_fixture_path(schema_name).read_text(encoding="utf-8"))
        validate(data, schema_name)
        return data

    def _complete_real(self, system: str, user: str, schema_name: str) -> dict:
        payload = {
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            "response_format": {"type": "json_object"},
        }

        router = chat_completions(
            payload=payload,
            http_client=self._http_client,
            timeout=120.0,
        )
        response = router.response
        if response.status_code >= 400:
            detail = (response.text or "")[:800]
            raise ValueError(f"LLM HTTP {response.status_code}: {detail}")
        content = response.json()["choices"][0]["message"]["content"]
        result = json.loads(content)
        validate(result, schema_name)
        return result

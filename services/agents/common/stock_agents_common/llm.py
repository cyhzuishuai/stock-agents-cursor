"""Shared LLM client with mock fixture mode and OpenAI-compatible API."""

from __future__ import annotations

import json
import os
from pathlib import Path

import httpx

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
        api_key = os.environ.get("LLM_API_KEY", "").strip()
        if not api_key:
            raise ValueError("LLM_API_KEY is required when LLM_MODE is not mock")

        base_url = os.environ.get("LLM_BASE_URL", "https://api.openai.com/v1").strip().rstrip("/")
        model = os.environ.get("LLM_MODEL", "gpt-4o-mini").strip()

        payload = {
            "model": model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            "response_format": {"type": "json_object"},
        }

        if self._http_client is not None:
            response = self._http_client.post(
                f"{base_url}/chat/completions",
                headers={"Authorization": f"Bearer {api_key}"},
                json=payload,
            )
        else:
            with httpx.Client(timeout=120.0) as client:
                response = client.post(
                    f"{base_url}/chat/completions",
                    headers={"Authorization": f"Bearer {api_key}"},
                    json=payload,
                )

        response.raise_for_status()
        content = response.json()["choices"][0]["message"]["content"]
        result = json.loads(content)
        validate(result, schema_name)
        return result

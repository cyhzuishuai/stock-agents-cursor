from __future__ import annotations

import json
from pathlib import Path

import jsonschema


def _find_repo_root() -> Path:
    current = Path(__file__).resolve().parent
    for parent in [current, *current.parents]:
        if (parent / "packages" / "contracts").is_dir():
            return parent
    raise FileNotFoundError("Could not locate repo root (packages/contracts)")


def _schema_path(schema_name: str) -> Path:
    path = _find_repo_root() / "packages" / "contracts" / f"{schema_name}.schema.json"
    if not path.is_file():
        raise FileNotFoundError(f"Schema not found: {path}")
    return path


def validate(instance: dict, schema_name: str) -> None:
    schema = json.loads(_schema_path(schema_name).read_text(encoding="utf-8"))
    jsonschema.validate(instance, schema)

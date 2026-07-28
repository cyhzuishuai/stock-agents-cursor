from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[4]
RUNTIME_ROOT = Path(__file__).resolve().parents[1]
CONTRACTS = ROOT / "packages" / "contracts"

# Prefer this worktree's runtime package over other projects' `app` modules on PYTHONPATH.
_shadow_markers = ("agents-ad", "agents-chat-demo")
for path in list(sys.path):
    normalized = path.replace("\\", "/")
    if any(marker in normalized for marker in _shadow_markers):
        try:
            sys.path.remove(path)
        except ValueError:
            pass

if str(RUNTIME_ROOT) in sys.path:
    sys.path.remove(str(RUNTIME_ROOT))
sys.path.insert(0, str(RUNTIME_ROOT))

for key in list(sys.modules):
    if key == "app" or key.startswith("app."):
        mod = sys.modules.get(key)
        file = getattr(mod, "__file__", "") or ""
        if "agent-runtime-tool-loop" not in file.replace("\\", "/"):
            del sys.modules[key]


@pytest.fixture
def agent_run_request() -> dict:
    return json.loads((CONTRACTS / "fixtures" / "agent_run_request.valid.json").read_text(encoding="utf-8"))


@pytest.fixture
def mock_script_paths() -> dict[str, Path]:
    base = CONTRACTS / "fixtures" / "mock_tool_scripts"
    return {"analyst": base / "analyst.json", "portfolio": base / "portfolio.json"}

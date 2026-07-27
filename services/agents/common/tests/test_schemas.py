import json
from pathlib import Path

from stock_agents_common.schemas import validate


def _repo_root() -> Path:
    current = Path(__file__).resolve().parent
    for parent in [current, *current.parents]:
        if (parent / "packages" / "contracts").is_dir():
            return parent
    raise RuntimeError("repo root not found")


def test_validate_agent_run_request_fixture():
    root = _repo_root()
    fixture_path = root / "packages" / "contracts" / "fixtures" / "agent_run_request.valid.json"
    fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
    validate(fixture, "agent_run_request")

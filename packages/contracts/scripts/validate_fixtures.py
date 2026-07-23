# packages/contracts/scripts/validate_fixtures.py
import json, sys
from pathlib import Path
try:
    import jsonschema
except ImportError:
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", "jsonschema"])
    import jsonschema

root = Path(__file__).resolve().parents[1]
schema = json.loads((root / "agent_run_request.schema.json").read_text())
fixture = json.loads((root / "fixtures" / "agent_run_request.valid.json").read_text())
jsonschema.validate(fixture, schema)
print("OK agent_run_request")

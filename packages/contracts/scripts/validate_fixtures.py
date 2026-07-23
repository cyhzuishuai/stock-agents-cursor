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
fixtures_dir = root / "fixtures"

for schema_path in sorted(root.glob("*.schema.json")):
    name = schema_path.stem.replace(".schema", "")
    fixture_path = fixtures_dir / f"{name}.valid.json"
    if not fixture_path.exists():
        print(f"SKIP {name}: no matching fixture")
        continue
    schema = json.loads(schema_path.read_text())
    fixture = json.loads(fixture_path.read_text())
    jsonschema.validate(fixture, schema)
    print(f"OK {name}")

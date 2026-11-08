"""Run T8's two independent real clusters and compare ordered effects/state."""

import argparse
from datetime import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

REPO = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go")
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    go = shutil.which(args.go)
    if not go:
        raise ValueError(f"Go executable not found: {args.go}")
    output = (args.output or REPO / "local-runs" / datetime.now().strftime("repeat-%Y%m%d-%H%M%S-%f")).resolve()
    output.mkdir(parents=True, exist_ok=False)
    command = [go, "test", "./src/realnet", "-run", "^TestStage5/T8$", "-count=1", "-json"]
    if args.race:
        command.append("-race")
    print(f"Running two real clusters; evidence: {output}", flush=True)
    with (output / "go-test.jsonl").open("w", encoding="utf-8") as log:
        result = subprocess.run(command, cwd=REPO, env=dict(os.environ, LAB01_EVIDENCE_DIR=str(output)),
                                stdout=log, stderr=subprocess.STDOUT)
    if result.returncode:
        raise RuntimeError(f"T8 failed (exit {result.returncode}); inspect {output / 'go-test.jsonl'}")
    events = [json.loads(line) for line in (output / "go-test.jsonl").read_text(encoding="utf-8").splitlines()]
    if not any(e.get("Action") == "pass" and e.get("Test") == "TestStage5/T8" for e in events):
        raise RuntimeError("T8 did not execute successfully")
    first, second = [json.loads((output / name / "verified.json").read_text(encoding="utf-8"))
                     for name in ("T8-first", "T8-second")]
    if not first["Transactions"] or first["Transactions"] != second["Transactions"] or first["Root"] != second["Root"]:
        raise RuntimeError("Ordered signed transactions or final roots differ")
    summary = {"equal_transaction_order": True, "equal_state_root": True, "root": first["Root"],
               "transaction_ids": [bytes(value).hex() for value in first["Transactions"]],
               "comparison": "Ordered effects and state; real timestamps and arrival logs may differ."}
    (output / "comparison.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(f"PASS: ordered transactions and final root match: {first['Root']}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("Interrupted; repeatability was not verified.", file=sys.stderr)
        sys.exit(130)

"""Submit many transactions from one fresh identity outside the validator set."""

import argparse
from datetime import datetime
import json
import os
from pathlib import Path
import subprocess
import sys


REPO = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config-dir", type=Path, required=True,
                        help="prepared configuration directory of a running cluster")
    parser.add_argument("--via", type=int, default=9,
                        help="live validator that receives the client transactions")
    parser.add_argument("--count", type=int, default=20_000)
    parser.add_argument("--rate", type=int, default=200,
                        help="maximum accepted transactions per second; 0 is unlimited")
    parser.add_argument("--value-size", type=int, default=256)
    parser.add_argument("--record", default="bulk")
    parser.add_argument("--retry-ms", type=int, default=250)
    parser.add_argument("--max-retry-seconds", type=int, default=60,
                        help="stop if one transaction cannot be admitted within this time")
    parser.add_argument("--go", default="go", help="Go executable path")
    parser.add_argument("--key-file", type=Path,
                        help="path for the new key; it must not already exist")
    args = parser.parse_args()

    directory = args.config_dir.resolve()
    configs = sorted(directory.glob("node-*.json"))
    if not configs:
        raise ValueError("configuration directory contains no node-*.json files")
    if args.via < 0 or args.via >= len(configs):
        raise ValueError("--via is outside the configured validator set")
    ingress = json.loads(configs[args.via].read_text(encoding="utf-8"))
    if ingress.get("Behavior", "honest") != "honest":
        raise ValueError("choose an honest live ingress node with --via")
    output = Path(ingress.get("OutputDir", f"node-{args.via:02d}"))
    if not output.is_absolute():
        output = directory / output
    if not (output / "ready").exists():
        raise ValueError(f"ingress node {args.via} is not ready: {output / 'ready'}")
    if args.count < 1 or args.rate < 0 or not 1 <= args.value_size <= 32768 or args.retry_ms < 1 or args.max_retry_seconds < 1:
        raise ValueError("require count>0, rate>=0, value-size 1..32768 and positive retry limits")

    key_path = args.key_file
    if key_path is None:
        stamp = datetime.now().strftime("%Y%m%d-%H%M%S-%f")
        key_path = directory / f"external-client-{stamp}.hex"
    else:
        key_path = key_path.resolve()
    if key_path.exists():
        raise ValueError("--key-file must be a new path; existing keys are never reused")

    binary = REPO / "local-runs" / ("txload.exe" if os.name == "nt" else "txload")
    binary.parent.mkdir(exist_ok=True)
    subprocess.run([args.go, "build", "-o", str(binary), "./src/cmd/txload"],
                   cwd=REPO, check=True)
    command = [
        str(binary), "-config", str(configs[0]), "-key", str(key_path),
        "-via", str(args.via), "-count", str(args.count),
        "-rate", str(args.rate), "-value-size", str(args.value_size),
        "-record", args.record, "-retry", f"{args.retry_ms}ms",
        "-max-retry", f"{args.max_retry_seconds}s",
    ]
    print(f"Fresh external key: {key_path}", flush=True)
    print(f"Submitting {args.count} transactions through node {args.via}...", flush=True)
    subprocess.run(command, cwd=REPO, check=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)

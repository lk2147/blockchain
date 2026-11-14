"""Read a running node's status, RAM mempool, or on-disk ledger over localhost HTTP."""

import argparse
import json
from pathlib import Path
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import urlopen


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config-dir", type=Path, required=True)
    parser.add_argument("--node", type=int, required=True)
    parser.add_argument("--view", choices=("status", "mempool", "ledger"), required=True)
    parser.add_argument("--from-height", type=int, default=1,
                        help="first ledger height; only used with --view ledger")
    parser.add_argument("--limit", type=int,
                        help="maximum blocks returned; only used with --view ledger")
    parser.add_argument("--timeout", type=float, default=5.0)
    args = parser.parse_args()

    directory = args.config_dir.resolve()
    config_path = directory / f"node-{args.node:02d}.json"
    if not config_path.is_file():
        raise ValueError(f"node config does not exist: {config_path}")
    config = json.loads(config_path.read_text(encoding="utf-8"))
    if config.get("NodeID") != args.node:
        raise ValueError("node ID does not match config filename")
    output = Path(config.get("OutputDir", f"node-{args.node:02d}"))
    if not output.is_absolute():
        output = directory / output
    address_path = output / "admin"
    if not address_path.is_file():
        raise ValueError(f"node is not exposing its admin endpoint: {address_path}")

    query = ""
    if args.view == "ledger":
        if args.from_height < 1 or (args.limit is not None and args.limit < 1):
            raise ValueError("ledger range values must be positive")
        values = {"from": args.from_height}
        if args.limit is not None:
            values["limit"] = args.limit
        query = "?" + urlencode(values)
    url = f"http://{address_path.read_text(encoding='utf-8').strip()}/{args.view}{query}"
    with urlopen(url, timeout=args.timeout) as response:
        value = json.load(response)
    print(json.dumps(value, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, json.JSONDecodeError, HTTPError, URLError) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)

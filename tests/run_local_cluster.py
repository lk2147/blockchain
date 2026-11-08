"""Build, configure and run real Go nodes. Python 3.9+, standard library only."""

import argparse
from collections import Counter
from contextlib import ExitStack
from datetime import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import time


# Edit these defaults or pass command-line options.
DEFAULT_NODES = 10
DEFAULT_BASE_PORT = 29000
DEFAULT_RUN_MS = 10_000

# Optional overrides for freshly generated configurations only.
# For example, uncomment node 0 to delay only its outgoing messages by 500 ms.
NODE_FAULT_OVERRIDES = {
    # 0: {"MinDelayMS": 500, "MaxDelayMS": 500, "DisableAfterMS": 0},
}

REPO = Path(__file__).resolve().parent.parent


def build_node(go):
    output = REPO / "local-runs"
    output.mkdir(exist_ok=True)
    binary = output / ("node.exe" if os.name == "nt" else "node")
    subprocess.run([go, "build", "-o", str(binary), "./src/cmd/node"],
                   cwd=REPO, check=True)
    return binary


def prepare_cluster(binary, args):
    if args.nodes < 8 or (args.nodes - 1) % 3:
        raise ValueError("Use n >= 8 and n = 3f + 1, for example ten nodes.")
    directory = args.run_dir or REPO / "local-runs" / datetime.now().strftime("run-%Y%m%d-%H%M%S-%f")
    directory = directory.resolve()
    if directory.exists():
        raise ValueError("Choose a fresh --run-dir; existing runs are never overwritten.")
    directory.parent.mkdir(parents=True, exist_ok=True)
    command = [str(binary), "-init-dir", str(directory), "-nodes", str(args.nodes),
               "-base-port", str(args.base_port), "-run-ms", str(args.run_ms)]
    if args.faults:
        command.append("-faults")
    subprocess.run(command, cwd=REPO, check=True)
    for node_id, changes in NODE_FAULT_OVERRIDES.items():
        if node_id not in range(args.nodes):
            raise ValueError(f"Unknown node in NODE_FAULT_OVERRIDES: {node_id}")
        path = directory / f"node-{node_id:02d}.json"
        config = json.loads(path.read_text(encoding="utf-8"))
        config["Faults"].update(changes)
        path.write_text(json.dumps(config, indent=2), encoding="utf-8")
    return directory


def load_configs(directory):
    paths = sorted(directory.glob("node-*.json"))
    if len(paths) < 8 or (len(paths) - 1) % 3:
        raise ValueError("Configuration directory must contain a valid complete validator set.")
    configs = []
    for node_id, path in enumerate(paths):
        config = json.loads(path.read_text(encoding="utf-8"))
        if config["NodeID"] != node_id:
            raise ValueError(f"Unexpected node ID in {path}")
        output = Path(config["OutputDir"])
        if not output.is_absolute():
            output = path.parent / output
        configs.append((path, config, output))
    return configs


def require_fresh_output(configs):
    """Only nodes about to start must have fresh outputs, not their peers."""
    for _, _, output in configs:
        if any((output / name).exists() for name in ("events.jsonl", "result.json", "running.lock")):
            raise ValueError(f"Existing node output at {output}; initialize a fresh cluster.")


def run_one_node(binary, path):
    """Keep this real Go node attached to the user's current terminal."""
    process = subprocess.Popen([str(binary), "-config", str(path)], cwd=REPO)
    try:
        try:
            code = process.wait()
        except KeyboardInterrupt:
            # The attached Go process also receives terminal Ctrl+C. Let it
            # save its result before falling back to forced cleanup.
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                pass
            raise
        if code:
            raise RuntimeError(f"Node exited with code {code}.")
    finally:
        stop_processes([process])


def stop_processes(processes):
    """Clean up only child processes launched by this script."""
    for process in processes:
        if process.poll() is None:
            process.terminate()
    for process in processes:
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def verify_results(configs):
    expected_headers = None
    expected_root = None
    pids = set()
    counts = Counter()
    for path, config, output in configs:
        result = json.loads((output / "result.json").read_text(encoding="utf-8"))
        ledger = result["Ledger"]
        if (result["NodeID"] != config["NodeID"] or result["PID"] in pids
                or len(ledger) != config["TargetHeights"]):
            raise ValueError(f"Incomplete or invalid result for {path.name}")
        pids.add(result["PID"])
        headers = [entry["Block"]["Header"] for entry in ledger]
        if expected_headers is None:
            expected_headers, expected_root = headers, result["StateRoot"]
        elif headers != expected_headers or result["StateRoot"] != expected_root:
            raise ValueError(f"Nodes disagree at {path.name}")
        print(f"Node {result['NodeID']:2}: PID {result['PID']:6}, blocks {len(ledger)}")
        with (output / "events.jsonl").open(encoding="utf-8") as log:
            for line in log:
                counts[json.loads(line)["Event"]] += 1
    print(f"Matching state root: {expected_root}")
    for name in ("fault_drop", "fault_duplicate", "fault_reorder", "fault_replay",
                 "send_error", "block", "unblock"):
        if counts[name]:
            print(f"  {name}: {counts[name]}")
    print("Histories and roots agree. Go tests additionally verify execution and quorum signatures.")


def run_cluster(binary, directory, configs):
    processes = []
    try:
        with ExitStack() as files:
            deadline = time.monotonic() + max(c[1]["RunForMS"] for c in configs) / 1000 + 20
            for path, config, _ in configs:
                node_id = config["NodeID"]
                stdout = files.enter_context((directory / f"stdout-{node_id:02d}.txt").open("w"))
                stderr = files.enter_context((directory / f"stderr-{node_id:02d}.txt").open("w"))
                process = subprocess.Popen(
                    [str(binary), "-config", str(path)], cwd=REPO,
                    stdout=stdout, stderr=stderr,
                    creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
                )
                processes.append(process)
            for process in processes:
                code = process.wait(timeout=max(0, deadline - time.monotonic()))
                if code:
                    raise RuntimeError(f"Node PID {process.pid} failed; inspect stderr in {directory}.")
            verify_results(configs)
    finally:
        stop_processes(processes)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go", help="Go executable path")
    parser.add_argument("--nodes", type=int, default=DEFAULT_NODES)
    parser.add_argument("--base-port", type=int, default=DEFAULT_BASE_PORT)
    parser.add_argument("--run-ms", type=int, default=DEFAULT_RUN_MS)
    parser.add_argument("--faults", action="store_true", help="Generate example fault policies")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--run-dir", type=Path, help="Fresh directory to initialize")
    group.add_argument("--config-dir", type=Path, help="Start existing, edited configs without regenerating")
    parser.add_argument("--prepare-only", action="store_true", help="Generate configs, then stop for editing/manual launch")
    actions = parser.add_mutually_exclusive_group()
    actions.add_argument("--node", type=int, help="Run only this node in the current terminal; requires --config-dir")
    actions.add_argument("--check-only", action="store_true", help="Check saved results without building or starting nodes; requires --config-dir")
    args = parser.parse_args()
    if (args.node is not None or args.check_only) and (not args.config_dir or args.prepare_only):
        parser.error("--node/--check-only require --config-dir and cannot be combined with --prepare-only")
    if args.config_dir:
        directory = args.config_dir.resolve()
        binary = REPO / "local-runs" / ("node.exe" if os.name == "nt" else "node")
    else:
        binary = build_node(args.go)
        directory = prepare_cluster(binary, args)
    configs = load_configs(directory)
    print(f"Cluster directory: {directory}", flush=True)
    if args.check_only:
        verify_results(configs)
    elif args.node is not None:
        selected = [entry for entry in configs if entry[1]["NodeID"] == args.node]
        if not selected:
            raise ValueError(f"Unknown node ID: {args.node}")
        require_fresh_output(selected)
        if not binary.exists():
            binary = build_node(args.go)
        run_one_node(binary, selected[0][0])
    elif args.prepare_only:
        require_fresh_output(configs)
        print("Edit the JSON files, then run:")
        print(f'python tests/run_local_cluster.py --go "{args.go}" --config-dir "{directory}"')
        print(f"Or use --config-dir with --node 0 through --node {len(configs) - 1} in separate terminals.")
    else:
        require_fresh_output(configs)
        if args.config_dir:
            binary = build_node(args.go)
        run_cluster(binary, directory, configs)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("Interrupted; started child processes were stopped.", file=sys.stderr)
        sys.exit(130)

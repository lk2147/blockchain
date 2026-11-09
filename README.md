# Blockchain Lab01

This project runs independent Go blockchain nodes over real libp2p GossipSub
connections and persistent bidirectional gRPC streams.
Python is the only launcher/experiment scripting language. There is no virtual
network backend. Each node validates signatures, executes owned key-value updates,
and finalizes its own ledger and Merkle state commitment.

## Requirements

- Python 3.9+; the launcher uses only the standard library.
- The installed Go toolchain matching go.mod; keep the pinned dependencies.
- Available localhost TCP ports (default: gRPC 29000-29009 and gossip 29010-29019).

Headers and votes are published through GossipSub. Accepted-header body requests,
bodies and directed messages travel over the gRPC Exchange stream. Either end
can send independently on that same stream; disconnected sessions reconnect.
Libp2p uses the validator's Ed25519 identity, Noise and Yamux over TCP. The static
peer list supplies bootstrap addresses; it is not a dynamic validator registry.

Open any terminal in the repository root (the folder containing go.mod and this
README; for example, `D:/applied-blockchain/blockchain`, or the extracted
`blockchain` folder).
All commands below use Python or Go directly; no shell-specific helper is needed.

## Quick start: launch all ten nodes

```text
python tests/run_local_cluster.py
```

Enable the example network faults:

```text
python tests/run_local_cluster.py --faults
```

If Go is not on PATH, add its executable path:

```text
python tests/run_local_cluster.py --go D:/tools/golang/bin/go.exe --faults
```

The script builds the Go node, generates fresh cryptographic keys and per-node
JSON configurations, starts ten actual node processes, and checks their saved
histories and state roots. It prints the run directory and fault counts. The
standard generated workload targets two blocks. Processes keep serving peers
until `RunForMS` expires (10 seconds by default), then save their results.

Options include `--run-ms 60000`, `--base-port 29200`, `--nodes 10`, and
`--run-dir local-runs/my-experiment`. Node counts must satisfy n = 3f + 1 and
n >= 8; ten nodes use f = 3 and quorum = 7. Start all ten for a normal run.

## Prepare configurations for editing

Prepare a fresh cluster without starting any nodes:

```text
python tests/run_local_cluster.py --run-dir local-runs/manual --nodes 10 --run-ms 60000 --prepare-only
```

Add `--go D:/tools/golang/bin/go.exe` if needed. This creates the executable,
keys, and `node-00.json` through `node-09.json`. Choose a fresh directory for each
experiment; existing output cannot be safely resumed.

Edit the desired node's JSON before launching. For example, replace the `Faults`
object in `local-runs/manual/node-00.json` with this to delay node 0's outgoing
messages by at least 500 ms while other nodes remain unchanged:

```json
"Faults": {
  "Seed": 1,
  "MinDelayMS": 500,
  "MaxDelayMS": 500,
  "DropBPS": 0,
  "DuplicateBPS": 0,
  "ReorderEvery": 0,
  "ReorderHoldMS": 0,
  "ReplayEvery": 0,
  "ReplayAgeMS": 0,
  "DisableAfterMS": 0
}
```

`DropBPS` and `DuplicateBPS` use basis points: 1000 means 10%. Reorder settings
hold every Nth message longer. Replay settings retransmit eligible old signed
votes. `DisableAfterMS: 0` keeps faults enabled; a positive value ends injections
after that node's elapsed milliseconds. See [network controls](tests/REAL_NETWORK.md).

`RunForMS` controls process lifetime, `BaseTimeoutMS` controls initial consensus
timeouts, and `RetryMS` controls retransmission. Configs are read at startup, not
reloaded live. Keep the shared chain/validator list consistent and each node's
ID/key unchanged. Editable defaults and `NODE_FAULT_OVERRIDES` at the top of the
Python script apply only when generating new configs.

Each Peers entry now contains Address (gRPC), GossipAddress (libp2p TCP) and
PublicKey. Old unary-gRPC configurations must be regenerated in a fresh directory.
The Python launcher rejects different ordered peer lists across node configs.
Nodes log the shared membership fingerprint and require it in signed stream
handshakes. Gossip topic names also include that fingerprint.

## Start one node per terminal using Python

After preparation/editing, open ten terminals in the repository root. Start
one command in each terminal, using node IDs 0 through 9:

```text
# Terminal 1
python tests/run_local_cluster.py --config-dir local-runs/manual --node 0
```

```text
# Terminal 2
python tests/run_local_cluster.py --config-dir local-runs/manual --node 1
```

Continue through `--node 9`. These commands reuse the prepared executable and
configs; they do not rebuild an executable that other nodes are using or replace
your edits. Start nodes promptly because each process's duration begins at its
own startup. Quiet terminals are normal: events are written to files.

Alternatively, start all edited configurations with one command:

```text
python tests/run_local_cluster.py --config-dir local-runs/manual
```

Choose either the separate-terminal approach or the all-node approach for a
fresh run, not both. The all-node command rebuilds before launching. Ctrl+C in
a single-node terminal lets the attached Go node save its current result; an
interrupted all-node launcher stops its children and partial results may be absent.

## Inspect logs and check results with Python

Each node writes its own output directory, for example `local-runs/manual/node-00`:

- `events.jsonl`: live message, fault, consensus, and rejection events.
- `ready`: written after the gRPC server starts.
- `result.json`: finalized blocks/certificates and root, saved on orderly shutdown.

Read the current log (repeat the command to refresh):

```text
python -c "from pathlib import Path; print(Path('local-runs/manual/node-00/events.jsonl').read_text(encoding='utf-8'))"
```

After all nodes stop, check their saved histories and roots without building
or starting anything:

```text
python tests/run_local_cluster.py --config-dir local-runs/manual --check-only
```

A successful check requires the configured block count, matching histories/roots,
and distinct process results. Missing/inconsistent results are errors. The Go
multi-process tests additionally re-execute blocks and verify quorum signatures;
the Python launcher is a consistency checker, not an independent cryptographic verifier.

Keys and outputs are under Git-ignored `local-runs`; do not commit private keys.
Use a fresh cluster for another experiment. Safe crash recovery is not implemented.
Fresh initialization generates new keys, so state roots need not match between
different generated clusters. Compare nodes within a run; controlled rerun tests
use fixed identities and the same ordered inputs when comparing final states.

## Run the tests

```text
python tests/run_tests.py
python tests/run_tests.py --race
```

Add `--go D:/tools/golang/bin/go.exe` if Go is not on PATH. This single entry
checks formatting, runs all unit tests and named real-process T1-T8 scenarios,
checks that no scenario is missing, and runs go vet. Evidence is saved in the
printed fresh local-runs/suite-* directory; use --output to choose a fresh path.
Race detection needs a supported C compiler. The suite launches ten actual node
processes for each case and takes a few minutes, especially with --race.

The child-entry helper is skipped in the parent and executed inside the children;
it is not a skipped scenario. Direct `go test ./... -count=1` also runs the suite,
but its scenario logs use temporary directories unless LAB01_EVIDENCE_DIR is set.
See [test status](tests/TEST_STATUS.md) and [Stage 5](tests/STAGE5.md) for observed
results, injection details and limitations.

## Scope and implementation notes

The configured faults act on real messages. Tests verify holds before actual
transmission, rate limits, replay/duplicate/reorder transmissions, peer blocking,
header acceptance before body sends, ordered workloads, and finalized state.
Repeatability means expected transaction order/results, as clarified in AGENTS.md;
real timestamps, incidental arrival order, and proposer choices may vary.

Connections are currently loopback-only. gRPC is unencrypted for this laboratory;
Ed25519 transport signatures authenticate sender/recipient and messages. This is
not a production or public-internet deployment. Workload transactions are preloaded
signed client inputs. Dynamic membership and safe restart/recovery are not implemented.

The message controls act before local gossip publications and directed stream
writes. GossipSub's connection/control traffic and automatic relay forwarding
are managed by libp2p, outside these artificial holds and application send limits.
They are not a kernel-packet network conditioner. A send/publish event is not
proof of application acceptance or finalization; check receiver events and ledgers.

See [real networking](tests/REAL_NETWORK.md) for the current design and test
commands. Historical stage reports describe earlier snapshots. Final report and
packaging work is deferred until the implementation and required checks pass.

## Repeatability

Run only the controlled rerun experiment (two independent ten-process clusters):

```text
python tests/verify_repeatability.py
```

Optional arguments are --go, --race and --output (a fresh evidence directory).
This checks signed transaction order and final state, plus the Go T8 invariants;
it does not compare timestamps or incidental network log order.

Current evidence is saved under the fresh local-runs directory printed by the
runner. Older logs/evidence describes the earlier unary transport. Generate new
experiments with the scripts rather than attempting to restart recorded runs.

## Protobuf maintenance

Use the existing tools and go.mod. Regenerate only after editing the schema:

```text
protoc --go_out=paths=source_relative:. --go-grpc_out=paths=source_relative:. src/proto/consensus.proto
```

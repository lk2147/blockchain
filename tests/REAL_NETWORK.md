# Real networking: design, configuration and evidence

## Execution and ownership

Each validator is a separate Go process with its own private key, consensus
engine, ledger, state and Merkle root. The only cluster transport is actual
localhost TCP/gRPC. There is no shared ledger or central finalization authority.

```text
node's signed message -> configurable fault layer -> actual gRPC/TCP
                     -> receiver's validation and consensus engine
```

The fault layer acts on application messages before their RPC is sent. A drop
deliberately omits that RPC; it does not drop kernel TCP packets, which TCP would
normally retransmit. A delay holds a message for real elapsed milliseconds.
This changes real communication without simulating the blockchain nodes.

One owner loop handles each node's consensus state and real timer callbacks.
Concurrent gRPC handlers authenticate messages and post them to this loop.
An IO worker enforces outbound rate limits at actual RPC-attempt time. Logs use
a mutex because the owner loop and IO worker both produce events. Protocol
rejection is logged; internal/scheduling/log failures propagate instead of
being silently treated as bad peer messages.

## Running

From the repository root:

```powershell
python tests/run_local_cluster.py --faults
# If Go is not on PATH:
python tests/run_local_cluster.py --go D:\tools\golang\bin\go.exe --faults
```

The script builds node.exe, generates fresh cryptographic keys/configurations,
starts ten distinct node processes, waits for their run duration and compares
their saved two-block headers and state roots. Use `--base-port` if 29000..29009
are occupied, and a fresh `--run-dir` to name an experiment. Keys/logs are in Git-ignored
local-runs directories. Keys come from crypto/rand, never the fault seed.

To customize a node, prepare with Python --prepare-only, edit its JSON, then use
--config-dir and --node 0 through --node 9 in separate terminals as in README.
The launcher initializes a fresh cluster, so it does not reuse edited configs.
Each JSON independently specifies NodeID, peer addresses/public keys, own key
file, output directory, timeouts, retries, duration, workload and fault policy.

For example, edit only node-00.json to deliberately delay node 0:

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

Its outgoing messages are held for at least 500 ms. Queueing/OS delays may make
them later; they cannot be sent earlier. Other node configs can remain fault-free.

| Control | Action |
|---|---|
| MinDelayMS / MaxDelayMS | Choose an elapsed-time hold in this range |
| DropBPS | Discard transmissions with this probability; 10000 = 100% |
| DuplicateBPS | Schedule an additional original signed frame |
| ReorderEvery / ReorderHoldMS | Hold every Nth admitted message longer, allowing later messages to pass |
| ReplayEvery / ReplayAgeMS | On every Nth admission, resend an eligible old vote to that destination, preserving its original content, sequence and signatures |
| DisableAfterMS | Disable injections after this node's elapsed run time; zero keeps them enabled |

Replay history is bounded to 256 frames; an opportunity with no eligible old
vote produces no replay. Tests require actual replay events and rejection of
old-height votes. The generated -faults policy has a three-second fault window.
Real input arrival/OS scheduling can vary despite fixed fault seeds.

SendGapMS limits actual RPC-attempt rate; QueueLimit bounds pending traffic.
Overflow is logged and ordinary cached-message retries try again. Each receiver
counts authenticated arrivals per peer/ReceiveWindowMS and temporarily blocks
overactive peers for BlockMS. Honest traffic must fit those limits for liveness.
Faults stopping does not make the OS enforce a hard latency guarantee.

## Header/body and signatures

Only a proposal/decision header and evidence are advertised initially; no body
is embedded. The receiver checks its own expected chain/height/proposer/parent,
signatures and quorum evidence, then sends a signed matching body_request.
Normal sender code refuses a body without that request. Receiver code also
requires its own accepted-header record and verifies BodyHash before execution.
A signed request acknowledges acceptance; a sender cannot inspect a Byzantine
receiver's private internal state. Invalid bodies never become valid by reception.

The transfer reference is SHA-256 of the exact advertised JSON envelope, keeping
different round envelopes separate even when they wrap the same immutable block.
Protocol signatures/hashes still use Stage 1 canonical encoding, not arbitrary
Protobuf/JSON bytes. TX/HEADER/VOTE/PROPOSAL formats and Merkle semantics are unchanged.

All gRPC envelopes, including requests, authenticate source and recipient with
Ed25519 under TRANSPORT:<chain>. Canonical version-1 fields, in order:

```text
version byte 1
uint32 length + UTF-8 "TRANSPORT:<chain_id>"
uint32 sender, uint32 recipient, uint64 sequence
uint32 kind length + kind
fixed 32-byte reference
uint64 height, uint64 round
uint32 payload length + payload
```

Integers are unsigned big-endian; own signature is excluded. Expected chain and
recipient are checked. Sequence numbers identify signed frames, not an early
duplicate filter: injected replays must reach the application's nonce/vote/height
guards. The old tooling-only amount/recipient schema was replaced by Envelope,
Ack and Transport.Deliver and regenerated with installed tools. Its Go package
path now matches the module. Dependency versions were not changed.

## Files and outputs

- src/node/runtime.go: node-local blockchain messages, retries and consensus.
- src/realnet/config.go: configuration/key loading.
- src/realnet/auth.go: canonical transport authentication.
- src/realnet/faults.go: configured message fault planning.
- src/realnet/run.go: gRPC sockets, owner loop, real IO/timers, logs/results.
- src/realnet/generate.go: fresh local development cluster generation.
- src/cmd/node/main.go: runnable node/initializer.
- src/network/message.go: wire/event/error types, no simulator implementation.
- tests/run_local_cluster.py: launcher and result comparison.

Per-node events.jsonl records UTC wall time, elapsed milliseconds, local sequence,
peer, kind, height/round, packet/reference and reasons. Fault records include
planned holds; send records mark actual transmission attempts. result.json contains
the process PID, independently finalized blocks/certificates and state root.
Nodes keep serving decisions until RunForMS expires so lagging peers can catch up.

Results are saved at orderly shutdown, not persisted as safe crash-recovery
state. Existing output is refused and a lock prevents concurrent use of one
output directory. Restarting a validator into an ongoing run is unsupported.
Connections are currently loopback-only and unencrypted gRPC, with signed messages;
encrypted remote/public deployment is not claimed. gRPC incoming size is 4 MiB.

## Actual results

The documented launcher was executed with ten node.exe processes, faults enabled,
ports 29200..29209 and a ten-second configured run. Every process saved two
matching blocks and this final state root:

```text
1833b74a2ded7ac3c0b1f6e4c5b95959ad5654f50f061c781cedde3596d36acf
```

Observed injections: 189 drops, 254 duplicates, 223 reorder holds and 285 replays.
There were 69 send_error events; startup/unavailable-peer RPC attempts can fail
and are retried. These are measured counts, not predictions for future runs.
Evidence remains in local-runs/verified-real-faults (ignored because it contains keys).

| Test | Initial conditions / injected input | Expected invariant | Observed |
|---|---|---|---|
| TestTransportAuthenticationAndDomains | Fixed test key; mutate each signed field/context; wrong chain/recipient | Reject tampering and cross-domain/scope reuse | PASS |
| TestFaultPolicyPreservesSignedReplays | Explicit policy/input times; forced duplication/drop, reorder, old history | Exact holds; unchanged old signatures/content; fault-disable behavior | PASS |
| TestRealProcessClusters/normal | Ten distinct child processes over real TCP, two fixed signed workloads | Independently re-execute all ledgers, verify 7-signature quorums, expected values/nonces and common history/root | PASS |
| TestRealProcessClusters/injected-faults | Same real cluster with all five faults enabled, later disabled | Same independent checks; all fault types actually occur; old votes rejected; every transmission honors its planned hold | PASS |
| TestRealSocketRejectsInvalidHeadersAndBodies | Actual gRPC connection with invalid proposal signature, mismatched/unsolicited body and authenticated peer flood | Reject invalid data; no vote/finalization supporting it; actual peer block/drop/unblock | PASS |

The child-entry test helper is skipped in the parent and executed inside real
child processes; it is not a skipped scenario. The parent verifies 10 distinct
PIDs, complete signed quorum evidence and block execution rather than trusting
reported success flags. Fault planning is unit-tested directly, not by adding
another virtual network.

Actual checks: gofmt clean; go test ./... -count=1 passed; final go test -race ./...
-count=1 passed including sockets/child processes; go vet ./... passed with no
diagnostics. One intermediate extraction build failed due to a duplicated type
suffix; it was corrected before the successful regressions. No protocol assertion
was weakened. Existing crypto/state/block/consensus tests remain.

## Interpretation and pending work

Repeatability means expected results and ordered transaction effects, with
configured faults demonstrably applied. Block transaction execution order is
deterministic; incidental socket arrivals and correct proposer/round choices can
vary. Honest nodes must agree within each run; fixed ordered workloads must yield
the expected state. Do not assert identical timestamps or all real arrival orders.

The virtual backend was removed. AGENTS.md records the student's approved
interpretation/overrides of the PDF simulated-network/strict log-byte conditions.
Tests also compare exact signed transaction order and the resulting state across
the normal and fault-injected runs. Faulted duplicate/reorder/replay frames must
have reached actual transmission, not just a planning log. Actual send timestamps
must obey the configured outbound rate, in addition to each planned hold.
Stage 5 still needs the full real T1-T7/adapted-T8 matrix, including multi-process
equivocation/crash scenarios. Stage 6 still needs the controlled rerun script,
report and archive. Production readiness, safe crash recovery, dynamic membership
and unrestricted network/workload support are not claimed.

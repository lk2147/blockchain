# Current checks - GossipSub and bidirectional gRPC

The transport revision uses actual libp2p GossipSub for signed broadcasts and
persistent gRPC Exchange streams for directed traffic. This page records test
execution, not a final submission report. Earlier 203-test results and the
selected logs/evidence describe the previous unary transport.

## Executed final full suites

From the repository root, with the existing installed Python runtime:

```text
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --output local-runs/transport-normal-final
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --race --output local-runs/transport-race-final
```

Both exited 0: all T1-T8, 205 passing tests/subtests, no missing scenarios and
no failed tests. Both checked gofmt -l src (clean) and ran go vet ./... (exit 0).
The race-enabled suite reported no races. The runner executes go test ./...
-count=1 -json, adding -race for the second command. Counts include parent tests
and subtests, not 205 distinct end-to-end scenarios. TestProcessNodeEntry is
skipped only in the parent and executed inside actual child processes.

Each evidence directory contains summary.json, raw go-test.jsonl, go-vet.log,
per-case configs, explicitly test-only keys, and independently produced node
events/results. They remain under ignored local-runs because they include keys.

## Scenario coverage

All cases launch ten real processes with n=10, f=3 and quorum seven. Honest
ledgers are re-executed and their signed finality certificates checked. All
recorded finalizations are checked for conflicts, not just the final snapshots.

| Case | Actual check and required outcome |
|---|---|
| T1 | Normal agreement on two-block history and expected state |
| T2 | Actual duplicated/reordered transmissions; no duplicate-created quorum, conflicting signing or finalization |
| T3 | Every honest node rejects/logs bad proposal signatures and wrong-domain votes, then progresses |
| T4 | Duplicate nonce batch rejected atomically; nine old-candidate replay sends verified; finalized ordered transactions applied once |
| T5 | Complete initial loss for 1200ms; no protocol send/finalization before it ends; later agreement |
| T6 | Separate silent and actually killed proposer cases; honest survivors time out and progress |
| T7 | Three Byzantine processes sign/transmit both candidates in both phases; seven honest nodes agree on one block |
| T8 | Two independent controlled runs preserve exact signed transaction order and expected state; actual drop/duplicate/reorder/replay sends required |

T1/T2/T8 additionally require each node to log membership, GossipSub connection,
publication and reception, and gRPC stream opening, sending and reception.
All integration cases retain actual hold-deadline/rate checks and matching
accepted-header-before-body gates. T7 targets one height; other cases target two.
T5's nonzero-delay coverage is provided by T2/T8; T5 itself stresses total loss.

New focused tests in hybrid_test.go passed in normal and race-enabled suites:

- TestGossipRelaysWithoutDirectConnection: authenticated 0--1--2 propagation
  across real sockets while 0 and 2 have no direct connection.
- TestStreamBidirectionalReconnectAndMembership: both ends write protocol frames
  on the shared session; closing/reconnecting preserves communication; mismatched
  membership and invalid handshake signatures are rejected by verification.
- The retained real socket rejection test now obtains the matching body_request
  over the same bidirectional RPC used to send the header. It still verifies
  invalid header/body rejection and temporary peer blocking/unblocking.

Existing crypto/state/block/consensus tests retain reference encodings, known
hashes/Merkle roots, ownership/nonces, locking/NIL and invalid certificate cases.

## Failure investigated during this change

The first full race run at local-runs/transport-race failed T2 because honest
nodes finalized only one of the required two blocks within the run. Its raw
failure evidence is retained. No data race was reported. Logs showed repeated
old finality-certificate publications competing with next-height traffic under
gossip replication and 100% duplication.

The production retry policy now re-advertises old finalized headers every fourth
retry tick while keeping current proposal/vote retries and initial decisions
prompt. Requested bodies remain immediate. The focused race T2 rerun at
local-runs/transport-race-t2-fixed passed, followed by both final full suites.
The duplicate policy, two-block requirement, quorum, safety, hold and state
assertions were not relaxed. Normal cancellation/deadline is treated as expected
PubSub shutdown rather than falsely logged as a close failure.

## Configuration and execution limits

The Python preflight accepted generated ten-node configs and rejected reordered
membership and missing GossipAddress endpoints. It also requires the config
directory to cover the complete configured validator set. Nodes check the shared
fingerprint in signed stream handshakes and log it at startup.

The final production launcher command also exited 0:

```text
python tests/run_local_cluster.py --go D:/tools/golang/bin/go.exe --faults --base-port 29750 --run-ms 15000 --run-dir local-runs/transport-cli-final
```

Ten distinct node processes each saved two matching blocks and root
a586608a2496b4cf1190c712c3467279081d1b22eafa3d7494d6266fcafb8321.
This fresh random-key experiment observed 18 drops, 32 duplicate, 23 reorder and
23 replay plans. Counts vary with real scheduling; the Go integration verifier
separately requires actual transmissions for its fault-coverage assertions.

Fault controls and send limits apply before authored gossip publications and
directed stream writes. Libp2p control/automatic relay traffic is outside those
artificial controls. This is not full kernel-packet conditioning. Connections
are loopback-only; process restart recovery, public deployment, a live Python
solver API and arbitrary configurable Byzantine strategies are not claimed.
See REAL_NETWORK.md for code paths and current usage. Final report/ZIP work is
deferred by the student until coding/testing is accepted.

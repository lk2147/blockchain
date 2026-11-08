# Current test status — rechecked 2026-11-09

Passing the existing suite does not mean all T1-T8 scenarios have been implemented
as separate real-network tests. Stage 5 and submission preparation are unfinished.

## Executed Go checks

| Command | Observed result |
|---|---|
| go test ./... -count=1 -json | Exit 0; 195 passing test/subtest events |
| go test -race ./... -count=1 -json | Exit 0; 195 passing test/subtest events; no reported data races |
| go vet ./... | Exit 0; no diagnostics |

Raw output is saved in local-runs/readme-review-tests/tests.log, race.log and
vet.log. Counts include parent tests and subtests, not 195 end-to-end scenarios.
Five packages have tests: crypto, state, block, consensus and realnet. Cmd/node,
network metadata, the node adapter, generated proto and types have no direct test
files; real integration tests exercise adapter/runtime behavior.

TestProcessNodeEntry is skipped in the parent and executed inside each child
process. It is an entry helper, not a skipped required scenario.

## Verified behavior

| Area | Existing evidence | Result |
|---|---|---|
| Crypto/encoding | Reference vectors, canonical bytes, tampering, wrong domains/chains | Passed |
| State/Merkle | Ownership, nonces, transaction replay rejection, atomic rollback, snapshot isolation, tree shapes/reference roots | Passed |
| Blocks | Parent/proposer/signature/body/root checks, invalid batches, parent links | Passed |
| Consensus | Unique votes, equivocation evidence, locks, NIL, timeouts, later certificates, old/future rounds | Passed |
| Real transport | Routing authentication, invalid headers/bodies over sockets, peer block/unblock | Passed |
| Real clusters | Ten distinct processes, two blocks each, independent execution/quorum verification and history/root agreement | Passed |
| Real faults | Drop, actual duplicate/reorder/replay sends, hold deadlines, send-rate limits, old-vote rejection | Passed |
| Ordered execution | Exact signed transaction order and state compared between normal/faulted runs | Passed |

Controlled message harnesses are focused protocol unit tests. There is no virtual
network execution backend; realnet supplies the actual-process evidence.

## T1-T8 coverage and remaining work

| ID | Existing coverage | Missing formal Stage 5 evidence |
|---|---|---|
| T1 | Real ten-node normal run, explicit workload results and authenticated finality | Named scenario entry/report |
| T2 | Vote-counting unit cases and real combined-fault run with duplication/reordering | Dedicated isolated real scenario and trace assertions |
| T3 | Unit signature/context checks; real bad transport/proposal signatures and body rejection | Dedicated real wrong-protocol-domain case and complete reason assertions |
| T4 | State/block transaction replay/duplicate/nonce unit tests | Duplicate/replayed transaction injection across actual processes |
| T5 | Real delay/drop run with faults later disabled and histories checked | Explicit pre/post-stabilization progress conditions; current finalization need not wait for disable time |
| T6 | Silent-proposer/timeout unit case | Actual silent/crashed process scenario |
| T7 | Ten-validator unit case with three equivocating validators | Real-process Byzantine equivocation at fault bound |
| T8 (student interpretation) | Normal/faulted real runs preserve transaction order/state; holds checked | Dedicated same-config controlled-rerun script/report, ignoring timestamps as agreed |

Safe crash recovery, encrypted remote deployment and public-network timing
guarantees have not been demonstrated.

## Python launcher

Python uses only its standard library and starts actual Go processes. Its result
check is a consistency check; the Go tests additionally re-execute blocks and
verify signatures/quorums independently. README gives Python-only quick-start,
prepare/edit, per-terminal and saved-result-check commands.

The recheck exercised ten separate `--node` Python invocations against one
prepared cluster, then `--check-only` with an intentionally nonexistent Go path
to confirm it does not build/start anything. All ten nodes saved matching
two-block histories/roots. Node 0's edited 30 ms hold survived launch and all
564 observed transmissions honored it. Reusing completed node output was
rejected as expected. Evidence: local-runs/readme-reviewed-manual.

The all-node `--faults` quick-start also passed using a fresh cluster:
ten nodes, two matching blocks each, 193 injected drops, 249 duplicates,
226 reorder holds and 287 replay injections. It also logged 68 send errors
handled by retries; all nodes still reached the expected result. These counts
are observed values, not predictions. Evidence: local-runs/readme-reviewed-auto.

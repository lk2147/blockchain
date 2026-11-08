# Test status — Stage 6, 2026-11-09

The named real-process T1-T8 suite is implemented and passed both normal and
race-enabled runs. The final Stage 6 race-enabled full suite also passed all
203 tests/subtests, formatting and vet. The separate Python repeatability command
passed. Selected raw evidence is in logs/evidence; see STAGE6.md for provenance,
archive checks and the review boundary.

## Actual complete-suite results

| Entry point | Result |
|---|---|
| python tests/run_tests.py --go D:/tools/golang/bin/go.exe --output local-runs/stage5-normal | Exit 0; all T1-T8; 203 passing tests/subtests; formatting and vet passed |
| python tests/run_tests.py --go D:/tools/golang/bin/go.exe --race --output local-runs/stage5-race | Exit 0; all T1-T8; 203 passing tests/subtests; no reported races; formatting and vet passed |

Both directories contain summary.json, go-test.jsonl, go-vet.log and per-case
configurations, logs, node results and verified.json. They are ignored by Git
because they include explicitly test-only private-key fixtures. Counts include
parent tests and subtests, not 203 independent end-to-end scenarios.

TestProcessNodeEntry is skipped only in the parent process; it runs inside the
actual child node processes. The runner refuses success if any T1-T8 case is
missing. Five packages have direct tests: crypto, state, block, consensus and
realnet; real integration tests exercise the production node adapter/transport.

## T1-T8 results

All cases use ten distinct Go processes communicating over real TCP/gRPC, with
n=10, f=3, quorum=7 and fixed, explicitly test-only key fixtures. Each honest
ledger is independently executed and every finality certificate authenticated.

| ID | Fault/input | Observed outcome | Invariant |
|---|---|---|---|
| T1 | Two ordered transactions, no injected faults | All ten nodes finalized the same two blocks/root | Normal agreement and execution |
| T2 | 100% duplication, 2-8 ms holds and every fourth admission held 40 ms longer | Real duplicate/reordered sends, duplicate rejections, matching two-block histories | At most one honest signed vote per slot; unique authenticated quorums |
| T3 | One faulty proposer sends an invalid proposal signature and wrongly domain-signed votes | Every honest node logged both rejection reasons; nine honest nodes progressed in a later round | Invalid signatures/domains cannot affect consensus |
| T4 | Faulty proposer sends a body containing nonce-1 transaction twice; after each peer finalizes, replays the old candidate | All nine honest nodes rejected the duplicate nonce and old candidate; finalized transactions occur once in the exact expected order | Atomic rejection and at-most-once execution |
| T5 | All outgoing protocol messages dropped for 1200 ms after a shared real-clock start, then injection disabled | No send/finalization before the loss window ended; all ten nodes subsequently finalized two blocks | Safety during loss, progress after stabilization |
| T6 | Silent proposer, then a separate run terminating the actual round-0 proposer process before consensus starts | Both cases triggered timeouts and later-round decisions; nine surviving honest nodes finalized two blocks | Faulty proposer cannot permanently stall the chain |
| T7 | Three faulty processes sign/transmit conflicting PREVOTEs/PRECOMMITs to two honest groups | All seven honest nodes finalized the same valid block; signed conflicts verified and detected | No conflicting finalization at the fault bound |
| T8 | Same fixed keys, ordered workload, seed and fault settings in two independent real runs | Same exact signed transaction order and root; timestamps/fault counts may vary | Student-approved repeatability interpretation |

T5 deliberately tests complete pre-stable loss. Configured delays/holds are
separately exercised by T2 and T8 and checked against actual RPC-attempt times.
T7 targets one block; other cases target two. Faulty/crashed processes are not
counted as honest and are never assigned successful ledger results.

## Other checks retained

Crypto reference vectors/domain separation; ownership/nonces; Merkle reference
roots/tree shapes; block validation; vote counting, locking, NIL, late/future
rounds; real transport authentication; invalid header/body delivery; actual peer
blocking/unblock; outbound rate and header-before-body gates remain tested.
Recorded finalization events are inspected as well as saved results, checking
that no honest process decided twice or that different honest decisions disagree.

The final targeted replay/rerun check was also rerun with race detection after
adding packet-ID binding for the deliberate T4 replays and requiring actual
fault transmissions in both T8 runs; it is reported in STAGE5.md.

## Regular launcher

The production CLI was rebuilt and run through the Python launcher after the
peer-IO change. All ten processes produced matching two-block histories/root.
Evidence: local-runs/stage5-cli-smoke. Observed injections were 190 drops,
251 duplicates, 220 reorder holds and 297 replays, plus 73 retried send errors.
These are observed counts, not fixed predictions for another real run.

## Review boundary and limitations

Stage 6 prepared the report, archive/structure verification and final evidence.
The student must still review the report and demonstrate understanding before
submission. Safe crash recovery/restart, encrypted remote deployment,
production readiness and exhaustive exploration of all Byzantine schedules are
not claimed. T6 tests crash tolerance of the surviving cluster, not restart safety.

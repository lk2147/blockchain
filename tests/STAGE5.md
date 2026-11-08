# Stage 5 — real-process integration and T1-T8

## Result and entry point

All named T1-T8 scenarios passed in normal and race-enabled full-suite runs.
Both complete runs recorded 203 passing tests/subtests and successful static
checks. Stage 6 has not started; the submission report/archive are not complete.

Run from the repository root:

```text
python tests/run_tests.py
python tests/run_tests.py --race
```

If necessary, add `--go D:/tools/golang/bin/go.exe`. Use `--output <fresh-path>`
to choose the evidence directory. The standard-library Python entry checks
formatting, executes unit and real-network tests, requires all eight scenario
names to pass, and runs go vet. It returns an error on missing/failed scenarios
instead of treating a partial run as success. Expect a few minutes for the suite.

## Files created/changed

- `src/realnet/stage5_test.go`: ten-process fixtures, common real-clock startup,
  T1-T8 scenarios and an independent verifier over ledgers/certificates/events.
- `src/realnet/adversary_test.go`: actual faulty peer programs used only by tests.
  They operate real sockets and their own configured private keys.
- `src/realnet/realnet_test.go`: preserves authentication/socket/policy tests;
  the old combined cluster smoke case was replaced by the named suite. The
  process-entry helper can launch honest or explicitly faulty test processes.
- `src/realnet/run.go`: independent peer IO, a global transmission rate gate,
  globally bounded scheduled/queued/inflight messages, and an owner-loop endpoint
  boundary allowing a test process to behave Byzantine without changing an
  honest engine. Production always constructs the ordinary node runtime.
- `src/realnet/config.go`: optional StartAtUnixMS for controlled experiments.
- `tests/run_tests.py`: one suite entry point and saved evidence.
- README, AGENTS.md and test/network notes: current commands, stage status and
  observed outcomes. No Protobuf, consensus algorithm or Merkle scheme changes.

## Important design decisions

### A faulty peer cannot stall every healthy connection

The earlier sender serialized RPCs through one IO worker. Stage 5 isolates each
peer's IO, so a failed connection's timeout cannot hold up healthy peers. A
shared gate still enforces SendGapMS between actual RPC attempts across the
whole node. An atomic queue count includes scheduled, dispatcher, per-peer and
inflight messages, preserving the configured node-wide capacity.

This does not parallelize consensus. Each node's proposals, votes, locks and
execution remain on its single owner loop. Workers only send immutable frames
and write synchronized logs. Race detection was executed, not assumed safe.

### Real startup coordination is not a finalization coordinator

Each test starts ten independent OS processes, waits for all listening servers
to be ready, and gives them a common StartAtUnixMS timestamp. The fault window
uses that real-clock epoch. Timers and network connections remain real, with
OS scheduling variability. No controller supplies a ledger/state or chooses a
finalized block. The consensus engine does not know stabilization time.

Normal generated cluster configs leave StartAtUnixMS at zero and start
immediately. The configured run duration still begins when a process starts;
allow enough time after a future experiment start for the intended workload.

### Byzantine behavior is confined to declared faulty validators

Faulty peers are separate test processes, not mutations of honest engine state.
The test-only driver receives only its own configured private key, public peer
identities and preloaded signed client inputs. It cannot sign as honest peers.
It creates real signed headers/votes and uses the same authenticated transport
and header/request/body gates. These programs are compiled into the Go test
binary, not the production node CLI. Their empty ledgers are never counted as
successful honest results.

T7's proposer creates two individually executable blocks differing in its own
account value. The three Byzantine validators send conflicting votes in both
phases. Four honest validators initially see A and three see B: A can obtain
seven votes; B's six cannot finalize. All seven honest nodes eventually verify
the same valid decision. Each signed conflict is checked independently and bound
to the actual transmitted packet ID, phase and block hash.

### Independent evidence, not success flags

For every honest process the verifier:

1. Confirms its PID matches the launched OS process.
2. Compares the exact signed transaction sequence with the expected workload.
3. Re-executes blocks from an empty state with trusted parent/proposer context.
4. Verifies every finality signature/scope and at least seven distinct members.
5. Compares the result with the independently executed expected workload state.
6. Checks complete finalized histories across honest nodes.
7. Reads all recorded finalization events, rejecting any repeated height or
   conflicting honest decision, even if a later saved result looked consistent.
8. Checks one honest vote-signing event per height/round/phase, actual send rate,
   planned hold deadlines and matching signed body acceptance before body sends.

These are concrete finite test schedules, not a formal proof over every possible
Byzantine execution. Existing focused locking/negative tests remain valuable.

## Scenarios, expectations and actual outcomes

Common setup: n=10, f=3, quorum=7, fixed explicitly TEST-ONLY Ed25519 fixtures,
real loopback gRPC, 1 ms outbound gap and 300 ms retransmission interval. Tests
write the same owner's key-value record at nonce 1, then nonce 2, so changing
transaction order or replaying an update cannot be hidden by independent writes.
T7 targets one height; other cases target two. Processes run for eight seconds,
with a common start about two seconds after launch configuration begins.

| ID | Initial conditions/input or fault | Expected outcome/invariant | Actual normal-run evidence |
|---|---|---|---|
| T1 | Ten honest nodes, no injected faults | Same two-block history and expected state | 20 honest finalization events; no round timeouts |
| T2 | Ten honest nodes; duplicate every message, 2-8 ms holds, every fourth admission held 40 ms longer | No duplicate counting/signing; no conflicting finalization | 4,197 actual duplicate sends, 1,043 reordered sends; same two-block histories |
| T3 | One faulty round-0 proposer; invalid proposal signature and votes signed under the wrong domain | Every honest node rejects/logs both types, then progresses | Nine honest nodes finalized two blocks in later rounds; both rejection reasons checked per node |
| T4 | One faulty proposer sends a body containing the nonce-1 transaction twice, then replays that old candidate to each peer after its decision | Atomic rejection and at-most-once application | Every honest node rejected duplicate nonce and old-height proposal; nine deliberate replay sends verified; nonce sequence 1,2 finalized once |
| T5 | Every protocol transmission dropped for 1,200 ms, then fault injection disabled at the shared epoch | Safety throughout loss; progress only after communication resumes | 342 injected drops; 10 disable events; no send/decision before window ends; all ten finalized two blocks |
| T6 | Separate silent-proposer and actual proposer-process termination runs | Timeout/round advancement; surviving honest chain progresses | Both variants: nine honest nodes finalized two blocks; 18 timeout events per run |
| T7 | Three actual Byzantine processes equivocate in both voting phases, sending two valid candidate histories | No conflicting honest finalization at f=3 | Seven honest nodes finalized A; both authentic conflicts from each faulty key/phase actually transmitted; 84 conflict-rejection log events |
| T8 | Two independent runs with identical keys, workload, fault seeds and policy | Exact signed transaction order and final state agree; timestamps may vary | Both roots identical; all ten nodes agree within each run; configured drop/duplicate/reorder/replay actions actually exercised |

T5 intentionally stresses complete loss before stabilization. Nonzero configured
delays and reorder holds are covered by T2/T8, with deadlines checked against
actual sends. T6 terminates the proposer before consensus starts; it does not
claim restart/recovery. T4 sends an invalid duplicate batch and later replays
that candidate over real connections; existing state tests also enforce nonce
progression across successful execution snapshots.

The expected two-height state root for this fixed test workload is:

```text
9fb497b5fbc68ed4eccf7f153742e02b87c3febb42bb54957da8cd9742fc38f6
```

T7's one-height root is:

```text
60459d58fd7c12f67f23dc9b04ad416351339fe6e31a957b3384b2fa49bcf414
```

Different roots here reflect different height counts, not disagreement. These
values are observed results and independently recomputed by tests, not constants
used by consensus. Fresh development clusters generate different keys/inputs.

## Actual commands and evidence

Executed complete suites:

```text
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --output local-runs/stage5-normal
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --race --output local-runs/stage5-race
```

Both exited 0: all T1-T8, 203 passing tests/subtests, clean gofmt and successful
go vet. No races were reported. The child-entry helper is skipped only in the
parent; it is executed inside every child. No scenario is skipped.

After tightening T4 packet binding and T8 actual-fault-send assertions, this
targeted race-enabled rerun and vet also passed:

```text
go test -race ./src/realnet -run "TestStage5/T(4|8)$" -count=1
go vet ./...
```

Each full-suite evidence directory contains raw Go JSON events, a suite summary,
vet output and subdirectories for T1-T5, both T6 variants, T7 and both T8 runs.
Per-case files include configs, keys, per-node logs/results and verified.json.
They remain Git-ignored because they contain test key fixtures. No private keys
are printed into logs or used as real development identities.

The production Python launcher was also rebuilt/run after peer-IO changes:
local-runs/stage5-cli-smoke contains ten matching two-block results. Observed
injections: 190 drops, 251 duplicates, 220 reorder holds and 297 replays, with
73 retried send errors. Counts can vary under real scheduling. During development,
focused builds initially failed on a log-field naming collision and an unused
import; these were fixed before successful runs. Assertions were not weakened.

## Viva points and remaining work

- A bad proposer can send plausible signed data; honest nodes still execute and
  validate it before supporting it.
- Duplicate transactions differ from duplicate packets: nonce rules protect
  execution, while validator/height/round/phase keys protect vote counting.
- Crashing a proposer changes who can communicate, not the quorum threshold.
- Equivocation uses genuine signatures on conflicts; it cannot manufacture
  honest signatures or seven distinct validators from six votes.
- Delays/loss change message availability and timing; they do not authorize a
  block or change application execution order.
- T8 compares ordered effects/state under controlled inputs, not timestamps or
  incidental socket arrivals, following the student's approved interpretation.

At this stage boundary, Stage 6 still needed submission preparation and review.
The student subsequently authorized Stage 6; STAGE6.md records its final checks,
report, selected evidence and archive. Safe crash recovery, encrypted
remote/public deployment, dynamic membership and production readiness remain
outside the implemented scope. Student review remains required before submission.

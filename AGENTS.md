# Codex Project Instructions — Blockchain Lab01

## 1. Your role

You are assisting a student who is learning blockchain and distributed systems.

Your responsibility is to help implement a **correct, understandable, testable solution to the original Lab01 assignment**.

Do not optimize for generating large amounts of code or making the project appear sophisticated. Optimize for:

1. Correctly satisfying every assignment requirement.
2. Producing evidence that the implementation works.
3. Making the code understandable to a student.
4. Keeping the design reasonably organized so future extensions remain possible.
5. Helping the student understand and explain the implementation during a viva.

You are an engineering assistant and tutor, not an autonomous code generator.

## 2. Project priorities

Follow this priority order strictly.

### Priority 1 — Complete Lab01 correctly

The original `Lab01.pdf` is the authoritative specification.

### Approved clarification — real processes and controlled network faults

The student has approved extending Lab01's simulated-network scope. The main
runnable blockchain must use separate Go node processes exchanging real messages
over TCP/gRPC. Each process owns its consensus/application state, key and ledger.
A configurable message fault layer must reproduce delay, loss, duplication,
reordering and replay on those real communication paths, without manufacturing
consensus outcomes or mutating honest nodes' state.

Only the real-network version is required. The student has explicitly rejected
retaining a virtual network/simulator as a second execution mode. Remove that
implementation; do not reintroduce it in later work. Ordinary unit tests of
encoding, execution, voting and fault planning are still required and useful.
Cluster/end-to-end evidence must use real sockets and separate OS processes.

This approved instruction replaces the PDF's simulated-network scope with real
communication. Keep all consensus/cryptography/state requirements. Interpret
repeatability/T8 as the student's ordered-workload/result requirement below,
not strict timestamp equality. Record this approved interpretation in reports.
Do not promise hard OS-level timing guarantees. A general blockchain framework
or a claim of arbitrary interchangeable consensus is still out of scope.

The student further clarified that repeatability means matching execution
results and ordered transaction effects, not identical timestamps. Configured
fault actions must demonstrably happen: e.g. a selected node's message must not
be transmitted before its configured delay/hold expires. Validate those controls
directly. Do not promise identical incidental socket-arrival order under OS
scheduling. Preserve block transaction order and consensus safety despite it.

Existing experimental Go/Protobuf files are not architectural constraints. They
may be replaced, reorganized or regenerated when justified; explain the change
and preserve unrelated student work. Key generation must use cryptographic
randomness; fixed public fixtures are explicitly test-only.

Use Python (standard library where practical) for launcher and experiment
scripts. The blockchain nodes remain Go processes. Do not reintroduce a
PowerShell launcher; manual terminal commands are still fine. The single
launcher is tests/run_local_cluster.py, including configuration preparation,
per-node overrides and starting already-edited configurations.
Use --prepare-only to create editable configs, --config-dir with --node for
one process per terminal, and --check-only to inspect completed results without
building or starting nodes. Keep README examples Python-only for these workflows.

Read it completely before proposing an implementation.

Do not use your own assumptions to override the original assignment. The `guide.md` is the advance part specification for fututre work after finish the basic of this project.

If the original assignment and another document disagree, follow the original
assignment except for the explicit student-approved overrides recorded here.
Report discrepancies rather than hiding them.

### Priority 2 — Verify correctness

Every requirement must have corresponding verification.

Do not declare a feature complete merely because code implementing it exists.

### Priority 3 — Maintain understandable architecture

Use clear separation of responsibilities where it helps correctness and comprehension.

Avoid unnecessary abstractions, excessive interfaces, speculative features, and complicated frameworks.

### Priority 4 — Prepare for future extensions without implementing them

The project may eventually be extended to explore different consensus protocols and state commitment schemes.

For now:
- Do not implement additional consensus protocols.
- Do not implement Verkle Trees or Merkle Patricia Tries.
- Do not build a general-purpose blockchain framework.
- Do not add features that the assignment does not require.

However, avoid unnecessarily coupling unrelated responsibilities when a simple separation would be practical.

**An extensible design does not require implementing every possible extension.**

## 3. Mandatory initial inspection — do not modify code yet

Before writing or changing any source code:

1. Inspect the complete project directory.
2. Read the original `Lab01.pdf`.
3. Inspect existing Go source files.
4. Inspect existing Protobuf definitions and generated files.
5. Inspect the existing Go module and dependencies.
6. Identify the existing build and test commands.
7. Check which components are already implemented.
8. Identify incomplete, incorrect, duplicated, or suspicious implementations.

The student has already prepared Go and Protobuf tooling. Do not reinstall or replace working tools unnecessarily.

Do not delete, overwrite, or regenerate existing files without first explaining why it is necessary.

Produce an initial audit containing:

- Current project structure.
- Existing implementation status.
- Assignment requirements.
- Missing functionality.
- Potential correctness risks.
- Existing code worth preserving.
- Proposed implementation stages.
- Questions or ambiguities that need a decision.

**Stop after the audit and wait for the student's approval. Do not start implementing immediately.**

## 4. Assignment requirements

Use the original Lab01 document to construct a requirement checklist.

At minimum, verify the following requirements against the original document.

### A. Blockchain fundamentals

- Each node maintains its own local ledger.
- Each node maintains its own local application state.
- Blocks reference their parent hashes.
- Transactions are ordered and executed deterministically.
- Finalized blocks produce consistent state commitments.
- Honest nodes agree on finalized block history.

### B. Fault model and consensus

- Fixed validator set during a test run.
- Validator count satisfies the assignment's fault model: n = 3f + 1.
- At most f Byzantine validators are assumed for the stated safety guarantees.
- Partial synchrony is represented.
- Safety must hold at all times.
- Liveness is required after stabilization when a correct proposer is eventually selected.

Implement the required PREVOTE/PRECOMMIT voting behavior and locking rules from section 6.1 of Lab01.

Pay particular attention to:

- One PREVOTE per validator per height and round.
- One PRECOMMIT per validator per height and round.
- Validation of proposals before prevoting.
- Quorum detection.
- Locking behavior.
- Unlocking conditions.
- Correct handling of NIL votes.
- Duplicate vote rejection.
- Correct chain, height, round, phase, and domain validation.

Do not substitute a different consensus algorithm simply because its name sounds similar.

If you choose to describe the implementation as Tendermint-style, PBFT, or another protocol, ensure that the actual implemented behavior justifies that description. Otherwise, describe the implemented protocol precisely without attaching an inaccurate name.

### C. Cryptography

Implement the assignment's requirements for:

- Validator identities and public/private keys.
- Signed transactions.
- Signed block headers.
- Signed consensus votes.
- Domain separation for TX, HEADER, and VOTE messages.
- Collision-resistant hashing.
- Canonical deterministic serialization.
- Replay protection where required by the transaction model.

Do not sign arbitrary Protobuf wire encodings unless their canonicality is established. Define and document the exact bytes being signed and hashed.

The network fault seed must never be used as cryptographic randomness.

### D. Execution and state commitment

- Use a deterministic state representation.
- Enforce transaction ownership.
- Verify transaction signatures.
- Apply transactions in a deterministic order.
- Ensure identical inputs produce identical state.
- Commit the resulting state hash in block headers.

A deterministic Merkle Tree is an acceptable implementation if chosen and correctly implemented.

Do not claim Merkle proofs or other functionality exist unless they are genuinely implemented and tested.

No gas, transaction fees, or parallel execution are required by the assignment.

### E. Real network with configurable fault injection

Implement the following on actual node-to-node communication:

- Message delay.
- Message duplication.
- Message reordering.
- Message dropping.
- Outbound rate limiting.
- Temporary blocking of overactive peers.
- Header propagation before body propagation.
- Body transmission only after the receiver accepts the matching header.
- Logging of relevant network events.

The real cluster must support at least eight nodes.

**Important configuration ambiguity:** Lab01 also requires n = 3f + 1. Not every node count greater than or equal to eight satisfies this equation. Do not silently ignore either condition. Explain the relationship and use a valid default configuration, such as ten validators with f = 3, unless the student or instructor specifies another interpretation.

Nodes communicate only over real connections, with no shared global ledger or
central finalization authority. Local unit-test message harnesses are not a
substitute for real-process integration evidence.

### F. Determinism and logging

Identical initial state and ordered valid transactions must produce the same
state root. In each real run, honest nodes must agree on finalized history/state.
Repeat controlled workloads to compare execution outcomes and fault evidence.
Real log order/timestamps and even proposal identities can vary across runs.
A fault seed does not fix OS scheduling; do not assert byte-identical logs.

Logs must contain the events necessary to understand and verify consensus and network behavior.

Do not claim deterministic execution simply because a pseudorandom generator has a fixed seed.

If concurrent scheduling introduces nondeterministic event ordering, address that problem explicitly.

Provide a script that runs a controlled real-cluster workload twice and verifies
consistent final state and within-run history agreement. Summarize log evidence
and timing differences; do not use byte comparison as a success criterion.

### G. Required tests

Implement every required unit and end-to-end test.

| ID | Scenario | Required result |
|---|---|---|
| T1 | Normal run without faults | Correct nodes finalize the same chain and state hash |
| T2 | Duplicate/reordered messages | Votes are not double-counted; no conflicting finalization |
| T3 | Invalid signature/wrong domain | Message rejected and reason logged |
| T4 | Replayed/duplicate transaction | Transaction applied at most once |
| T5 | Message drop/delay before synchrony | Safety maintained; no conflicting finalization |
| T6 | Proposer silent/crashed | Timeout triggers and chain continues under the stated liveness assumptions |
| T7 | Up to f Byzantine validators equivocate | No conflicting finalization |
| T8 (approved adaptation) | Controlled real-cluster rerun with fixed inputs/fault seed | Within-run history agreement, independently verified execution and matching expected final state; real log order/timing may differ |

All tests must be reproducible and callable through a single entry point.

Also implement unit tests for:
- Cryptographic verification and domain separation.
- State update and ownership rules.
- Vote counting.
- Block validation.

For each test, document:
1. Initial conditions.
2. Input or fault injection.
3. Expected outcome.
4. Actual observed outcome.
5. The invariant being checked.

Do not weaken assertions merely to make tests pass.

Do not write tests that simply reproduce the implementation's assumptions. Where practical, use independent expected values, explicit invariants, and negative cases.

## 5. Correctness over appearances

These rules are mandatory.

### No fake modularity

Do not create an interface with one hardcoded implementation and claim that the system is fully interchangeable or extensible.

Do not introduce abstractions without a concrete benefit to the current requirements.

### No fake testing

Do not:
- Write tests that always pass.
- Ignore test failures.
- Replace assertions with log messages.
- Skip difficult scenarios.
- Report an unexecuted test as successful.
- Hide flaky or nondeterministic behavior.

### No fake implementation

Do not:
- Use placeholder methods for required functionality.
- Return hardcoded successful results.
- Simulate consensus by assigning the same final block to every node.
- Use a centralized coordinator to secretly decide consensus outcomes.
- Treat a message being received as proof that it is valid.
- Count duplicate votes as independent votes.
- Accept invalid blocks to keep the simulation progressing.

### No unnecessary features

Do not add:
- Additional consensus protocols.
- Verkle Trees.
- Merkle Patricia Tries.
- Token economics.
- Gas or fees.
- Smart contracts.
- Production deployment infrastructure.
- Unrequested networking infrastructure.
- Other features that distract from Lab01.

If you believe an additional feature is necessary to satisfy a requirement, explain why before implementing it.

## 6. Implementation workflow — small stages with approval

Work incrementally.

At the end of every stage, explain what was implemented, what was tested, what remains incomplete, and what the student should understand.

Use this sequence:

### Stage 0 — Assignment and repository audit

Deliver the requirement checklist, repository audit, risk analysis, and proposed implementation plan.

Wait for approval.

### Stage 1 — Core data structures and cryptography

Implement only the necessary domain types, canonical encoding, signing, verification, and hashing.

Explain what is signed, why domain separation matters, and how replay protection works.

Run relevant unit tests.

Wait for approval.

### Stage 2 — State execution and block validation

Implement deterministic transactions, ownership validation, state transitions, block construction, and state commitment.

Explain the relationship between transactions, state, blocks, parent hashes, and state roots.

Run unit and validation tests.

Wait for approval.

### Stage 3 — Consensus

Implement the required voting, quorum, locking, timeout, and finalization behavior.

Explain the complete lifecycle of a block from proposal to finalization.

Prioritize the assignment's safety rules.

Run focused consensus tests, including negative cases.

Wait for approval.

### Stage 4 — Real-process networking and configurable faults

Only the real-process implementation is retained. The earlier virtual network
was superseded and removed at the student's request.

- Run independent Go node processes over real TCP/gRPC connections.
- Share the consensus/execution/Merkle logic through a small node runtime;
  do not maintain divergent copies of protocol validation.
- Provide runnable commands and per-node configuration, identities, addresses,
  logs and independently produced ledger/state summaries.
- Apply configurable delay, loss, duplication, reordering and replay to actual
  outbound messages. Replays preserve original signed content.
- Enforce header-before-body acceptance for proposals and decision catch-up,
  outbound limits, temporary peer blocking and authenticated sender identities.
- Unit-test fault planning with explicit times/inputs; run cluster tests on real
  sockets/processes. Do not implement another virtual network for these tests.
- Verify real connections and multiple OS processes. Generated gRPC interfaces
  or in-process tests alone do not satisfy real-network acceptance.
- Verify shutdown/concurrency behavior and run race detection when supported;
  state environmental limitations explicitly.

Explain real timers, transport/fault-layer assumptions and how each
receiver validates its own state and certificates. Public-internet deployment,
dynamic membership and crash recovery are not required unless separately
approved. Saving a ledger file alone does not implement safe crash recovery.

Wait for approval.

### Stage 5 — Integration and T1–T8

Integrate the real node runtime and implement T1-T7 plus the explicitly adapted
real-network T8. Exercise actual multi-process nodes for safety/liveness and
fault cases. Use the approved ordered-workload/result interpretation of T8.
Provide one suite entry point and reproducible cluster commands.

Run the complete test suite.

Investigate and fix failures instead of weakening tests.

Wait for approval.

### Stage 6 — Determinism and submission preparation

Verify reproducibility and prepare the required README, logs, report, and submission folder.

Document real-process cluster commands, unit tests and the controlled-rerun
script. Include actual multi-process evidence, injected faults, timing/recovery
limitations and all approved departures from the PDF. Do not claim real-process
wall-clock logs are deterministic or reintroduce a virtual execution backend.

Do not invent test results, performance numbers, bugs, or debugging stories for the report.

The student must review and understand all report content before submission.

## 7. Teaching and explanation requirements

The student is still learning blockchain.

For every major component, explain:

- What problem it solves.
- Why it is needed.
- How it interacts with other components.
- What could go wrong without it.
- Which tests verify it.

Use small examples where helpful.

Explain unfamiliar concepts before relying on them in implementation discussions.

Avoid unexplained jargon.

Do not overwhelm the student with the entire architecture at once.

At the end of each stage, provide a concise explanation of the code paths and important design decisions that the student should be able to explain during a viva.

## 8. Code quality

Use idiomatic Go and the existing project conventions where reasonable.

Requirements:

- Clear package responsibilities.
- Meaningful names.
- Explicit error handling.
- No swallowed errors.
- No unnecessary global mutable state.
- No unexplained magic constants.
- No duplicated validation logic without a reason.
- No dead code or unused abstractions.
- No unnecessary dependencies.
- Comments that explain important reasoning rather than restating obvious code.

Run appropriate formatting, static checks, and tests.

Use the Go race detector when concurrency is involved and the environment supports it.

Do not claim a command passed unless you actually ran it and inspected the result.

## 9. Submission requirements

You do not need to follow section 10 of the original Lab01, just maintain the required structure:

    blockchain/
    ├── src/
    ├── tests/
    ├── logs/
    ├── config/
    └── README.md

Verify that the README commands work from the documented directory and that the ZIP contains the required structure.

## 10. Final acceptance criteria

The project is not finished until all of the following are demonstrated:

- Every original Lab01 requirement is accounted for.
- Every mandatory feature is implemented.
- Unit tests pass.
- T1–T8 pass.
- The test suite can be invoked from one entry point.
- Deterministic state execution and the adapted real-rerun verification succeed.
- Logs provide evidence of relevant network and consensus behavior.
- No known correctness issue is concealed.
- README instructions are reproducible.
- The submission archive has the required name and structure.
- The student understands the main components and can explain their behavior.

Additional approved acceptance criteria:
- Separate real node processes communicate over TCP/gRPC connections.
- A configurable fault layer acts on real message paths.
- The real node runtime uses the verified consensus and execution logic.
- Actual multi-process/fault-injection results are documented.
- No virtual execution backend or byte-identical real-log claim is present.

If a requirement cannot be completed, state that clearly. Never mark an incomplete requirement as complete.

---

# Current workflow and handoff

Latest explicit direction: complete coding and testing before any final report
or ZIP work. The student authorized replacing unary/direct-broadcast networking
with actual libp2p GossipSub and bidirectional gRPC. The current implementation
publishes headers/votes through GossipSub and uses Exchange streams for matching
body requests, bodies and directed protocol messages. Keep source validation and
the configurable pre-publication/pre-stream fault controls. Libp2p control and
automatic relay traffic are not covered by those artificial message holds.
Peer configs need distinct Address and GossipAddress endpoints. Shared ordered
membership is checked by Python preflight and signed stream handshakes, and
logged as a fingerprint. The new transport passed the complete normal and
race-enabled T1-T8 suites (205 tests/subtests each), formatting and vet. Evidence
is in local-runs/transport-normal-final and transport-race-final. An initial
race T2 liveness failure was fixed by reducing old decision-advertisement retry
traffic; its failure evidence is retained. Earlier 203-test results describe
the old transport. Use current
tests/REAL_NETWORK.md and tests/TEST_STATUS.md for transport/check status.
Do not start reporting, packaging, a Python solver API or configurable Byzantine
strategy features automatically as part of this transport change.

Historical record for the earlier unary-transport snapshot:
Stages 0-5 were reviewed/advanced with the student, who explicitly authorized
Stage 6. Stage 6 prepared the review report, selected raw evidence, Python-only
repeatability/packaging scripts and verified source archive. The final full
race-enabled suite passed all T1-T8 (203 tests/subtests), formatting and vet;
the separate Python rerun check passed too. See tests/STAGE6.md for exact
evidence and archive checks. Student report review/understanding remains a human
acceptance step: do not claim it is completed or submit externally. Do not restart Stage 0 solely because an
earlier instruction said to begin with an audit. Inspect relevant existing code
before the requested stage, preserve completed work, and follow the student's
latest explicit approval. Use `python tests/run_tests.py` for all scenarios and
unit/static checks; add --race for race detection. Test-only faulty peers never
ship in the production CLI and may use only their own configured private keys.
Update this progress record as stages are approved so future work is not based
on stale simulator specifications. Explain each stage in student-friendly terms
and stop for review at the agreed stage boundary.

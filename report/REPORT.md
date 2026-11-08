# Blockchain Lab01 - implementation and evidence

Prepared on 2026-11-09 for student review. This report describes the implemented
lab, not a production blockchain. Team names and student IDs have not been
provided and are not invented. The student should supply any institutional
cover sheet required by the instructor.

## 1. Scope and agreed interpretation

The starting specification is the original Lab01.pdf. The student approved
separate real node processes communicating over TCP/gRPC instead of a virtual
network. Configurable message faults operate on those real communication paths.
Repeatability means identical ordered transaction effects and expected state,
not byte-identical timestamps or incidental arrival logs. These are explicit
departures from the original simulator/log wording, recorded in AGENTS.md.

The submission structure follows AGENTS.md section 9 rather than the original
PDF section 10 naming convention: blockchain/src, tests, logs, config and
README.md, with module files and this report. The default ZIP is blockchain.zip.

Ten validators satisfy both n>=8 and n=3f+1: f=3, quorum=7. Eight validators
would not satisfy this equation. Validator membership remains fixed in a run.
Honest nodes own independent ledgers, application state and signing keys.

## 2. Responsibilities and data flow

| Location | Responsibility |
|---|---|
| src/types, src/crypto | Message shapes, exact signed bytes, hashes and Ed25519 verification |
| src/state | Owned key-value execution, nonces and deterministic Merkle root |
| src/block | Parent/body/state checks and candidate construction |
| src/consensus | Proposals, PREVOTE/PRECOMMIT, locks, rounds and finality |
| src/node | Connect protocol actions to local execution and transport |
| src/realnet | Real sockets, authenticated transport, faults and logs |
| src/proto, src/cmd/node | gRPC schema/generated bindings and runnable CLI |
| tests/*.py | Prepare/run processes, run checks and package reviewed files |

The message flow is:

```text
Signed client inputs -> proposer builds block -> signed proposal/header
  -> receiver checks header -> receiver requests that exact body
  -> body hash + transaction execution + state root validated
  -> PREVOTE quorum -> lock + PRECOMMIT -> PRECOMMIT quorum
  -> each honest node appends its own ledger and commits its own state
```

The launcher starts processes; it does not choose the final block. Tests may
terminate a process or inject bad messages through peers' real connections.
They never assign an honest node a successful ledger. Faulty test peers are
compiled only in tests and can sign only with their own configured keys.

## 3. Cryptography, replay and execution

Versioned canonical payloads use fixed field order, big-endian integers and
length-prefixed variable bytes. Ed25519 signs explicit TX, HEADER or VOTE domain
bytes and the chain context. Verification checks the expected chain ID as well
as the signature. Proposal and transport envelopes have their own domains.
Protobuf transports messages but its arbitrary wire encoding is not signed.
See tests/STAGE1.md for the exact canonical field layout.

A TransactionID is SHA-256 of the complete canonical signed transaction,
including its signature. Execution primarily prevents replay with the sender's
next nonce: 1, then 2, and so on. Keys have the form
<lowercase sender public-key hex>/<nonempty record>. A sender cannot edit another
sender's namespace. Every transaction is verified; an invalid batch changes
nothing. Candidate execution occurs on a copy and becomes local state only
after finalization. Keys for real experiments come from cryptographic randomness;
reproducible tests use explicitly test-only fixtures, never the fault PRNG.

The header commits to the ordered signed transaction body with SHA-256 and to
the executed state with its Merkle root. A signature on a header therefore
authenticates a particular body commitment, but the body must still arrive and
pass all checks. Parent hashes link finalized history. The receiver cannot vote
merely because it received a signed header.

The deterministic binary Merkle tree sorts typed record/nonce leaves, hashes
leaves and internal nodes with distinct prefixes, duplicates the final odd
node at each level, and wraps the tree hash with version, chain and leaf count.
Nonces are committed too, so replay-relevant state cannot silently differ.
This is an ordinary Merkle commitment; membership proofs, MPT and Verkle trees
are not implemented. The state package is separate from consensus so another
studied commitment can be introduced without changing voting rules.

## 4. Voting and locks

The protocol implements Lab01's round-based PREVOTE/PRECOMMIT rules. It is not
classical PBFT. A valid block may receive one PREVOTE per validator/height/round;
NIL represents no acceptable candidate. Seven distinct authenticated non-NIL
PREVOTEs establish a lock and permit PRECOMMIT. Seven matching PRECOMMITS permit
finalization. Duplicate packets do not create extra validators or signatures.

A locked validator cannot simply follow another proposal: a later-round quorum
is needed to switch the lock. NIL alone does not unlock it. Timeouts advance
rounds; proposer rotation eventually gives a correct proposer a turn under
the stated liveness assumptions. Reproposal carries evidence while retaining
the immutable block identity. A lagging node verifies a finalized block and
its certificate before advancing; a certificate alone does not bypass execution.

At f=3, two seven-vote quorums among ten intersect in at least four validators.
At least one of those is honest. This motivates the signing/locking rules;
finite tests demonstrate selected schedules, not a formal proof for every
possible adversarial execution.

## 5. Real network controls and logs

Each Go process listens on its own loopback TCP port. Signed transport envelopes
bind chain, sender, recipient, sequence, kind, reference, height, round and payload.
Receiver authentication occurs before protocol delivery. Message delays hold
actual sends; drops omit sends; duplicates retransmit; reordering adds holds;
replay retransmits eligible old signed votes. These are application-message
faults on real gRPC connections, not kernel-level packet impairment.

Independent peer IO avoids one unavailable peer holding up all others. A
shared gate limits actual outbound RPC attempts; bounded queues limit admitted
work. Per-peer inbound limits temporarily block an overactive peer, then allow
it again. Header acceptance/request tracking gates body transmission and binds
it to the matching advertised header. Retries permit recovery from loss.

An optional shared real-clock start aligns controlled experiments; no virtual
clock drives consensus. Faults can stop after a configured interval to represent
stabilization. Events record node, sequence, time, type and relevant protocol
metadata. Raw local logs and saved results provide evidence; rejected messages
are not counted as successful protocol actions.

## 6. Required scenarios and invariants

Every case uses ten real processes with explicit test-only identities. The
checker independently executes honest histories, verifies parent/body/state
commitments and seven-signature certificates, and inspects finalization events
for conflicts. It checks send deadlines/rates and header-before-body behavior.

| Test | Initial conditions / injection | Expected and checked observed outcome |
|---|---|---|
| T1 | Honest ten-node cluster, no faults | All ten finalize identical two-block history/state |
| T2 | Duplicate every message; 2-8ms holds; periodic extra 40ms hold | Actual duplicates/reordered sends; no double-counting or conflicting decision |
| T3 | One proposer sends bad proposal signature and wrong-domain votes | Each of nine honest nodes logs both rejections; later-round progress |
| T4 | One proposer repeats nonce-1 TX within a body, then replays old candidate | Each honest node rejects invalid nonce and old height; nine deliberate replay sends; finalized nonces 1,2 applied once |
| T5 | Drop all outgoing protocol messages for 1200ms, then disable faults | No sends or decisions before loss window ends; all ten subsequently finalize |
| T6 | Separate silent and actual killed-proposer cases | Nine surviving honest nodes time out and finalize in later rounds |
| T7 | Three Byzantine processes genuinely sign/send both candidates in both voting phases | Authentic equivocation detected; all seven honest nodes finalize the same one-block history |
| T8 | Two independent runs with same fixtures, ordered workload, seed/policy | Same exact signed TX order and final state; actual drop/duplicate/reorder/replay sends in each run |

T5 covers complete pre-stable loss; configured nonzero delay is covered by T2/T8.
T6 kills the proposer before consensus begins and does not demonstrate restart
safety. T7's two honest groups initially see different candidates; no oracle
forces agreement. All cases are finite, bounded laboratory scenarios.

Unit tests additionally cover canonical bytes/reference vectors, tampering,
wrong chain/domain, ownership/nonces, Merkle shapes/reference values, block
validation, duplicate votes, locking/NIL, late/future rounds, transport
authentication, invalid header/body delivery and temporary peer blocking.
The parent process-entry helper is intentionally skipped in the parent and
executed inside actual children; none of T1-T8 is skipped.

## 7. Reading the evidence

logs/evidence contains the latest full suite's raw Go JSON log, summary, vet
output, per-case verified summaries and every available node event/result file.
It excludes private key files and generated configs. A crashed or Byzantine
node is not represented as an honest successful node. Original absolute paths
and PIDs in logs are historical provenance; they are not portable run configs.

The separate repeatability run has comparison.json and both run summaries.
tests/STAGE6.md records the exact executed commands and outcomes. The packaging
script verifies archive contents against a SHA-256 manifest. A checksum detects
accidental changes; it is not a signature or a proof of consensus correctness.

The final full race-enabled suite actually passed all T1-T8 and 203 tests/subtests;
formatting and vet passed, and no races were reported. The separate Python T8
command also passed. Its two runs produced the root
9fb497b5fbc68ed4eccf7f153742e02b87c3febb42bb54957da8cd9742fc38f6
and identical signed transaction order. These observed outcomes apply to the
tested finite scenarios and fixed inputs, not all possible network schedules.

## 8. Real issues addressed and remaining limits

Earlier shared peer IO could let an unavailable peer stall others; per-peer IO
and a global actual-send gate now separate availability from rate control.
Queue accounting includes scheduled, queued and in-flight work. The integration
checker was strengthened to bind T4 replay evidence to actual transmitted
packet IDs and to require actual T8 fault transmissions. Build errors from a
log-field name collision and an unused import were corrected. These are recorded
development changes, not invented debugging stories or performance results.

Connections are loopback-only and unencrypted; signatures authenticate messages
but do not provide confidentiality. Transactions are preloaded signed workloads;
there is no production mempool/client API. No safe crash recovery, dynamic
membership, additional consensus protocol, production deployment or exhaustive
Byzantine schedule exploration is claimed. Network counts vary under real
scheduling. Student understanding and report approval require human review.

## 9. Viva preparation and references

Before submission, explain why signatures alone cannot authorize a state edit;
why a body must match its header; why nonce replay protection and vote duplicate
protection differ; why seven signatures are required; why a timeout does not
permit unlocking; and why two real reruns can have different logs yet equal
ordered effects. Run the README commands and locate a rejected message, a lock,
a finality certificate and its resulting root in the recorded evidence.

References: original Lab01.pdf (local assignment); approved project decisions
in AGENTS.md; exact layouts and stage evidence in tests/STAGE1.md through
STAGE6.md; Go standard-library crypto/ed25519 and crypto/sha256 implementations;
the checked-in src/proto/consensus.proto and pinned go.mod/go.sum dependencies.

# Stage 3: Lab01 voting, locking and finalization

Stage 4 now uses real gRPC node processes only; see REAL_NETWORK.md. Controlled
message harnesses below are protocol unit tests, not a virtual cluster backend.

## Scope and files

This is Lab01's two-phase, round-based locking protocol. It is not labelled
classical PBFT or claimed to be a full production Tendermint implementation.

New files:

- `src/consensus/messages.go`: proposal envelope, canonical signing bytes,
  signature verification, and safe copying of messages.
- `src/consensus/engine.go`: fixed validators, independent node state, round
  transitions, proposals, votes, quorum evidence, locks, decisions and ledger.
- `src/consensus/order.go`: stable ordering of evidence used in decisions.
- `engine_test.go`, `adversarial_test.go`, `round_test.go` in the same package
  directory: controlled message-delivery and negative tests.
- This document.

No existing source files, Protobuf schema/generated files, dependency versions,
entry point, or state commitment scheme were changed. The Merkle root from
Stage 2 remains the state commitment. There is no network or clock scheduler
in this stage. An external caller supplies messages and timeout callbacks.

## Configuration and ownership

`New` checks n = 3f + 1, unique nonzero validator identities, positive timeout,
valid chain ID and a matching local private key that belongs to the validator
set. Validator list and private-key material are copied. This set/order is
fixed for the engine lifetime. Ten validators give f=3 and quorum=7; small
four-validator fixtures use f=1 and quorum=3 for focused tests.

Each Engine owns its ledger, committed state, candidates, votes, signed-vote
history, locks and output queue. It has no shared mutable consensus state.
The state starts empty at height 1 with zero parent hash. One engine owns each
honest key; recreating the engine to forget signed votes mid-run is forbidden.
Persistence and crash/restart recovery are not implemented. A silent proposer
can be modelled by withholding its messages, without restarting it.

Call the engine sequentially; it is not designed for concurrent calls. Private
keys do not appear in messages, events or ledger. Tests use fixed public
TEST-ONLY seed fixtures, unrelated to any simulator random generator.

## Block lifecycle

1. Proposer index is ((height - 1) + round) mod n, computed using reduced
   operands to avoid overflow. The ordered validator list defines the schedule.
2. `MakeProposal` builds a candidate with Stage 2 execution/validation. If a
   quorum-backed candidate is known, it carries that candidate forward instead
   of replacing it with fresh transactions.
3. `ReceiveProposal` checks the expected height/round/current proposer, envelope
   signature, original header proposer, parent, body, transactions, state root
   and any attached PREVOTE certificate before admitting the proposal.
4. If still in the proposal step, the node signs one PREVOTE for that block,
   or NIL if locked on another block without newer justification.
5. A non-NIL PREVOTE quorum for an available, fully validated candidate records
   its valid round and locks it. A current-round quorum causes PRECOMMIT for
   that block while the node is in the prevote step. A NIL quorum instead
   causes NIL PRECOMMIT and never removes a lock.
6. A quorum of distinct authenticated PRECOMMITs for one block and one round,
   together with the validated block, finalizes it. Only now does the engine
   append to its ledger and adopt the candidate state. It advances height,
   retains the finalized parent hash, and resets per-height round/lock data.

The signature of a transaction is not sufficient for execution; execution and
the Merkle state commitment are checked exactly as in Stage 2. Likewise,
receiving a block or PREVOTE quorum does not by itself finalize a block.

## Why a signed proposal envelope was added

Changing the block's header proposer during reproposal would change its hash,
destroying the identity that existing votes and locks refer to. A proposal
therefore wraps the original signed block. Its current proposer and round can
change without changing the block. OriginRound identifies the original header
proposer via the same deterministic schedule. A reproposal requires a prior
PREVOTE certificate; an original fresh proposal uses OriginRound = Round.

The new domain is `PROPOSAL:<chain_id>`. Its canonical bytes are, in order:

1. Version byte 1 and uint32-length-prefixed UTF-8 domain/context.
2. uint64 height, current round and origin round.
3. 32-byte current proposer ID.
4. 32-byte block hash and 64-byte original header signature.
5. uint32 proof count, then for each proof vote its uint32-length-prefixed
   Stage 1 canonical VOTE payload and fixed 64-byte signature.

All integers are unsigned big-endian. The proposal signature itself is excluded.
The signed block hash binds the header, whose BodyHash binds ordered body
contents. Proof order is authenticated exactly as transmitted. Locally assembled
proofs follow the configured validator order. TX, HEADER and VOTE formats are
unchanged. Verification requires the configured chain ID, as in Stage 1.

The attached proof must contain at least 2f+1 distinct configured validators,
all signing PREVOTE for the same non-NIL block, chain, height and round. The
proof round must be at least OriginRound and strictly before the proposal's
current round. Authentic but insufficient or incorrectly scoped evidence fails.
If a Byzantine validator previously sent a receiver a conflicting vote, a
separately authenticated complete certificate is still usable evidence; it is
not counted as a second independent vote in the receiver's vote table.

## Lock rules and safety reasoning

- A locked validator can support the same block in another round.
- A different block requires a PREVOTE quorum at a round strictly greater than
  its locked round. Evidence may arrive as individual votes or in a validated
  proposal certificate. Equal/older evidence cannot replace a lock.
- NIL quorums and timeout callbacks never clear a non-NIL lock.
- Receiving later evidence does not permit signing a second PREVOTE or
  PRECOMMIT in an already used (height, round, phase) slot.
- Late quorums still update lock/evidence even after a node timed out, but
  cannot cause a second vote in that round.
- Only finalization resets a height's lock and signing history. Heights advance
  monotonically, so old-height messages cannot cause new old-height signatures.

With n=3f+1, two quorums of size 2f+1 intersect in at least f+1 validators.
At least one is honest. A finalized block has at least f+1 honest precommitters
locked on it. A first later conflicting PREVOTE quorum cannot be formed without
an honest locked validator changing before it has the later evidence required
to do so. This is the reason votes, locks, signature checks and certificates
must all be enforced together. Focused tests support this reasoning; they are
not an exhaustive proof over all possible message schedules.

## Timeouts, NIL and round catch-up

The steps are Propose, Prevote and Precommit. Each entry emits a Timer carrying
height, round, step and a delay in logical ticks. Delay is BaseTimeout*(round+1),
saturating at uint64 maximum; round/height exhaustion returns an error rather
than wrapping. A callback matching the current step does the following:

- Proposal timeout: enter Prevote and sign NIL PREVOTE.
- Prevote timeout: enter Precommit and sign NIL PRECOMMIT.
- Precommit timeout: advance round and enter Propose with locks intact.

A valid NIL PREVOTE quorum can enter the precommit step before its timer.
NIL precommit quorums do not finalize anything; the precommit timer advances
the round. Stale timer callbacks return a reason and have no effect.

Votes from earlier rounds of the current height remain available for late
certificates. Future-round votes are authenticated and buffered as evidence,
not counted with current-round votes. At least f+1 distinct validators voting
in the same future round cause catch-up to that round. A validator counts once
even if it sent both phases. Byzantine validators alone cannot trigger this
under the fault bound. A catch-up does not itself sign a vote or clear a lock.
Current-height non-NIL PRECOMMIT certificates remain usable for a decision even
if the local node is in another round.

Liveness still requires stabilization, sufficiently long round timeouts, an
eventual correct proposer, delivery/retransmission of the required proposals,
bodies and certificates, and the external caller actually scheduling timers
and invoking the current proposer's MakeProposal. Stage 4 will implement and
test those delivery/time conditions. Future evidence is kept until finalization;
this stage does not impose a separate memory quota on Byzantine message floods.

## Public API and outputs

| API | Responsibility |
|---|---|
| New | Validate fixed configuration and start height 1 |
| MakeProposal | Build or re-propose a valid candidate when locally selected |
| ReceiveProposal | Authenticate and fully validate a received proposal |
| ReceiveVote | Authenticate, scope, deduplicate and process a vote |
| Timeout | Process a matching logical timer without using real time |
| ReceiveFinalized | Independently validate a decision certificate for the next height |
| Drain | Transfer pending votes, proposals, timers, events and decisions to the owner |
| Status, State, Ledger | Inspect local progress and safe snapshots |
| Equivocations | Return retained pairs of conflicting authenticated votes |

Output is a queue of instructions, not delivery through shared state. Stage 4
must transport messages through real connections and implement header
acceptance before body transmission; the full-block APIs here do not implement
or claim that handshake. Events contain height/round, kind/reason, hash, step,
validator, and vote phase where relevant. No event timestamps or log files are
generated in Stage 3; the real runtime attaches actual timestamps and node identities.

ReceiveFinalized reexecutes the block against the local parent state and checks
every unique PRECOMMIT signature, scope and quorum. It does not trust a peer's
claim that a block finalized. Missing heights must be supplied sequentially;
future heights and attempts to overwrite an already finalized height fail.
Ledger outputs and stored proposal bodies use independent copies.

## Test evidence

Each test uses fixed TEST-ONLY identities and an initially empty independent
engine unless the table says it first establishes a lock. The delivery harness
copies outputs; it never assigns ledgers or forces an engine's decision.

| Test | Input/fault injection | Expected invariant | Observed |
|---|---|---|---|
| TestTenNodeAgreementAndHeightAdvance | Ten engines, two heights; ordered delivery and duplicate/reverse delivery | All independently finalize matching block hashes, Merkle state roots and explicit values/nonces; each emits at most one vote per signing slot | PASS |
| TestVoteCountingAndRejections | Duplicate/conflicting votes, mixed phase/round, bad chain/height/phase/signature/domain/validator | Only unique authenticated votes form quorums; PREVOTEs do not finalize; retain equivocation pairs; rejection reason emitted | PASS |
| TestTimeoutNilAndSilentProposer | No round-0 proposal, explicit timers, stale timer, correct round-1 proposer | NIL votes, increasing delay, no stale callback effect, eventual finalization | PASS |
| TestLockSurvivesTimeoutAndReproposal | Lock in round 0; lost precommit delivery; re-propose in rounds 1/2; competing fresh block | Preserve block hash, lock and relayable certificate; no unjustified switch | PASS |
| TestOnlyLaterQuorumChangesLock | Locked node sees newer conflicting PREVOTE quorum directly or in a later proposal; NIL quorum | Only later evidence permits switch; no second prevote; NIL does not unlock | PASS |
| TestInvalidProposalAndProofCannotUnlock | Bad envelope/signature/scope/proposer/body/state, absent/repeated/short/wrong-phase/hash/signature/round/unknown-member proof | Reject without votes or lock/step/ledger changes | PASS |
| TestLateVotesAndDecisionCertificates | Precommits before body, older-round precommits, lagging node, malformed or replayed certificates, mutate returned ledger | Require valid body and unique quorum; independently validate catch-up; no overwrite or memory alias | PASS |
| TestByzantineEquivocationAtFaultBound | Ten validators; three Byzantine validators sign conflicting proposals and both vote phases to two honest groups | Seven votes can finalize A; six cannot finalize B; all honest nodes subsequently verify A's decision | PASS |
| TestProposalSignatureScope | Mutate every signed envelope component, wrong configured chain/domain, reconstruct equivalent proposal | Reject tampering and cross-chain/domain reuse; equivalent canonical bytes match | PASS |
| TestInvalidConfiguration | Invalid n/f, membership, duplicate/zero IDs, key, chain, timeout | Reject before starting consensus | PASS |
| TestRoundCatchupNeedsDistinctHonestEvidence | One future sender in both phases, then second sender, then old proposal | Require f+1 distinct senders; sign only in current round; reject old proposal | PASS |
| TestLateQuorumCannotCauseSecondPrecommit | NIL precommit timeout followed by non-NIL PREVOTE quorum | Retain late lock but do not sign a second precommit | PASS |
| TestNilQuorumDoesNotFinalize | NIL PREVOTE/PRECOMMIT quorums and timeout | No lock/finalization; timeout advances round | PASS |

Local certificate-validation tests synthesize signed evidence to test the
receiving node's rules. The fault-bound scenario instead obtains all honest
votes from honest engine outputs; only the three Byzantine keys are controlled
by fault injection. Neither is presented as the completed network T7 scenario.

Commands actually executed using the installed Go toolchain and
GOPATH=D:/go-workspace:

- gofmt on src/consensus: completed; final gofmt -l returned no paths.
- go test ./... -count=1 -cover: exit 0, all tests executed without cache.
  Statement coverage: consensus 90.4%, block 90.6%, state 97.6%, crypto 86.7%.
  Root/generated proto packages have 0% coverage; types has no test files.
- go vet ./...: exit 0, no diagnostics.

One intermediate run failed because the Byzantine test harness treated a vote
arriving after finalization as an unexpected error. The engine correctly rejected
the stale height. The harness was corrected to assert that rejection while
retaining the quorum and no-conflicting-finalization assertions. Later focused
and full runs passed. No race detector was run: this stage adds no goroutines
or concurrent engine calls.

These are Stage 3 unit/component results. T1-T7/adapted-T8 through the real
network, byte-identical log checks, outbound limits and header/body handshake
remain for Stages 4-6. Do not treat this coverage percentage as proof that every
possible Byzantine schedule has been tested.

## Viva walkthrough

A proposal carries an authenticated block. Validation computes its candidate
state. A PREVOTE says the validator can support it under its lock rules. A
PREVOTE quorum creates a lock and justifies PRECOMMIT. A PRECOMMIT quorum makes
the validated block final and commits its state. Timeouts rotate proposers but
never erase a lock. Certificates let later proposers carry the same block and
evidence forward, and let lagging nodes validate a decision for themselves.

Stop after Stage 3 for student review before implementing the Stage 4 network.

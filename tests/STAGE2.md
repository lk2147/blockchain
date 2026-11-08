# Stage 2: deterministic execution and candidate blocks

## Scope

Implemented owned key-value execution, per-sender nonce checks, canonical state
commitments, candidate block construction, and header/full-block validation.
Consensus, ledger finalization, networking, and T1-T8 integration remain for
later stages. A successful validation does not finalize or commit a block.

## Code and responsibilities

- `src/types/types.go`: adds `Block` (header plus body); corrects comments about
  nonce starting at 1 and the body commitment being SHA-256, not a Merkle root.
- `src/state/state.go`: private records and nonce maps, copy-safe reads,
  independent candidate snapshots, and atomic ordered batch execution.
- `src/state/commitment.go`: canonical typed leaves and a deterministic SHA-256
  Merkle commitment, adopted following student review.
- `src/block/block.go`: trusted local validation context, header checks,
  candidate construction and full validation.
- The corresponding `_test.go` files verify behavior and rejection invariants.

Existing Protobuf files, dependencies, main entry point and Stage 1 encoding
are unchanged. No existing files were removed.

## Execution rules

1. Verify the TX signature and configured chain ID.
2. Require a target starting with the exact lowercase hexadecimal sender public
   key followed by `/`, with a nonempty remaining record name. Keys are literal
   UTF-8 strings, not filesystem paths; no path or Unicode normalization occurs.
3. Require nonce = that sender's last successful nonce + 1, starting at 1.
   A sender at uint64 maximum cannot execute another transaction; no wraparound.
4. Replace the record value and store the executed nonce in the candidate state.

Transactions run in the exact order supplied by the block body. For two valid
writes to the same key, the later transaction wins. Independent senders have
independent nonce sequences. Empty byte values are permitted and do not delete
records. Any invalid transaction rejects the entire batch, including all prior
candidate writes and nonce increments. Error messages identify the zero-based
transaction index and reason. There is no silent filtering or reordering.

Example: Alice's nonce 1 sets her message to `hello`, and nonce 2 sets it to
`updated`. The resulting value is `updated` and her last nonce is 2. Replaying
nonce 1 fails even if its signature is authentic.

The zero value of State is empty. All execution writes use freshly allocated
maps and copied values. Get returns a copy. Consequently nodes and competing
candidate states cannot change each other's records through shared maps/slices.
Immutable snapshots can be read concurrently, but callers must not concurrently
mutate transaction inputs or block slices during a call.

## State commitment specification

The state now uses a deterministic binary Merkle tree. No membership or
non-membership proofs or interchangeable commitment framework are implemented.
Stage 1 TX/HEADER/VOTE/BODY encodings remain unchanged. The transaction body
commitment is still a direct hash; this change concerns the state commitment.

Canonical leaf payloads, in order:

1. Records sorted by raw UTF-8 key bytes, each encoded as
   `0x00 || uint32 key length || key || uint32 value length || value`.
2. Nonce entries sorted by raw public-key bytes, each encoded as
   `0x01 || 32-byte sender || uint64 last nonce`.

All integers are unsigned big-endian; lengths count bytes. Type tags separate
record keys from replay metadata. CanonicalLeaves returns copies of these
payloads. It replaces the earlier flat CanonicalBytes API, whose only caller
was a state unit test. State.Root(chainID) retains its original signature, so
execution and block construction/validation require no changes.

Tree hashes use SHA-256 with separate prefixes:

- Leaf: `SHA256(0x00 || canonical leaf payload)`.
- Internal node: `SHA256(0x01 || left hash || right hash)`.
- Empty tree: `SHA256(0x02)`.
- Single leaf: its leaf hash, without an extra tree level.
- At any odd-sized level, duplicate its last hash before pairing; preserve
  left/right order and repeat until one tree hash remains.

The final state root is:

```text
SHA256(0x03 || 0x01 || uint32 context length || STATE-MERKLE:<chain_id>
       || uint64 leaf count || tree hash)
```

Here 0x03 separates the root wrapper from other hashes, and 0x01 is the format
version. The chain ID must be nonempty valid UTF-8. The leaf count prevents
duplicate-last padding from hiding different tree sizes. For chain `c`, the
empty state root is:

```text
6669b6e772347d12f2402f32126038c7eec78f168db48ec1aa97910b0ce5aaff
```

A missing nonce entry means zero; normal execution stores only successful
nonces. Nil/empty values encode identically. Including nonce history prevents
identical visible records from hiding different future replay behavior. Sorting
removes Go map iteration order from the hash. Field/context lengths exceeding
uint32 capacity are rejected, but huge allocations are not exercised in tests.

This deliberately changes state roots and therefore signed block headers and
block hashes compared with the earlier flat commitment. All nodes in a run
must use this scheme. No history migration is implemented or needed for the
current project, which has no persistent finalized ledger yet.

## Block lifecycle in this stage

The receiver supplies Context from its own chain knowledge: expected chain,
height, parent hash, and proposer. It must not populate that context from the
untrusted incoming header. Height 1 uses the empty initial state and zero parent
hash; later contexts require a nonzero finalized parent hash. The caller is
responsible for supplying the actual finalized parent's state and next height.
There is no ledger or proposer-selection algorithm in Stage 2.

Build copies transaction values, executes the batch, computes body and state
hashes, and signs the resulting candidate header. It returns a block, without
mutating the caller's state or announcing/finalizing anything.

ValidateHeader checks the trusted context and header signature. Full Validate
then compares the body commitment, executes all transactions, and compares the
computed state root. It returns a candidate snapshot on success or the original
snapshot on failure. Later consensus must decide when that candidate becomes
the node's committed state. A valid header may still have an unavailable or
invalid body; it does not authorize a vote or finalization by itself.

## Executed test evidence

All tests below passed in the final Stage 2 run.

| Test | Initial conditions and input/fault | Expected outcome and invariant | Observed |
|---|---|---|---|
| TestOrderedExecutionAndIsolation | Empty state; Alice and Bob updates, repeated Alice write; mutate caller/read buffers and build another branch | Explicit expected values/nonces; inputs, prior snapshots and branches stay independent; empty values remain present | PASS |
| TestRejectedBatchesAreAtomic | Existing nonce 1; valid write followed by invalid signature, owner, empty/uppercase namespace, foreign chain, replay, duplicate, zero/gap/reversed nonce | Reject with original root/nonce intact; no partial updates | PASS |
| TestCommitmentCanonicalAndDeterministic | Equivalent states with opposite insertion order, repeated root calls, same-value write with higher nonce, foreign chain, manually specified record/nonce leaves, returned-leaf mutation | Equal states yield equal roots; nonce/chain differences affect root; exact sorted leaves match expectation; no exposed state memory | PASS |
| TestMerkleTreeShapes | Empty, one, two, three, four and five explicit leaves; reversed children; internal preimage supplied as leaf | Matches independent small-tree formulas; odd padding repeats at each level; order and hash domains remain distinct | PASS |
| TestMerkleRootGoldenAndMutations | Empty/single/odd trees with independently calculated fixed roots; modify/add/remove records or nonce metadata; invalid UTF-8; equal padded inner trees with different counts | Golden roots match; changes affect roots; invalid keys rejected; root binds actual leaf count | PASS |
| TestInvalidContextAndNonceExhaustion | Invalid chain contexts, sender already at maximum nonce | Reject invalid context and prevent unsigned overflow; preserve state | PASS |
| TestBuildValidateAndParentLink | First block independently validated twice, repeated build, caller slice mutation, linked empty second block, old block/transaction replay | Same signed header and post-state; real parent hash link; no implicit commit; reject replay | PASS |
| TestRejectInvalidBlocks | Wrong signed chain/height/parent/commitments, proposer, signature, missing/changed body; authentic unexpected proposer; rehashed and re-signed bodies containing invalid transactions | Reject independently of authenticity where applicable; header acceptance cannot bypass execution; preserve input state | PASS |
| TestRejectInvalidBuildContext | Invalid chain, height, genesis/later parent combinations, malformed signing key and invalid batch | No invalid candidate produced | PASS |

Stage 1 tests also pass. These are unit/component tests, not T1-T8 end-to-end
results. No claim of consensus safety or network determinism is made.

Final commands were executed using the existing installation at
`D:/tools/golang/bin` with `GOPATH=D:/go-workspace`:

- gofmt on all added/modified Go files: completed; gofmt -l returned no paths.
- go test ./...: exit 0 after Merkle adoption; block and state tests executed
  successfully, crypto passed from cache. Root, proto and types have no tests.
- go vet ./...: exit 0 with no diagnostics.

No test failures occurred during Stage 2. No race detector was run because this
stage introduces no concurrent execution. No tools or dependencies were installed.
Merkle fixed reference roots were calculated separately with Python hashlib
from literal encoded leaf payloads and explicit small-tree formulas.

## Viva points and remaining work

- A signed transaction can still be unauthorized or replayed: execution must
  check ownership and the sender's current nonce.
- Transaction order defines execution; map sorting defines commitment encoding.
- The parent hash links history, the body hash binds inputs, and the state root
  binds execution output including replay metadata.
- Validation computes a possible next state. Consensus must select and finalize
  a block before that state is committed.
- Stage 3 still needs rounds, voting, locks, timeouts, quorum validation and
  finalization. Stage 4 supplies real communication, message faults and header/body flow.

Stop here for student review and approval before Stage 3.

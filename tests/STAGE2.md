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
- `src/state/commitment.go`: canonical sorted state encoding and SHA-256 root.
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

This is a direct SHA-256 commitment over canonical bytes, not a Merkle tree.
No membership proofs or interchangeable commitment framework are implemented.
Lab01 permits this choice; the Stage 1 message encodings remain unchanged.

Bytes in order:

1. Version byte `01`.
2. uint32-length-prefixed UTF-8 context `STATE:<chain_id>`.
3. uint32 record count.
4. Records sorted by raw UTF-8 key bytes: length-prefixed key, then
   length-prefixed value for each record.
5. uint32 sender-nonce count.
6. Senders sorted by their raw public-key bytes: 32-byte key followed by uint64
   last-executed nonce.

All integers are unsigned big-endian; lengths count bytes. A missing nonce
entry means zero; normal execution stores only successfully executed nonces.
Nil/empty values encode identically. Root is SHA-256 of the complete encoding.
Including nonce history prevents identical visible records from hiding different
future replay behavior. Sorting removes Go map iteration order from the hash.

For chain `c`, the empty state encoding is:

```text
010000000753544154453a630000000000000000
```

Lengths/counts above uint32 are rejected but those impractically large memory
boundaries are not exercised by unit tests.

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
| TestCommitmentCanonicalAndDeterministic | Equivalent states with opposite insertion order, repeated root calls, same-value write with higher nonce, foreign chain, manually specified record/nonce bytes and empty bytes | Equal states yield equal roots; nonce/chain differences affect root; exact sorted bytes match independent expectation | PASS |
| TestInvalidContextAndNonceExhaustion | Invalid chain contexts, sender already at maximum nonce | Reject invalid context and prevent unsigned overflow; preserve state | PASS |
| TestBuildValidateAndParentLink | First block independently validated twice, repeated build, caller slice mutation, linked empty second block, old block/transaction replay | Same signed header and post-state; real parent hash link; no implicit commit; reject replay | PASS |
| TestRejectInvalidBlocks | Wrong signed chain/height/parent/commitments, proposer, signature, missing/changed body; authentic unexpected proposer; rehashed and re-signed bodies containing invalid transactions | Reject independently of authenticity where applicable; header acceptance cannot bypass execution; preserve input state | PASS |
| TestRejectInvalidBuildContext | Invalid chain, height, genesis/later parent combinations, malformed signing key and invalid batch | No invalid candidate produced | PASS |

Stage 1 tests also pass. These are unit/component tests, not T1-T8 end-to-end
results. No claim of consensus safety or network determinism is made.

Final commands were executed using the existing installation at
`D:/tools/golang/bin` with `GOPATH=D:/go-workspace`:

- gofmt on all added/modified Go files: completed; gofmt -l returned no paths.
- go test ./...: exit 0; block, state and crypto tests passed (crypto/state
  cached in the final run; state ran successfully without cache in the first
  Stage 2 run). Root, proto and types have no test files.
- go vet ./...: exit 0 with no diagnostics.

No test failures occurred during Stage 2. No race detector was run because this
stage introduces no concurrent execution. No tools or dependencies were installed.

## Viva points and remaining work

- A signed transaction can still be unauthorized or replayed: execution must
  check ownership and the sender's current nonce.
- Transaction order defines execution; map sorting defines commitment encoding.
- The parent hash links history, the body hash binds inputs, and the state root
  binds execution output including replay metadata.
- Validation computes a possible next state. Consensus must select and finalize
  a block before that state is committed.
- Stage 3 still needs rounds, voting, locks, timeouts, quorum validation and
  finalization. Stage 4 still needs simulated communication and header/body flow.

Stop here for student review and approval before Stage 3.

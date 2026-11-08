# Stage 1: authenticated data, not execution

## Scope and code paths

`src/types` contains ordinary Go message types, independent of Protobuf.
`src/crypto` encodes, signs, verifies, and hashes them. No state execution,
block production, vote counting, network, or simulator is implemented here.
The existing Protobuf schema/generated files and main entry point are unchanged.

Signing follows: message -> canonical payload -> Ed25519 -> returned message
copy with signature. Verification reconstructs that payload, requires an exact
match with the configured expected chain ID, and checks the embedded signer's
public key. This authenticates the signer, not their authorization as a validator
or expected proposer; those checks belong to subsequent stages.

## Version 1 canonical format

Each payload starts with byte `01` then a uint32-length-prefixed UTF-8 context.
All unsigned integers are big-endian. Variable strings/bytes use uint32 byte
lengths. Hashes/public keys are fixed 32 bytes; signatures are fixed 64 bytes.
Strings must be valid UTF-8; no Unicode normalization occurs. Chain IDs must
be nonempty. Nil and empty byte values have the same representation.

Payload fields following the context, in order:

| Payload | Context | Fields |
|---|---|---|
| TX | `TX:<chain_id>` | sender, target key, value, nonce (uint64) |
| HEADER | `HEADER:<chain_id>` | height (uint64), parent hash, body hash, state root, proposer |
| VOTE | `VOTE:<chain_id>` | height (uint64), round (uint64), phase (byte), validator, NIL marker (byte), block hash |
| BODY | `BODY:<chain_id>` | transaction count (uint32), followed by each length-prefixed TX payload and fixed-size signature |

PREVOTE is 1 and PRECOMMIT is 2. NIL is marker 1 with an all-zero hash;
non-NIL is marker 0 with a nonzero hash. Other combinations are rejected.
Body transactions must have the body's chain ID. Encoders reject lengths
above uint32 capacity; these huge allocation boundaries are not unit-tested.

Ed25519 signs payloads directly, excluding the message's own signature.
TransactionID is SHA-256(TX payload || signature), incorporating the user's
approved clarification. BlockHash is SHA-256(HEADER payload), excluding the
header signature. BodyHash is SHA-256(BODY payload), including transaction
signatures and order. IDs and body hashing do not independently verify signatures.

The header signature authenticates the body hash. A later receiver must hash
the received body and compare it with that commitment before accepting the
association. Signature verification alone does not validate transactions or
the resulting state root.

Ownership is planned as `<lowercase sender public-key hex>/<nonempty record>`.
Nonces will start at 1 and must progress per sender. Stage 1 authenticates the
key, value and nonce but does not enforce ownership, reject nonce zero, maintain
nonce state, or prevent replay. Those are Stage 2 execution responsibilities.

`GenerateIdentity` uses crypto/rand. Tests use the public RFC 8032 section 7.1
test-1 seed only; it is explicitly test-only and unsuitable for real identities.
No simulator randomness is used. Signing rejects malformed private keys and
keys inconsistent with the message identity rather than allowing a panic.

## Test evidence

Run from the repository root with the existing Go installation:

```powershell
go test ./...
go vet ./...
```

The authoring session used `D:/tools/golang/bin/go.exe` and set
`GOPATH=D:/go-workspace` because its default tool path/cache differed from the
student's setup. No dependencies or tools were reinstalled.

| Test | Initial conditions and input/fault | Expected invariant | Observed |
|---|---|---|---|
| TestReferenceVectors | Fixed RFC 8032 seed, empty signed message, SHA-256 input `abc` | Public key/signature and hash match independent published known answers | PASS |
| TestAuthentication | Fixed key; signed TX/header/vote; each field changed independently; correctly signed message against wrong configured chain; all cross-domain pairs; NIL votes in both phases | Valid signatures pass; changed fields, wrong contexts and chains fail; canonical NIL signatures pass | PASS |
| TestCanonicalGoldenBytes | Small explicit messages and manually specified hexadecimal TX/header/vote/body/signed-TX bytes | Exact order, width, lengths, version and byte order match expected bytes | PASS |
| TestEncodingAndHashes | Two signed transactions; modify content/signature, insert/delete/reorder; reconstruct equivalent value; move field boundaries | Body binds exact ordered signed contents; TX ID includes signature; header ID excludes signature; equivalent values encode identically; boundaries remain unambiguous | PASS |
| TestMalformedInputs | Empty/invalid UTF-8 chains, invalid UTF-8 target, invalid phase/NIL form, wrong body chain, short/inconsistent/mismatched private keys, unsigned TX | Reject malformed input with errors, without panic | PASS |

Actual checks after correction: gofmt completed; `go test ./...` exited 0
(crypto tests passed; root, types and proto have no test files); `go vet ./...`
exited 0. The initial test run failed because the test reference signature had
an extra byte and the hand-counted body payload length was wrong. These test
data errors were corrected; production encoding/signing behavior was not changed
to satisfy them. No race check was needed: this stage introduces no concurrency.

These are Stage 1 unit results, not T1-T8 end-to-end results. No logs,
state-transition correctness, replay rejection or consensus safety are claimed.

## Viva points

- Canonical encoding makes different nodes sign/hash identical bytes.
- Contexts prevent a signature for one message type being reused for another.
- The configured chain check prevents accepting an authentic foreign-chain message.
- A body hash binds contents and order; a state root binds the execution result.
- A nonce is signed here; enforcing its progression later prevents repeated execution.
- Knowing a public key and verifying its signature does not grant validator membership.

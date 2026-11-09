# Real networking: GossipSub and bidirectional gRPC

Each validator is a separate Go process with its own key, consensus engine,
ledger and application state. There is no virtual execution backend or central
finalization authority. Protocol validation and the Merkle commitment remain
unchanged by this transport revision.

## Message paths

| Traffic | Path |
|---|---|
| Normal proposal/finalized headers and votes | One signed broadcast publication through libp2p GossipSub |
| Accepted-header requests and transaction bodies | Persistent gRPC Exchange stream |
| Directed protocol messages, including test-only equivocation | Same authenticated gRPC stream |

GossipSub selects and repairs a forwarding mesh. A publication can reach a
validator through another peer; the application no longer expands every normal
broadcast into nine unary RPC calls. PubSub authenticates its publisher, and
the application checks the original validator identity and canonical transport
signature. Relays do not become the signers of forwarded votes.

The gRPC schema is now:

```protobuf
rpc Exchange(stream Envelope) returns (stream Envelope);
```

The lower node ID normally initiates one session for each pair. Both ends can
write protocol frames on that same RPC. A receive loop and a single send worker
per peer run independently. A broken session is removed and re-established;
cached protocol retries recover messages rather than assuming a stream write
proves receipt. One unavailable peer does not block every other peer's IO.

Initial decisions are announced immediately. Cached finalized headers are
re-advertised every fourth RetryMS tick, while current proposals/votes retain
their normal retry cadence. This prevents large old certificates from crowding
out the next height under gossip replication and injected duplication. Matching
body requests are answered immediately; the slower advertisement does not remove
catch-up data or relax validation.

Each consensus engine remains on one owner loop. Network readers submit frames
to that loop; they do not mutate votes, locks or application state themselves.
Shutdown cancels readers, stops servers and joins workers before closing logs.

## Configuration and identities

Generate fresh configurations from the repository root:

```text
python tests/run_local_cluster.py --run-dir local-runs/hybrid --prepare-only
python tests/run_local_cluster.py --config-dir local-runs/hybrid
```

Add --go D:/tools/golang/bin/go.exe when needed. Use --faults during preparation
for the example fault policy. README explains separate terminals with --node
and read-only inspection with --check-only.

Every Peers entry has Address (gRPC host:port), GossipAddress (libp2p host:port)
and PublicKey (validator Ed25519 key). For ten nodes, the default initializer
allocates gRPC ports 29000-29009 and gossip ports 29010-29019. All endpoints must
be distinct loopback addresses. Old configurations lacking GossipAddress fail
with an explicit instruction to regenerate. Existing experiment output cannot
be resumed safely; use a fresh directory.

The ordered validator list is fixed during a run. Libp2p peer IDs are derived
from those same validator public keys; its connection gate rejects identities
outside the configured set. TCP, Noise and Yamux are explicitly selected for
libp2p. Its bootstrap addresses come from configuration, not open membership
discovery. The Python launcher compares shared ChainID, F and ordered Peers
before launch. Real runs require n>=8 and n=3f+1; ten gives f=3 and quorum seven.

The membership fingerprint is SHA-256 of JSON encoding this fixed Go struct,
in its declared field order: Version="LAB01-NET-2", Chain, F, Peers. Peers retain
their configured order and fields Address, PublicKey, GossipAddress. Node ID,
private key, output directory and node-specific fault controls are excluded.
Every node logs its fingerprint and libp2p peer ID. The gossip topic is
/lab01/2/<fingerprint hex>. Streams begin with mutually signed hello envelopes
whose payload is the fingerprint; mismatches are refused. Chain ID and every
later frame's sender/recipient/signature are also checked.

gRPC remains unencrypted loopback transport with signed messages. Noise protects
libp2p connections. This implementation does not claim public-internet deployment
or safe validator crash recovery. Stream reconnection preserves the live engine;
restarting a process with forgotten signing history remains unsupported.

## Header acceptance and bodies

A header advertisement excludes its transaction body. The receiver verifies its
own expected chain, height, proposer, parent, signatures and certificate before
sending a signed body_request for the exact advertisement reference. A normal
sender refuses a body without that request. The receiver requires its own
accepted-header record, matches the body hash, executes transactions and checks
the state root. Only consensus finalization adopts the candidate state.

The advertisement reference is SHA-256 of the exact advertised JSON envelope.
Protocol signatures/hashes still use canonical TX/HEADER/VOTE/PROPOSAL encodings.
Protobuf transports bytes; arbitrary protobuf wire encodings are never signed.

Canonical TRANSPORT version 1 retains these big-endian fields:

```text
version byte; uint32 length + TRANSPORT:<chain>
uint32 sender; uint32 recipient; uint64 sequence
uint32 kind length + kind; fixed 32-byte reference
uint64 height; uint64 round; uint32 payload length + payload
```

The signature is excluded from its own signing input. Recipient 0xffffffff is
the broadcast marker and is allowed only for headers/votes. Body requests,
bodies and hello handshakes are directed. Hello has sequence/height/round zero,
a zero reference and the membership fingerprint payload. Application frames
cannot change the authenticated stream sender or substitute another recipient.

## Fault controls and their scope

| Setting | Action on authored protocol traffic |
|---|---|
| MinDelayMS / MaxDelayMS | Hold before local publication or stream write |
| DropBPS | Omit that publication/write; 10000 means 100% |
| DuplicateBPS | Schedule an additional unchanged signed frame |
| ReorderEvery / ReorderHoldMS | Add a hold periodically so later frames can pass |
| ReplayEvery / ReplayAgeMS | Republish/resend an eligible original signed vote |
| DisableAfterMS | End artificial injections after elapsed time; zero keeps them enabled |

For node 0's outgoing 500ms delay, edit only its Faults.MinDelayMS and MaxDelayMS
to 500, with DisableAfterMS=0, before launching. Its protocol behavior stays
honest. Incoming traffic and other nodes' authored traffic are not thereby held.

The layer runs before local GossipSub publication and directed gRPC writes.
It does not impair kernel TCP packets, connection handshakes, libp2p heartbeats
or automatic relay forwarding. SendGapMS limits authored protocol publications
and directed writes, not every packet emitted by libp2p. QueueLimit bounds the
application's scheduled/queued/in-flight frames; library queues have their own
bounds. This scope is deliberate and must not be described as full link-level
impairment or a bandwidth bound on GossipSub replication.

GossipSub suppresses relay loops using its own publication IDs. An explicit
duplicate injection is a fresh publication of the original signed envelope,
so the application can still see and reject the repeated vote. Sequence numbers
identify authenticated frames, not an early application duplicate filter.
Replay history is bounded to 256 frames. Eligible replay opportunities must
actually result in transmissions before a test claims to have exercised replay.

The receiver counts authenticated original senders per ReceiveWindowMS and
temporarily blocks an overactive sender for BlockMS at the protocol boundary.
This does not ban its libp2p forwarding connection or change validator membership.

Optional StartAtUnixMS coordinates a real-clock experiment start. It does not
choose a consensus outcome. The controlled suite allows four seconds to start
secure hosts/subscriptions and eight seconds for protocol execution. Configured
holds are minimum delays; OS scheduling can add latency. No hard delivery bound
or identical real log order is promised.

## Checks and evidence

Run the single entry point from the repository root:

```text
python tests/run_tests.py
python tests/run_tests.py --race
```

The suite preserves full T1-T8 checks: independent honest histories, execution,
state roots, distinct authenticated quorum members, vote signing slots, hold
deadlines, send gaps and accepted-header-before-body gates. T1/T2/T8 additionally
require every node to show real gossip and streaming send/receive events.

Focused tests in hybrid_test.go verify a real 0--1--2 gossip relay without a
0--2 connection, actual bidirectional stream traffic, reconnect and membership
handshake checks. The socket rejection test obtains its body_request over the
same RPC used to advertise the header. Test fixtures are explicitly test-only
keys. Fixed fault seeds never generate real identities.

New evidence directories are local-runs/transport-normal and transport-race.
Earlier logs/evidence and stage reports refer to the unary-gRPC snapshot, and
must not be cited as execution evidence for this implementation. TEST_STATUS.md
records actual final outcomes. Final report/ZIP work is deferred by the student.

## Code map and protocol references

- src/realnet/gossip.go: identity gate, topic validation, mesh bootstrap and relay.
- src/realnet/stream.go: signed handshakes, duplex sessions and reconnects.
- src/realnet/run.go: owner loop, fault scheduling, shared application send gate.
- src/realnet/auth.go: canonical envelope authentication and broadcast scope.
- src/node/runtime.go: protocol messages, accepted-header/body flow and retries.
- src/proto/consensus.proto: Exchange streaming service and regenerated bindings.
- tests/run_local_cluster.py: Python-only preparation, launch and inspection.

Library references: [official GossipSub implementation](https://github.com/libp2p/go-libp2p-pubsub/tree/v0.18.0)
and [official gRPC Go streaming tutorial](https://grpc.io/docs/languages/go/basics/).
Pinned versions are recorded in go.mod/go.sum. These references describe the
libraries; successful application behavior is established by executed tests.

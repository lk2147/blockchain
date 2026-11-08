# MASTER ENGINEERING SPECIFICATION
# Modular Layer 1 Blockchain and Distributed Systems Laboratory

You are the principal Go engineer responsible for completing this project.

Your task is to produce a fully functioning, modular Layer 1 blockchain experimental framework that satisfies the original Blockchain Lab01 assignment and supports advanced experiments involving consensus algorithms, authenticated state commitments, unreliable networks, Byzantine validators, node crashes, and recovery.

This is an engineering implementation task, not merely an architecture proposal.

You must inspect the existing project, understand its current state, implement the system, run the tests, fix defects, and produce the final submission artifacts.

Do not stop after generating a plan or skeleton.

---

# PART I — PROJECT OBJECTIVES

## 1. Primary objectives

The project has two objectives.

### Objective A: Satisfy the original Lab01 assignment

Implement a minimal Layer 1 blockchain with:

- Cryptographically authenticated transactions and consensus messages.
- Deterministic state execution.
- Pluggable state commitments.
- Byzantine fault-tolerant consensus.
- A simulated unreliable network.
- Partial synchrony.
- Reproducible experiments.
- Deterministic logs.
- Unit and end-to-end tests.

The original Lab01.pdf is the authoritative source for assignment requirements.

Preserve its required concepts, terminology, test scenarios T1–T8, and submission requirements.

### Objective B: Build an extensible experimental framework

The system must support independent experimentation with:

1. Different authenticated data structures.
2. Different BFT consensus algorithms.
3. Different network conditions.
4. Different Byzantine behaviors.
5. Node lifecycle failures and recovery.
6. Deterministic and real-network execution.
7. Comparative performance and correctness analysis.

The architecture must allow a new implementation of one component without requiring modifications to unrelated components.

## 2. Non-goals

Do not attempt to create a production-ready cryptocurrency.

The following are outside the current scope:

- Token economics.
- Gas and transaction fees.
- Mining.
- Smart contract virtual machines.
- Public peer discovery.
- Production validator membership changes.
- Mainnet deployment.
- Wallet interfaces.

Prioritize correctness, observability, modularity, and educational value.

---

# PART II — MANDATORY ENGINEERING RULES

## 3. Inspect before modifying

Before writing code:

1. Inspect the entire existing repository.
2. Read README files and configuration files.
3. Inspect Go modules and Protobuf definitions.
4. Identify existing implementations that work.
5. Identify incomplete modules and architectural coupling.
6. Identify existing tests and their actual coverage.
7. Identify generated files that must not be edited manually.

Preserve valid existing work.

Do not overwrite functioning code simply to match a preferred directory structure.

If the current architecture is fundamentally unsuitable, explain the reason and migrate it carefully.

Do not delete user data, configuration, keys, or experimental results without authorization.

## 4. No fake implementations

The following are prohibited:

- Empty interface implementations.
- Algorithms that only return a constant hash.
- Placeholder consensus engines presented as functional.
- Mock signatures used in real protocol tests.
- Tests that pass without exercising the relevant implementation.
- Artificially declaring a node synchronized.
- Artificially marking a block finalized.
- Silently ignoring invalid messages.
- Catching errors and reporting success.
- Using time.Sleep as the deterministic simulator's event scheduler.

Every implemented feature must have meaningful tests.

If an advanced cryptographic implementation cannot be completed correctly, explicitly report that limitation rather than inventing an insecure substitute.

## 5. Engineering standards

Use:

- Go 1.22 or newer.
- Strongly typed structures.
- Explicit error handling.
- Context cancellation.
- Race-safe concurrent code.
- Small, testable packages.
- Dependency injection.
- Structured logging.
- Deterministic serialization.
- Configuration validation.
- Clear documentation.

Run gofmt, go vet, and go test -race where applicable.

Avoid unnecessary dependencies.

Pin dependency versions and document their licenses.

---

# PART III — SYSTEM ARCHITECTURE

## 6. Required architectural separation

Organize the system around these independent responsibilities:

1. Core domain types.
2. Cryptography.
3. Transaction execution.
4. State commitment.
5. Consensus.
6. Networking.
7. Deterministic simulation.
8. Node lifecycle management.
9. Byzantine behavior.
10. Storage and recovery.
11. Logging and observability.
12. Testing and scenario orchestration.
13. Command-line applications.

Use dependency inversion.

Core domain logic must not depend on gRPC, a particular consensus algorithm, or a particular state commitment implementation.

Consensus must not directly manipulate network connections.

The network must not decide whether a block is valid or finalized.

The commitment implementation must not execute transactions.

The simulator must not secretly modify consensus state.

## 7. Proposed project layout

Use the following as the target structure. Adjust package boundaries when justified, but preserve the separation of responsibilities.

    .
    ├── cmd/
    │   ├── node/
    │   │   └── main.go
    │   ├── simulator/
    │   │   └── main.go
    │   └── controller/
    │       └── main.go
    │
    ├── api/
    │   └── proto/
    │       ├── common.proto
    │       ├── transaction.proto
    │       ├── network.proto
    │       ├── tendermint.proto
    │       ├── pbft.proto
    │       └── admin.proto
    │
    ├── internal/
    │   ├── core/
    │   │   ├── block.go
    │   │   ├── transaction.go
    │   │   ├── validator.go
    │   │   ├── ledger.go
    │   │   ├── types.go
    │   │   └── errors.go
    │   │
    │   ├── crypto/
    │   │   ├── signer.go
    │   │   ├── verifier.go
    │   │   ├── hash.go
    │   │   └── domain.go
    │   │
    │   ├── encoding/
    │   │   ├── canonical.go
    │   │   └── codec.go
    │   │
    │   ├── execution/
    │   │   ├── executor.go
    │   │   ├── state.go
    │   │   └── validation.go
    │   │
    │   ├── commitment/
    │   │   ├── interface.go
    │   │   ├── registry.go
    │   │   ├── merkle/
    │   │   ├── mpt/
    │   │   └── verkle/
    │   │
    │   ├── consensus/
    │   │   ├── interface.go
    │   │   ├── engine.go
    │   │   ├── registry.go
    │   │   ├── common/
    │   │   ├── tendermint/
    │   │   └── pbft/
    │   │
    │   ├── network/
    │   │   ├── interface.go
    │   │   ├── router.go
    │   │   ├── grpc/
    │   │   └── simulation/
    │   │
    │   ├── simulation/
    │   │   ├── clock.go
    │   │   ├── scheduler.go
    │   │   ├── event.go
    │   │   ├── topology.go
    │   │   └── scenario.go
    │   │
    │   ├── controller/
    │   │   ├── node_controller.go
    │   │   ├── lifecycle.go
    │   │   └── admin_server.go
    │   │
    │   ├── adversary/
    │   │   ├── interface.go
    │   │   ├── silent.go
    │   │   ├── equivocation.go
    │   │   ├── replay.go
    │   │   └── invalid_message.go
    │   │
    │   ├── storage/
    │   │   ├── interface.go
    │   │   ├── memory.go
    │   │   └── file.go
    │   │
    │   ├── node/
    │   │   ├── node.go
    │   │   ├── runtime.go
    │   │   └── recovery.go
    │   │
    │   └── observability/
    │       ├── logger.go
    │       ├── metrics.go
    │       ├── events.go
    │       └── invariants.go
    │
    ├── config/
    │   ├── cluster_config.json
    │   ├── simulator_config.json
    │   └── scenarios/
    │
    ├── tests/
    │   ├── unit/
    │   ├── integration/
    │   ├── e2e/
    │   ├── scenarios/
    │   └── scripts/
    │
    ├── logs/
    ├── data/
    ├── scripts/
    ├── docs/
    ├── Makefile
    ├── go.mod
    ├── README.md
    └── REPORT.pdf

Keep generated Protobuf Go files in their appropriate generated package.

Document the package dependencies and avoid circular dependencies.

---

# PART IV — CORE DATA AND CRYPTOGRAPHY

## 8. Core blockchain types

Implement:

- ChainID.
- NodeID.
- ValidatorID.
- Height.
- Round.
- Block.
- BlockHeader.
- BlockBody.
- Transaction.
- Vote.
- ValidatorSet.
- FinalizedBlock.
- State.
- StateRoot.
- Hash.
- Proof.

Use distinct types where they improve safety and prevent accidentally mixing identifiers, heights, rounds, hashes, or phases.

## 9. Cryptography

Use:

- Ed25519 signatures.
- SHA-256 hashing.
- Explicit domain separation.
- Canonical byte encoding.

All keys and validator identities must be loaded from configuration or generated reproducibly for tests.

Never store private keys in public logs.

Never use simulator PRNG output as cryptographic randomness.

### 9.1 Domain separation

Use distinct signing domains, including:

- TX:<chain_id>
- HEADER:<chain_id>
- VOTE:<chain_id>

Additional protocol-specific message types must have their own explicit domains.

A signature created for one message type must never validate for another.

### 9.2 Canonical encoding

Never sign or hash arbitrary Protobuf wire bytes.

Implement canonical encoding with:

- Fixed field order.
- Explicit integer byte order.
- Length-prefixed strings and byte slices.
- Explicit optional-field representation.
- Unambiguous encoding of nested structures.
- Versioned encoding where necessary.

Define exactly which fields are included in every signature and hash.

Block hashing must not include the block's own signature in a circular manner.

Define separate functions for:

- CanonicalTxBytes.
- CanonicalHeaderBytes.
- CanonicalVoteBytes.
- CanonicalConsensusMessageBytes.

Add golden-vector tests.

Identical logical objects must always produce identical canonical bytes.

### 9.3 Replay protection

Transactions must include a sender nonce.

Validate nonce progression deterministically.

A transaction must not be applied more than once.

Votes must be scoped by chain ID, height, round, phase, and validator identity.

Old messages must not be accepted as valid messages for a new chain or height.

---

# PART V — EXECUTION AND STATE COMMITMENT

## 10. Deterministic state execution

Represent the application state as a deterministic key-value state machine.

A transaction:

- Identifies its sender.
- Identifies the key it wants to modify.
- Contains the requested value.
- Contains a nonce.
- Contains a signature.

A sender may only modify keys owned by that sender.

For example:

Alice may update Alice/message but not Bob/message.

Transactions must be validated before execution.

Given the same initial state and the same ordered valid transactions, every correct node must compute exactly the same resulting state.

Define transaction ordering explicitly.

Define behavior for:

- Invalid signatures.
- Unauthorized keys.
- Duplicate transactions.
- Invalid nonces.
- Conflicting writes.
- Invalid transaction batches.

The execution engine must be independent of the state commitment implementation.

## 11. State commitment abstraction

Design a real interchangeable state commitment interface.

At minimum:

    type StateCommitment interface {
        Name() string
        ComputeRoot(state State) (Hash, error)
        Prove(state State, key []byte) (Proof, error)
        VerifyProof(
            root Hash,
            key []byte,
            proof Proof,
        ) (bool, error)
    }

The actual interface may include additional methods if required by the underlying commitment implementation.

The abstraction must support membership and non-membership proofs where the underlying algorithm supports them.

Define proof encoding and verification semantics.

Do not expose algorithm-specific internals to consensus.

## 12. Required commitment implementations

### 12.1 Merkle Tree

Implement the default commitment.

Requirements:

- Deterministic key ordering.
- Canonical leaf encoding.
- Domain-separated leaf and internal-node hashes.
- Defined empty-state root.
- Defined odd-node behavior.
- Membership proof generation.
- Proof verification.
- Non-membership proof strategy.

Write extensive tests.

### 12.2 Merkle Patricia Trie

Implement a genuine Merkle Patricia Trie.

Use a well-established implementation or a carefully specified educational implementation.

If using an external library, wrap it behind the commitment interface.

Document:

- Key encoding.
- Path encoding.
- Node types.
- Root calculation.
- Proof generation.
- Non-membership verification.
- Dependency and version.

Do not call a normal hash map with a SHA-256 root an MPT.

### 12.3 Verkle Tree

Implement a genuine Verkle commitment using an established, appropriate cryptographic implementation.

Document:

- Vector commitment scheme.
- Commitment parameters.
- Trusted setup requirements, if any.
- Key encoding.
- Proof format.
- Proof verification.
- Cryptographic assumptions.
- External dependencies.

Do not create a Merkle tree and label it Verkle.

If the chosen library cannot support a required operation, explicitly document that limitation and do not report full Verkle support.

## 13. Commitment configuration

Allow selection through configuration:

    {
      "commitment": {
        "algorithm": "merkle"
      }
    }

Supported names:

- merkle
- mpt
- verkle

Changing the commitment must not require changes to consensus or transaction execution.

Include a comparative test that executes identical transactions under all supported commitment algorithms and verifies each algorithm's own deterministic roots and proofs.

Do not expect different commitment algorithms to produce identical root hashes.

---

# PART VI — CONSENSUS

## 14. Consensus architecture

Consensus implementations must be independently replaceable.

Do not hardcode PREVOTE and PRECOMMIT into the shared consensus interface.

Define a protocol-neutral message envelope and protocol-specific message payloads.

A conceptual interface:

    type ConsensusEngine interface {
        Name() string

        Start(context.Context) error

        HandleMessage(
            context.Context,
            NodeID,
            ConsensusMessage,
        ) error

        HandleTimeout(
            context.Context,
            TimerEvent,
        ) error

        Status() ConsensusStatus

        Stop() error
    }

Provide an outbound-message interface or event sink so engines do not own network connections.

Provide a separate interface for finalized-block notifications.

Consensus engines must use the same node runtime, execution engine, storage interface, and network abstraction.

## 15. Primary consensus: Tendermint-style BFT

This is the primary consensus protocol for the original Lab01.

The original assignment specifies:

- PREVOTE.
- PRECOMMIT.
- Rounds.
- Locking.
- Quorum of 2f+1.
- Timeout-based progress.

Implement these requirements accurately.

Do not label this implementation classical PBFT.

### 15.1 Validator assumptions

The validator set is fixed during one test run.

Enforce:

    n = 3f + 1
    Q = 2f + 1

Reject invalid configurations.

The original assignment requires at least eight configurable nodes. Since n must equal 3f+1, the default cluster should use ten validators, with f=3.

Never silently use eight validators while claiming the exact fault model holds.

### 15.2 Protocol flow

Implement the full Tendermint-style sequence:

1. Proposer selection.
2. Proposal announcement.
3. Block body availability and validation.
4. PREVOTE.
5. Quorum observation.
6. Locking and valid-round tracking.
7. PRECOMMIT.
8. Finalization.
9. Height advancement.
10. Round advancement when progress is impossible.

Include NIL votes and timeout behavior.

### 15.3 Locking

Maintain the necessary protocol state:

- Locked block.
- Locked round.
- Valid block.
- Valid round.
- Current height.
- Current round.
- Current step.

Implement the protocol's locking and unlocking rules exactly.

A validator locked on a block must not switch arbitrarily.

Unlocking or changing its vote must be justified by the required valid-round evidence.

Document the rules and create adversarial tests for them.

### 15.4 Vote handling

Validate:

- Chain ID.
- Height.
- Round.
- Phase.
- Validator membership.
- Signature.
- Domain.
- Block hash.
- Duplicate votes.
- Conflicting votes.

Count at most one valid vote from a validator for a particular height, round, and phase.

Detect and record equivocation.

Retain sufficient evidence to explain conflicting messages.

### 15.5 Proposer and timeout policy

Use deterministic proposer selection.

Every validator must independently verify proposer identity.

Make the proposer-selection policy replaceable.

Implement configurable round timeouts and a documented progression policy.

Liveness should be evaluated only under the specified partial synchrony assumptions and eventual correct-proposer condition.

### 15.6 Finalization

A block becomes finalized only after:

- Block validation succeeds.
- Required votes are authenticated.
- The protocol's finality condition is satisfied.
- The relevant quorum evidence is verified.

Commit the resulting state only after valid finalization.

Persist finalized height, block hash, and state root.

Never equate block reception with finalization.

## 16. Secondary consensus: Classical PBFT

Implement classical PBFT as an independent engine.

Do not simply rename PREVOTE to PREPARE.

Implement the actual protocol concepts:

- Primary and backups.
- Pre-prepare.
- Prepare.
- Commit.
- View changes.
- New-view handling.
- Request ordering.
- Duplicate request handling.
- Checkpointing or a clearly documented simplified log-retention policy.
- Quorum authentication.
- View-change evidence.

Use the appropriate fault model and quorum requirements.

Do not reuse Tendermint locking rules as if they were PBFT rules.

For the initial Lab01 scope, implement the complete safety-critical protocol behavior required by the selected PBFT design, and document any intentionally omitted production optimizations.

The two engines must be selectable independently:

    {
      "consensus": {
        "algorithm": "tendermint"
      }
    }

or:

    {
      "consensus": {
        "algorithm": "pbft"
      }
    }

The rest of the node runtime must not need modification.

---

# PART VII — NETWORKING AND SIMULATION

## 17. Two independent execution modes

Support two modes.

### Mode A: Deterministic simulation

Run multiple logically independent nodes under a deterministic event scheduler.

Nodes must not share consensus state, ledger state, or mutable application state.

Communication must occur through message delivery abstractions.

### Mode B: Real local processes

Run multiple independent Go node processes.

Each process must have:

- Its own node ID.
- Its own key pair.
- Its own local ledger.
- Its own local state.
- Its own storage.
- Its own network server.
- Its own configuration.

Use gRPC and Protobuf for transport.

The two modes must use the same core domain logic and consensus engines.

Clearly document that deterministic simulation does not prove the absence of bugs in real transport, OS scheduling, or process recovery.

## 18. Deterministic event scheduler

Implement:

- Virtual clock.
- Event queue.
- Deterministic event ordering.
- Explicit event sequence numbers.
- Configurable message delivery times.
- Seeded fault decisions.
- Stable tie-breaking.

A network event should include:

- Event ID.
- Logical delivery time.
- Source node.
- Destination node.
- Message ID.
- Message type.
- Payload reference.
- Delivery or drop status.

Randomness must be deterministic.

Prefer independent PRNG streams per node and fault domain, with deterministic assignment of random decisions to events.

Do not depend on goroutine scheduling to determine random outcomes.

The same configuration and seed must reproduce the same event trace.

## 19. Network fault profiles

Support:

- Delay.
- Drop.
- Duplicate.
- Reorder.
- Bandwidth restriction.
- Peer disconnection.
- Network partition.
- Asymmetric connectivity.
- Temporary isolation.
- Recovery.

Faults must be configurable by:

- Node.
- Source-destination pair.
- Message type.
- Time interval.
- Scenario step.

Support file-driven network configurations.

### 19.1 Partial synchrony

Model:

- Unknown or configured-for-experiment stabilization time.
- Pre-stabilization message behavior.
- Post-stabilization bounded delivery.
- Configurable delivery bound Δ.

For tests, the simulator may know the stabilization time even though the consensus algorithm does not.

After stabilization, messages between correct connected nodes must obey the configured bound.

Do not use permanent random packet loss as a substitute for partial synchrony.

## 20. Header/body propagation

Preserve the original assignment's propagation rule:

1. Broadcast block header.
2. Receiver validates the header.
3. Receiver requests the matching body.
4. Sender supplies the matching body.
5. Receiver validates the body and block.

A body must be associated with the exact accepted header hash.

Test missing bodies, invalid body associations, duplicate requests, and delayed delivery.

---

# PART VIII — NODE LIFECYCLE AND BYZANTINE BEHAVIOR

## 21. Node lifecycle controller

Provide a controller that manipulates nodes independently of consensus.

Support:

- START.
- PAUSE.
- RESUME.
- CRASH.
- RESTART.
- ISOLATE.
- HEAL.
- STOP.

For real processes, support actual process termination and restart.

For deterministic simulation, model lifecycle transitions as explicit events.

Distinguish:

- A node that is alive but isolated.
- A node that is paused.
- A node that has crashed.
- A node that has gracefully stopped.

Do not treat these conditions as equivalent.

## 22. Recovery

On restart:

1. Load local persisted data.
2. Verify the integrity of the local finalized ledger.
3. Restore the correct consensus state.
4. Re-establish peer communication.
5. Request missing information.
6. Synchronize from the last finalized height.
7. Validate all recovered blocks and state transitions.
8. Verify the final state root.

A recovering node must not overwrite valid finalized history with unverified remote data.

Test crashes during proposal, voting, and finalization, including restarts after a prolonged offline period.

## 23. Byzantine strategy framework

Define adversarial strategies independently of consensus.

Support:

- Silent validator.
- Equivocation.
- Conflicting votes.
- Conflicting proposals.
- Invalid block proposal.
- Invalid transaction proposal.
- Duplicate messages.
- Replayed messages.
- Selective message suppression.
- Delayed message transmission.

The framework must be able to apply the same strategy to different consensus engines.

All adversarial actions must be observable in logs.

Do not allow Byzantine behavior in a test to secretly modify the private keys or internal state of honest nodes.

---

# PART IX — STORAGE AND OBSERVABILITY

## 24. Storage

Provide an abstraction for:

- Finalized blocks.
- Block headers and bodies.
- Finalized state.
- Consensus metadata.
- Validator metadata.
- Recovery information.

Provide an in-memory implementation for deterministic tests and a durable implementation for real local processes.

Persist information necessary for crash recovery and protocol safety.

A validator must not forget previously signed votes after a restart if doing so could violate its protocol's safety rules.

Use atomic or crash-safe persistence for critical consensus metadata.

## 25. Logging

Logs are primary correctness evidence.

Each event must include:

- Logical timestamp.
- Event sequence.
- Node ID.
- Event type.
- Height.
- Round or view, where applicable.
- Message ID, where applicable.
- Peer ID, where applicable.
- Structured event details.

Also record the actual wall-clock timestamp in real-process mode where useful, but never use wall-clock time to order deterministic simulation events.

Record:

- Message sent.
- Message received.
- Message delayed.
- Message dropped.
- Message duplicated.
- Peer blocked and unblocked.
- Node stopped and restarted.
- Proposal received.
- Vote sent and received.
- Equivocation detected.
- Block accepted or rejected.
- Block finalized.
- State committed.
- Synchronization progress.

Every rejection must have an explicit reason.

Provide JSONL logs and a deterministic canonical representation for comparison.

## 26. Metrics

Collect:

- Finalized height.
- Finalized block hash.
- State root.
- Consensus round count.
- Timeout count.
- Messages sent.
- Messages received.
- Messages dropped.
- Messages duplicated.
- Invalid messages.
- Equivocation events.
- Finalization latency.
- Node recovery time.
- Synchronization lag.

Do not fabricate metrics.

## 27. Invariant checker

Build an independent observer that checks safety and consistency.

At minimum:

1. No two distinct blocks at the same height are finalized by correct nodes.
2. All correct nodes that finalize a given height agree on its hash.
3. Identical finalized histories produce identical state roots under the same commitment algorithm.
4. Invalid signatures are rejected.
5. Invalid domains are rejected.
6. Duplicate votes are not double-counted.
7. Duplicate transactions are not applied twice.
8. Finalized history is not silently rewritten.
9. Recovered nodes validate their history before synchronization is accepted.

The observer must inspect actual events and ledgers, not trust a success flag emitted by the consensus engine.

---

# PART X — CONFIGURATION

## 28. Configuration system

Support JSON configuration files.

Validate every configuration before starting nodes.

The cluster configuration must define:

- Chain ID.
- Validator IDs.
- Public keys.
- Private-key references.
- Addresses.
- Consensus algorithm.
- Commitment algorithm.
- Network mode.
- Storage paths.
- Simulation seed.
- Fault model.
- Logging configuration.

Keep secrets out of committed configuration files.

Provide development-only key generation scripts.

### 28.1 Validator configuration

Enforce:

    n = 3f + 1
    Q = 2f + 1

Default:

    validators = 10
    max_byzantine = 3
    quorum = 7

Allow other valid configurations.

Reject invalid configurations before starting the cluster.

### 28.2 Scenario configuration

A scenario must describe:

- Initial cluster.
- Initial state.
- Transactions.
- Simulation seed.
- Network topology.
- Stabilization time.
- Fault schedule.
- Node lifecycle events.
- Byzantine strategies.
- Expected invariants.
- Maximum virtual execution steps.
- Output directory.

Example conceptual structure:

    {
      "name": "node_recovery",
      "seed": 42,
      "network_mode": "simulation",
      "cluster": {
        "validators": 10,
        "max_byzantine": 3
      },
      "faults": [
        {
          "type": "node_pause",
          "node": "node_3",
          "at": 100,
          "duration": 30
        },
        {
          "type": "node_resume",
          "node": "node_3",
          "at": 130
        }
      ],
      "assertions": [
        "no_conflicting_finalization",
        "eventual_node_convergence"
      ]
    }

The example is illustrative. Implement a documented, validated schema.

---

# PART XI — TESTING REQUIREMENTS

## 29. Unit tests

Implement unit tests for:

### Cryptography

- Valid signature.
- Invalid signature.
- Wrong domain.
- Wrong chain ID.
- Wrong height.
- Wrong round.
- Wrong message type.

### Serialization

- Canonical encoding.
- Golden vectors.
- Different logical objects produce different encodings.
- Same logical objects produce identical encodings.

### Execution

- Valid state update.
- Invalid ownership.
- Invalid nonce.
- Duplicate transaction.
- Invalid signature.
- Deterministic execution.

### Commitments

- Root calculation.
- Empty state.
- Key ordering.
- Membership proofs.
- Non-membership proofs.
- Invalid proofs.
- Update behavior.

### Consensus

- Quorum calculation.
- Vote authentication.
- Duplicate vote handling.
- Equivocation detection.
- Proposal validation.
- Locking and unlocking.
- Timeout behavior.
- Finality conditions.
- Restart safety.

### Networking

- Delay.
- Drop.
- Duplicate.
- Reorder.
- Partition.
- Healing.
- Bandwidth limits.
- Deterministic event ordering.

## 30. Mandatory original Lab01 tests

Implement all original tests T1–T8.

T1 — Normal run:

All correct validators finalize the same chain and state root.

T2 — Duplicate/reordered messages:

No double-counted votes and no conflicting finalization.

T3 — Invalid signature/wrong domain:

Messages are rejected and the reason is logged.

T4 — Replayed/duplicate transaction:

The transaction is applied at most once.

T5 — Drop/delay before synchrony:

Safety holds during pre-stabilization network faults.

T6 — Proposer silent/crashed:

Timeouts trigger and the chain progresses after the required assumptions are restored.

T7 — Up to f Byzantine validators equivocate:

No conflicting finalization occurs.

T8 — Same seed rerun:

Logs and final state are byte-identical in deterministic simulation.

Do not mark any test as complete until it exercises real code.

## 31. Additional experimental tests

Implement:

E1 — Node pause and resume.

E2 — Node crash and restart.

E3 — Node isolation and recovery.

E4 — Network partition and healing.

E5 — Asymmetric communication.

E6 — Consecutive proposer failures.

E7 — Byzantine equivocation.

E8 — Selective message transmission.

E9 — Invalid block proposals.

E10 — Delayed messages from old rounds.

E11 — State commitment comparison.

E12 — Consensus algorithm comparison.

E13 — Crash during consensus persistence.

E14 — Long-offline node synchronization.

E15 — Deterministic scenario reproduction.

E16 — F+1 Byzantine validators.

E17 — More than two-thirds Byzantine validators.

For E16 and E17, do not assert a guaranteed outcome that the original fault model does not provide.

Treat them as exploratory experiments.

Observe whether safety or liveness fails under the chosen adversarial schedule.

Report any violation without claiming that every execution must exhibit it.

An honest validator must still independently reject an invalid block.

## 32. Comparative experiments

Provide a way to compare:

- Tendermint-style BFT versus classical PBFT.
- Merkle Tree versus MPT versus Verkle.

Use equivalent transaction workloads and fault schedules where comparisons are meaningful.

Measure:

- Message count.
- Finalization latency.
- CPU time.
- Memory consumption.
- State commitment calculation time.
- Proof generation time.
- Proof verification time.
- Recovery behavior.

Clearly distinguish theoretical expectations from observed benchmark results.

Never claim that two algorithms are comparable if the experiment uses different assumptions without explaining them.

---

# PART XII — COMMAND LINE AND SCRIPTS

## 33. Required commands

Provide a simple command-line experience.

Examples:

    make build
    make test
    make test-unit
    make test-e2e
    make test-race
    make proto
    make run-sim
    make run-local
    make run-scenario
    make verify-determinism
    make clean

Provide commands to select algorithms:

    ./bin/simulator \
      --consensus tendermint \
      --commitment merkle \
      --scenario normal

And:

    ./bin/simulator \
      --consensus pbft \
      --commitment mpt \
      --scenario network_partition

For real local processes, provide commands to start, stop, restart, and inspect individual nodes.

## 34. Single test entry point

Provide one command that runs the mandatory test suite and produces an understandable summary.

Example:

    make test-all

The command must:

1. Run unit tests.
2. Run integration tests.
3. Run mandatory E2E scenarios.
4. Run deterministic replay tests.
5. Generate logs.
6. Check invariants.
7. Report failures with sufficient context.

Do not hide failing tests or convert failures into warnings.

---

# PART XIII — IMPLEMENTATION ROADMAP

## 35. Implement in controlled phases

Do not attempt to implement everything simultaneously.

Use the following order.

### Phase 0 — Repository audit

Inspect the current repository.

Deliver:

- Existing implementation inventory.
- Defect list.
- Architecture migration plan.
- Dependency list.

Do not destroy existing working code.

### Phase 1 — Domain model and cryptography

Implement core types, keys, signatures, canonical serialization, and domain separation.

Acceptance:

All cryptographic and serialization tests pass.

### Phase 2 — Execution and Merkle commitment

Implement deterministic transaction execution and the first genuine commitment plugin.

Acceptance:

Every node calculates identical state roots for identical ordered transactions.

### Phase 3 — Network abstraction and deterministic simulation

Implement virtual time, event scheduling, topology, delay, drop, duplication, and reordering.

Acceptance:

Identical scenarios produce identical event traces.

### Phase 4 — Node runtime and persistence

Implement independent node state, message handling, ledger, storage, and recovery foundations.

Acceptance:

Multiple independent nodes run without shared mutable consensus state.

### Phase 5 — Tendermint-style consensus

Implement the original Lab01 consensus requirements.

Acceptance:

T1–T8 pass with valid validator configurations.

### Phase 6 — Node controller and Byzantine behavior

Implement lifecycle manipulation and adversarial strategies.

Acceptance:

Pause, resume, crash, restart, isolation, and recovery experiments work.

### Phase 7 — Real gRPC local nodes

Implement independent local processes communicating over gRPC.

Acceptance:

A real local cluster can finalize blocks and recover after a process restart.

### Phase 8 — Classical PBFT

Implement a separate PBFT state machine.

Acceptance:

PBFT tests pass independently and the engine is selectable without changing the node runtime.

### Phase 9 — MPT and Verkle

Integrate genuine implementations through the commitment interface.

Acceptance:

Root, proof, and verification tests pass for each algorithm.

### Phase 10 — Comparative experiments

Implement benchmarking and comparative scenarios.

Acceptance:

Produce reproducible results with documented assumptions.

### Phase 11 — Final submission

Generate:

- README.md.
- REPORT.pdf, maximum 10 pages.
- Architecture diagrams.
- Test results.
- Required logs.
- Correct submission directory.
- Correct ZIP archive.

Do not claim completion if any mandatory original requirement is missing.

---

# PART XIV — ACCEPTANCE CRITERIA

## 36. Final quality gate

The project is complete only when all the following are true.

### Correctness

- Original Lab01 requirements are satisfied.
- Validator-set constraints are enforced.
- Authentication is verified.
- State execution is deterministic.
- Finality conditions are correctly implemented.
- Safety invariants are independently checked.

### Modularity

- Consensus engines are interchangeable.
- Commitment algorithms are interchangeable.
- Network transports are replaceable.
- Fault injection is independent of consensus.
- Node lifecycle control is independent of protocol logic.

### Experimental capability

- Nodes can be independently manipulated.
- Network partitions can be created and healed.
- Byzantine behavior can be configured.
- Crashed nodes can recover.
- Experiments are reproducible.

### Quality

- No placeholder implementations are presented as complete.
- No race detector failures in supported test configurations.
- No unexplained test failures.
- No silent error suppression.
- Documentation matches actual behavior.

### Submission

- README is complete.
- REPORT.pdf is within the page limit.
- All required tests have documented results.
- ZIP archive follows the required naming convention.

## 37. Required final engineering report

At the end, provide a summary containing:

1. Implemented modules.
2. Architecture decisions.
3. Implemented algorithms.
4. Dependency choices.
5. Mandatory test results.
6. Additional experiment results.
7. Known limitations.
8. Security assumptions.
9. How to run the simulator.
10. How to run independent local nodes.
11. How to introduce a new consensus algorithm.
12. How to introduce a new commitment algorithm.
13. How to create a custom fault scenario.
14. How to reproduce a failed experiment.

Separate completed features from planned or unsupported features.

Never claim that a feature is implemented merely because an interface exists.

---

# FINAL INSTRUCTION

You are responsible for producing the working project, not merely proposing its architecture.

Inspect first, implement incrementally, test continuously, and repair defects.

Preserve the original Lab01 requirements while building the experimental capabilities described above.

Prioritize protocol correctness over implementation speed.

When a design decision is uncertain, investigate the protocol specification and document the decision rather than guessing.

Do not stop at a generated project skeleton.

The final result must be buildable, executable, testable, reproducible, documented, and ready for submission.
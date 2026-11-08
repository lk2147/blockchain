# Stage 4: real networking with configurable message faults

The virtual transport and simulator were superseded and removed at the student's
explicit request. There is one cluster execution mode: independent Go node
processes communicating over real TCP/gRPC connections. Unit tests of protocol
functions and fault planning are not presented as cluster/network evidence.

Run from the repository root:

```powershell
python tests/run_local_cluster.py --faults
```

See [REAL_NETWORK.md](REAL_NETWORK.md) for architecture, configuration, actual
evidence and limitations. README has individual-node and tool-path commands.

Current implementation:

- `src/node/runtime.go`: node-local message handling, header/body exchange and
  retries using the existing consensus/execution/Merkle implementation.
- `src/realnet`: actual sockets, wall-clock timers, authenticated transport,
  message fault layer, queues, peer blocking, per-node logs/results.
- `src/network/message.go`: message/event/error types only; no virtual network.
- `src/cmd/node`: runnable node and fresh-cluster/key generator.
- `src/proto`: regenerated gRPC transport envelope definitions.
- `tests/run_local_cluster.py`: actual-process launcher and consistency checks.

The earlier virtual network, src/simulation and associated tests were removed.
The experimental Hello-world main.go was removed; src/cmd/node is the runnable
entry point. AGENTS.md, README and stage notes now describe the real-only goal.
The AGENTS.md ignore rule was removed so these directions can be versioned.

Repeatability means expected results and ordered transaction effects, not
identical timestamps. Tests directly check configured holds before actual RPC
transmission. Real socket-arrival order can vary. This is the student's approved
interpretation/adaptation of the PDF's strict deterministic-log requirement.

Actual checks: gofmt clean; go test ./... -count=1 passed; go test -race ./...
-count=1 passed including real multi-process clusters; go vet ./... passed.
The launcher itself was also executed successfully with ten node.exe processes.

This was the Stage 4 review boundary. Stage 5 subsequently added the real T1-T8
suite and controlled reruns; see STAGE5.md. Stage 6 subsequently prepared the
report, evidence and archive; see STAGE6.md for final checks and student review.

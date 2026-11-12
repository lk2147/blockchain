# Runtime logs

Each node writes events.jsonl in its own OutputDir, normally under local-runs.
The default is a concise event log and terminal output. Set Trace=true for the
complete event metadata used by packet-level integration assertions.

Old report snapshots and selected historical logs were removed during cleanup.
Current test evidence is in the fresh directory printed by tests/run_tests.py.
Finalized blocks live in each node's ledger.db, not in the logs or result.json.

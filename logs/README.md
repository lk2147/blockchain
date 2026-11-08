# Recorded verification evidence

evidence/ is an unmodified selection from local-runs/stage6-final, the final
race-enabled full suite executed on 2026-11-09. It includes summary.json,
go-test.jsonl, go-vet.log, all ten case directories' verified.json files, and
every available node events.jsonl/result.json. T6 has silent and crashed cases;
T8 has first and second cases. The crashed process may have no result.json.

repeatability/ contains comparison.json, the raw Go JSON log and both verified
summaries from the separately executed Python command with race detection at
local-runs/stage6-repeat. Its full node traces remain in that local run; the
full suite's T8 node traces are included under evidence/.

archive-check/ contains the extracted candidate's successful normal-suite
summary/raw Go log/vet output and ten production launcher result files.
Full traces for these extra checks remain in local-runs/stage6-unpacked.
The final archive's runnable files match the tested candidate; documentation
and this additional evidence were added after recording the extracted checks.

The final full suite passed all required scenarios and 203 tests/subtests;
formatting and go vet passed and no races were reported. Counts include test
parents/subtests. TestProcessNodeEntry is skipped only in the parent; it runs
inside actual children. An empty go-vet.log means vet returned no diagnostics;
the successful exit code is recorded in summary.json.

Original PIDs, absolute paths and real timestamps are retained as provenance.
Do not use these as runnable configs or expect another real run's logs to be
identical. No private-key files or original generated configs are included.
The reproducible fixtures and fault policies are in src/realnet/stage5_test.go
and adversary_test.go. New production runs generate fresh keys and configs.

Read events alongside results: receipt is not acceptance, a fault plan is not
necessarily an actual send, and a send failure is not a finalization. The Go
checker validates these distinctions, actual fault transmissions, header/body
gates, certificates and state. tests/STAGE6.md records the observed final counts.

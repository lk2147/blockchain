# Stage 6 - reproducibility and submission preparation

## Changes and stage boundary

No blockchain protocol source or Protobuf schema changed in this stage.
Added tests/verify_repeatability.py, tests/package_submission.py,
config/README.md, logs/README.md, selected evidence and report/REPORT.md.
Updated README.md, AGENTS.md, stage handoffs, TEST_STATUS.md and .gitignore.
Generated submission/blockchain.zip and its archive checksum; the ZIP contains
a per-file SHA-256 manifest and the required blockchain/ root directory.
Python remains the only launcher/experiment/packaging scripting language.

The report is Markdown for easy student review, under the project instruction
that replaces the PDF's section 10 submission rules. No team identities were
invented. If an instructor separately requires a PDF or institutional filename,
that presentation requirement still needs to be supplied. No external submission
has been made; student review/understanding is still required.

## Executed final checks

From the repository root, using the installed Python runtime and Go executable:

```text
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --race --output local-runs/stage6-final
python tests/verify_repeatability.py --go D:/tools/golang/bin/go.exe --race --output local-runs/stage6-repeat
```

Both commands exited 0. The first executed gofmt -l src (clean),
go test ./... -count=1 -json -race (203 passing tests/subtests, no missing T1-T8,
no failed tests or reported races), and go vet ./... (exit 0, no diagnostics).
The only parent skip is the actual child-process entry helper, not a scenario.
The second executed TestStage5/T8 again with race detection, launching two
independent real ten-node clusters and checking ordered signed transaction IDs,
expected state, certificates and actual fault transmissions. Its Python-side
comparison independently required both verified outputs to agree.

Both repeatability roots were:

```text
9fb497b5fbc68ed4eccf7f153742e02b87c3febb42bb54957da8cd9742fc38f6
```

## Actual final full-suite observations

These counts come from logs/evidence/*/verified.json, not predictions.

| Case | Observed result / selected counts |
|---|---|
| T1 | 20 honest finalizations, no timeout; common two-block history/root |
| T2 | 20 finalizations; 3,911 actual duplicate and 975 actual reordered sends |
| T3 | 18 honest finalizations; each honest node rejected bad signature and wrong domain |
| T4 | 18 honest finalizations; nine deliberately replayed candidate sends; nonce and old-height rejection checked |
| T5 | 342 drops, 29 timeouts; 20 finalizations after the complete-loss window |
| T6 silent / crashed | 18 honest finalizations and 18 timeouts in each variant |
| T7 | Seven honest finalizations; genuine conflicting votes from all three faulty validators; no honest conflict |
| T8 first | 89 drops, 127 duplicate, 124 reordered and 169 replay sends |
| T8 second | 89 drops, 134 duplicate, 128 reordered and 175 replay sends |

Counts vary because actual socket/process scheduling varies. Finalization counts
are per-node events, not chain height. T7 targets one block; other cases target
two. T5 tests complete pre-stable loss; nonzero artificial delay is tested by
T2/T8. T6 does not test safe process restart. T1-T8 initial conditions, inputs,
expected outcomes and invariants are documented in STAGE5.md and the report.

## Requirement-to-evidence map

| Requirement | Verification |
|---|---|
| Independent state/ledger, linked blocks, ordered execution | T1-T8 per-process PIDs, parent checks, independent re-execution |
| Owned updates, signatures, replay | Crypto/state/block unit tests; T3/T4 |
| Canonical encoding, chain/domain checks, hashes | Reference-vector and negative unit tests |
| Deterministic Merkle root including replay state | State reference roots/shapes, T1-T8 roots, T8 ordered effects |
| Fixed n=3f+1, votes/locks/NIL/quorums | Consensus unit cases; ten-node f=3 integration certificates |
| Partial synchrony, timeout/liveness, Byzantine bound | T5/T6/T7 finite real scenarios; no universal proof claimed |
| Delay/drop/duplicate/reorder/replay | Actual send/deadline assertions in T2/T5/T8 |
| Rate limits, temporary peer blocking | Realnet focused tests and integration send-gap checks |
| Header first, accepted/requested matching body | Real transport negative tests and all integration event checks |
| Logs and one entry point | run_tests.py; selected raw logs and summaries |
| Adapted repeatability | verify_repeatability.py, T8, comparison.json |
| Required structure and runnable archive | Manifest/integrity check and extracted-copy launcher check |

## Packaging and viva

Run python tests/package_submission.py to create a fresh archive. It uses an
explicit source/document/evidence allowlist, excludes executables and private
key files, verifies ZIP integrity and every manifest hash, and refuses overwrite.
The archive uses the AGENTS.md structure override, not the original PDF team-ID
name. Fresh experiments create their own keys; recorded runs are not resumed.

An archive candidate was extracted to local-runs/stage6-unpacked/blockchain.
From that root these documented commands were executed successfully:

```text
python tests/run_local_cluster.py --go D:/tools/golang/bin/go.exe --faults --base-port 29900 --run-dir local-runs/stage6-archive-smoke
python tests/run_local_cluster.py --config-dir local-runs/stage6-archive-smoke --check-only
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --output local-runs/stage6-archive-suite
```

The launcher/checker both exited 0: ten distinct real processes, matching
two-block histories and root
5380c5b782f31fc678927a9614f54ddda6bd0bd66f43bb81f54e70e875839703.
Observed controls: 197 drops, 263 duplicates, 235 reorder holds and 316 replays;
61 retried send errors. This fresh random-key cluster naturally has a different
root from the fixed-fixture suite. The extracted full normal suite also exited
0: all T1-T8, 203 passing tests/subtests, clean formatting and successful vet.
logs/archive-check contains its raw Go JSON/summary/vet and launcher results.

After recording these outcomes, the final ZIP was rebuilt with the updated
documentation/evidence. Every executable source/schema/module file was compared
with the tested extracted candidate and remained identical. ZIP integrity,
manifest hashes, required directories and exclusion of .hex/.exe/.ps1 files
were verified. Python syntax checks passed for all four scripts. Negative
checks confirmed archive overwrite refusal and missing-Go error reporting.

Before submission, the student should explain the code path in report/REPORT.md
and review all claims and limitations. Do not equate preparing artifacts with
student approval or claim production readiness.

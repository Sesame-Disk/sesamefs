# W2-4 controlled COMMITTED evidence isolation

Base main@5071d1701624b291a95db9ade8b5ff6abdbd63d7. Independent test-infrastructure
follow-up separated from E1-10e / PR #264 after cross-audit; no Sync or resurrection
runtime changes, new gate, schema, quota, TTL or GC activation.

The original controlled autoMerge/gcBeforeStage test correctly rejected HEAD
publication, but its later COMMITTED observation raced the shared daemon.
Failed full run: integration 591.150s. Main daemon at 20:17:08.788 UTC recovered
exact block cd64efa0972ebed687cb37bc2a69f23c2dbfc8970b87b17a0c458819f052f25e;
the fixture then found its canonical row gone. Preserve this failure as evidence.

Run all ten unchanged W2-4 legs on the existing manual-GC backend/keyspace in a
same-binary child. Check all child endpoints report GC OFF; preserve subfilters
and the existing required gate. The parent certifies only successful complete
child exit after cleanup. Original status/HEAD/refs/COMMITTED/K assertions and
the explicit fullyRetired leg remain unchanged. Shared daemon stays active.
Register exact K cleanup before retirement; fresh Background timeout and absence
assertion preserve cleanup even after canonical metadata disappears.

The original combined branch passed the isolated matrix 10/10, final serial
race 30/30 (73.548s), filtered/unavailable/active-GC negatives and standard full
Docker validation. This branch carries only that extracted infrastructure.
Independent-source verification and the final dependent full run are recorded
below before marking this prerequisite ready. Do not merge automatically.


## Independent-source verification (PR #265)

The extracted image contains main's production sources and no E1-10e
resurrection_admission.go. Required ten-leg W2-4 matrix passed three serial
same-binary race repetitions: 30/30, 76.527s. Filtered direct/writerFirst,
unavailable endpoint and active-GC endpoint all exited nonzero; no partial
child certificate is accepted. Exact-K teardown passed in every certified leg.

PR #264 is stacked on this independent prerequisite. Its diff excludes both
W2-4 test files and the W24 URL wiring; only the dependency supplies those
already-tested changes. Final standard Docker go-all-test against the updated
runtime is reported in each PR body before readiness. No automatic merge;
merge this prerequisite first, then retarget/rebase #264 onto main.

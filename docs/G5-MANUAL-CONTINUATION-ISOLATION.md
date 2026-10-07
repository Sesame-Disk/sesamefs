# G5 manual old-life continuation: isolated harness

Base main@4f7c29923fb6022c4b7e48996b50918011781679. Extracted from PR #266
as an independent pre-existing test-infrastructure fix; no E1-11 dependency.

The dev GC daemon can recover the old fixture root before the restarted manual
worker. Its legitimate n=0 result then violates the test's expected n>=1.
Only TestG5CassandraMinIOOldLifeWithoutDayScheduling delegates to the existing
e19 backend/keyspace, verifies GC is OFF there, and requires affirmative child
evidence before returning success. All original P1/P2, K1/K2, references and
worker progress assertions are preserved. Other daemon-dependent tests keep
the shared GC-enabled backend. No runtime, GC settings or timeout change.

Own negative controls reject filtered and unavailable evidence without relying
on E1-11 tests. Docker targeted race/gate/vet results will be recorded after
running this standalone branch. The prior combined full regression belongs to
the original PR #266 snapshot and is not a standalone-branch test claim.

## Standalone validation

Tested code commit 450599746: Docker ordinary/integration vets PASS; three
repetitions of all three G5 controls under race, 9/9 PASS (97.814s); gate
inventory PASS; independent filtered/unavailable negative controls PASS.
Both main and e19 cleanup checks report CLEAN with hard quota policy unchanged.
No production runtime or GC daemon configuration changes. Full go-all-test
was not rerun on this smaller standalone snapshot; targeted G5 evidence above
is independent of the original combined regression.

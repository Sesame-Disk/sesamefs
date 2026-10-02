# G4: independent physical lives and exact continuation

Decision: 2026-10-02. GC remains disabled (`GC_ENABLED=false`).
Deployment premise: clean greenfield installation; there are no old data to migrate.
G4 validates lives created by the current protocol and crash recovery within that deployment.

This decision supersedes the old requirement that every W2/R31 row close
before G4/G5 development. It does not close those rows or permit activation.

The authorized development order is W1 + covered W2 core evidence + G1/G2/G3,
then G4 -> G5 -> E1 targeted publication/GC evidence -> X1 closure ->
PRE-GC targeted validation -> A1 -> GC activation. Residual W2/R31,
PC-D1B.5, hard-delete non-fencing, ghost rows, link cleanup and lifecycle
findings remain recorded. Characterize them against the completed GC and
fix demonstrated defects before activation; they do not block G4 by merely
being open. Independent findings are follow-ups unless this change introduces
or depends on them.

## G4 contract

1. PREPARED is recoverable but cannot delete bytes.
2. D1 commits on exact P1; COMMITTED orphan(P1,D1) is confirmed before retirement.
3. Canonical retirement vacates blocks(L). A writer mints and PUTs a fresh K2
   and makes one INSTALL attempt for P2. An old orphan is never a mutex on L.
4. Reuse, repair and publication authority resolve the current canonical row.
   An active in-row claim still blocks. A stale P1 tuple becomes Changed;
   absence never authorizes repairing or reinstalling P1.
5. Physical continuation owns only the persisted P1/K1 and exact D1 certificate.
   References of L after D1 do not authorize, cancel or postpone deleting P1.
6. Restart can finish P1 with blocks(L)=P2 and live references for P2.
   Failed/ambiguous operations retain exact authority; terminal replay cannot
   acquire it again. Neither metadata nor bytes nor references of P2 change.

## Implementation

Writer changes: ProbeBlockReuse, BlockDeleteFenceActive,
ValidateBlockRepairAuthority/ValidateBorrowedFSPublicationAuthority and
RepairReleasedBlockStub stop using logical orphan existence as a fence.
Existing INSTALL and tuple-bound non-creating repair CAS remain intact.
No new schema or per-block Paxos read is introduced on the dedup path.
The separate baseline certifier stays conservative and outside this slice.

COMMITTED recovery uses existing exact canonical orphan and SERIAL lifecycle
settlement. FinalizeBlockDelete retains its exact (P,D) CAS. A SERIAL
observation of a different, complete P2 can classify P1 as already retired
only with the exact orphan and published lifecycle certificate. No P2 mutation
is authorized by that classification. Storage validation and a fresh topology
gate still precede DeleteBlockByStorageKey(K1). Canonical orphan reload,
phase persistence, terminal settlement and exact cleanup retain crash safety.
The legacy recovery path keeps its existing liveness policy.

## Evidence and remaining gates

New unit proof: TestG4RecoveryDeletesOnlyRetiredLifeWithReplacement covers
productive handoff, referenced P2 coexistence, failed storage, restart retry,
exact K1 deletion and terminal replay. New real-service proof:
TestG4CassandraMinIOPhysicalLifeCoexistence covers minted K1/K2, real Cassandra
INSTALL and handoff, rowless writer admission, P2 reuse/repair, stale P1
rejection, fresh-worker recovery, K1 absence and intact K2 bytes/metadata/refs.

G4 does not claim G5 scheduling hardening, all publication funnels proven,
X1 closed or production ready. E1 must attempt real Sync, SeafHTTP, OnlyOffice,
copy/move and restore/revert publication interleavings and crash/restart with
completed GC. Re-evaluate each W2/R31/PRE-GC finding with a reproducible current
sequence, fix real failures, and retain unresolved activation requirements.
A1 remains a separate activation decision after this evidence.

## Validation record (2026-10-02)

Base: main `79ca86bf8a89b3570e8fea2dfd599b562e1d483c`.

- PASS: `go test ./...` on a snapshot of versioned/current sources. DB, GC,
  API/v2 and all other Go packages passed. A direct workspace run also sees
  ignored historical `.go` copies in `tmp`, which fail package/inventory
  checks; those files were preserved rather than altered to manufacture green.
- PASS: real Cassandra + MinIO `TestG4CassandraMinIOPhysicalLifeCoexistence`
  and `TestEveryEvidenceGateIsWiredIntoTestMain`, with
  `SESAMEFS_REQUIRE_G4_EVIDENCE=1`. Single-DC evidence, not a three-DC claim.
- PASS: final complete GC unit suite and `go test -race ./internal/gc
  -run ^TestG4 -count=1` in the Linux test runner.
- PASS: related real-service regressions for active/released stubs, claim
  identity and the existing X1 characterization matrix.
- PASS: new negative cases for missing/terminal lifecycle certificate,
  ambiguous retirement and rejected topology; failed storage preserves D1
  and restart completes only K1. Covered post-D refs do not revoke D1.
- Expected semantic RED: the new physical-continuation proof against the
  base main reports `physical continuation: 0 <nil>` because COMMITTED
  remains parked. It compiles and reaches the protocol boundary.
- Expected gate failure: requiring G4 evidence but selecting only the wiring
  test returns non-zero with `required G4 Cassandra/MinIO coexistence evidence
  was not observed`.

The old 3-DC writer-fence test now specifies the G4 contract: deleting P1
blocks before retirement, fresh P2 is admitted afterwards, stale P1 repair
returns Changed, and the exact orphan remains intact. That 3-DC leg was not
executed in this single-DC validation; it remains separate evidence.

Reproduce with `./scripts/g4-validation.ps1 -Stage All`. The runner snapshots
current tracked and non-ignored new sources, retains ignored historical files
in place, uses the configured local `.env` through Docker, writes separate
logs in `tmp/g4-*`, and restricts physical-recovery enumeration to the fixture
organization. It leaves the source snapshot for diagnosis. No service
configuration, GC enablement, migration or rollout is performed.

## Follow-up audit (2026-10-02)

The full current-source suite and required Cassandra/MinIO proof were repeated.
The audit added fail-closed rejection of unknown nonempty recovery states rather
than interpreting them as legacy authority. Additional restart tests inject
failure after DeleteExact(K1), both before phase persistence and during orphan
cleanup, while P2 has live references in a different storage class. They verify
that every initial or replayed delete retains the complete org/class/K1 identity,
that cleanup resumes without reacquiring a terminal delete, and that P2 remains
intact. This is current-protocol crash evidence; migration of historical data
is outside the greenfield deployment premise.

## Separate evaluation: remove obsolete orphan recovery compatibility

The deployment is greenfield: production will start after GC is complete, and
there are no historical deployment data to preserve. The empty recovery-state
branch in RecoverS3Orphans predates the PREPARED/COMMITTED protocol. It checks
BlockExists(L) and BlockHasReferencesGlobal(L), whereas current COMMITTED
continuation settles the exact P,D certificate. StartBlockDeleteOrphan still
creates the old empty-state shape and is used by historical test fixtures;
the productive worker uses PrepareBlockDeleteOrphan, commit and promotion.

Decision requested on 2026-10-02: retain this branch in the G4 PR and evaluate
its removal in a separate PR. Inventory production callers and test fixtures,
replace obsolete fixtures with current-protocol setup while retaining negative,
identity, topology and crash coverage, remove the old publisher/recovery route
if the inventory confirms no current dependency, and require the full suite
plus current-protocol Cassandra/MinIO evidence. Historical deployment
compatibility alone is not a reason to retain it. This evaluation is not an
additional G4 prerequisite or a claim that old data exist.

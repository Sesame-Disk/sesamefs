# W2-0: native wire loss, process death and mixed rollout

Base: `main@76d68c928f127f82fcea69a98401223051098911` (#234).
Branch: `codex/w2-0-wire-crash-rollout`.

## Disposition

**W2-0 is CLOSED-EVIDENCE for the covered current-version mechanism** under
the [supported first-production contract](./X1-CRITICAL-PATH.md#first-production-deployment-contract).
#241 is merged; the original evidence baselines below remain historical.
The disposition is reconciled on 2026-09-30: durable non-TTL repair acquired
before final exact-P, destructive EACH_QUORUM repair/ref observation and final
reference re-read exclude expiry-before-HEAD for the covered chains. Real
native wire ambiguity, OS process death, independent recovery and three-DC
visibility support this source-level ordering argument. No independent
current-version W2-0 residual was identified; this is not CLOSED-GATED on
GC disablement, nor a claim of exhaustive fault timing or complete W2.

Both mixed-version D(P)+HEAD counterexamples remain valid and unchanged.
They require incompatible pre-#239 components or attempts absent from v1's
clean deployment and are tracked as P1 FOLLOW-UP / GENERAL in
ISSUE-W2-INCOMPATIBLE-MIXED-ROLLOUT-01. No version gate was implemented.
W2-1/2/6a retain their individual R31 dependencies; unsupported mixed rollout
is no longer their closure dependency.

The #201/#236 order remains W2 complete -> G4 -> G5 -> E1 -> X1 CLOSED ->
PRE-GC -> A1 -> GC ON. G4 cannot start while W2 rows remain open. Hard-delete
nonfencing remains its own current-runtime/PRE-GC item. Neither this PR nor
X1 closure authorizes GC activation.

This is an evidence-only follow-up to #239. There are no productive query,
schema, writer, repair settlement, lease or GC activation changes. The real
session adapter exists only under the `integration` build tag.

## Current-version evidence

Six productive funnels: Office CreateFile, UploadFile, BorrowedFS and
SessionUpload CreateFileFromBlocks, PutBlock-provenanced direct Sync HEAD,
and PutBlock-provenanced Sync auto-merge.

The native v4 proxy forwards actual Cassandra frames. It identifies prepared
HEAD CAS requests and either discards the request before delivery or discards
the coordinator's real response. A second mode also loses confirmation reads.
The real driver reports `gocql.ErrTimeoutNoResponse`; no seam invents the
CAS outcome. Verification bypasses the proxy and matches the canonical HEAD
to the precise commit in this attempt's durable repair row.

For each funnel, three wire legs cover applied/readable, applied/unconfirmed,
and request-lost/unconfirmed. F1/F2/F3 confirm and settle readable application;
Sync conservatively returns failure and retains repair even in that leg.
Unknown outcomes retain repair. After the temporary refs are removed to model
TTL expiration, productive GC postpones and preserves the exact candidate,
canonical placement and MinIO bytes. Independent recovery promotes reachable
fs references and settles repair for applied attempts. Unapplied attempts
remain conservatively guarded; bounded reclamation is still R31.

For each funnel, subprocess legs kill the productive writer before repair,
after final exact-P authority, and with an actually applied HEAD response
still in flight. Office also dies after HEAD before promotion. The parent
requires Linux OS `SIGKILL`, so child deferred cleanup cannot run. Before repair,
HEAD stays unchanged and D may commit. After acquisition, durable repair
survives death and prevents D even without temporary refs. Applied attempts
recover through a fresh session and the productive discovery sweep.

These are **37 mandatory named legs**, not a claim of exhaustive arbitrary
failure timing. Full profile runners require all 37. A filtered, skipped,
failed or unavailable run cannot be green evidence.

## Real three-datacenter visibility

Cassandra 5.0.9, NetworkTopologyStrategy RF=1 in dc-na/dc-eu/dc-asia. Hinted
handoff is disabled, NA/Asia are stopped, and EU acknowledges repair at
LOCAL_QUORUM. After restart the test first requires a clean NA LOCAL_QUORUM
miss; otherwise the drill fails as invalid. Productive EACH_QUORUM finds the
EU-only repair. Permanent fs reference promotion followed by repair removal
preserves global liveness. Stopping EU makes the destructive probe return
Unknown with `RequestErrUnavailable`, never Zero.

The LOCAL_QUORUM repair-read mutant fails semantically on that blind NA
fixture. Compilation errors cannot satisfy the negative control. This drill
proves a quorum intersection premise; it does not claim an atomic network
snapshot or a global safe state from one ordinary read.

## Mixed-version counterexamples

The legacy binary is compiled from exact pre-#239 commit
`50c50903e7ac49c04ef36f460dccc35ce122dfd6` (#238). The runner verifies the
Git archive commit identity. Only an integration session adapter, a helper
and child TestMain isolation are injected; productive legacy code is unchanged.

| Combination | Actual interleaving | Result |
|---|---|---|
| New writer + old GC | New Office acquires repair and validates P; temporary refs expire; old productive GC commits D and retires canonical P; writer resumes HEAD | D(P)+HEAD, unsupported |
| Old writer + new GC | Old Sync finishes readiness; its real repair INSERT is held before acquisition; pins expire; real candidate is queued; current productive `Worker.ProcessOrgOnce` probes liveness, commits D and retires P; old INSERT and HEAD resume | D(P)+HEAD, unsupported |

Tests pass only by **requiring these counterexamples**. A green rollout test
therefore means the unsafe combinations were reproduced, not certified safe.
Upgrading readers first alone is insufficient for such a future deployment.
Supporting an incompatible rolling upgrade with active GC would require a
separately reviewed compatibility/drain protocol. These legacy attempts do
not exist in the supported greenfield first-production deployment, where every
productive component is compatible before traffic. This accepted deployment
contract reconciles the original broad rollout blocker; it does not repair
either counterexample. No startup version/A1 gate is implemented here.
Existing A1/PRE-GC prerequisites remain separate and mandatory.

## Individual audit and limits

| Row / subset | New evidence | Closure impact |
|---|---|---|
| W2-1 exact SessionUpload | Native ambiguity, SIGKILL and independent recovery | Covered pre-HEAD continuity re-audited with closed W2-0; OPEN through R31 W2-11..14 |
| W2-2 foreign fs BorrowedFS | Same, with foreign ref removed after own repair acquisition | Covered pre-HEAD continuity re-audited with closed W2-0; OPEN through R31 W2-11..14 |
| W2-6 UploadFile | Same, exact materialized P and attempt commit checked | Historical CLOSED-FIX pre-HEAD unchanged |
| W2-6a Office | Same plus after-HEAD SIGKILL | Covered pre-HEAD exact-P/continuity proved; OPEN through R31 W2-11..14 |
| W2-3 direct/merge Sync | PutBlock-provenanced subset only | Expired/missing provenance and other Sync rows remain OPEN |

R31 abandonment/UNKNOWN liveness, SeafHTTP, OnlyOffice, resurrection,
cross-repo, G4/G5/E1 and the future physical executor are outside this proof.
Pre-D guard-only preserves pending work; post-COMMITTED late repair cannot
veto D. Existing #239 mechanism mutation tests remain part of validation.

## Reproduction from Windows (all Go execution in Docker)

Use the original Windows checkout, not WSL. All scripts use only projects
`sesamefs-w2-0-evidence` and `sesamefs-w2-0-3dc`, private volumes/networks, and
no host service ports. Production GC stays disabled. The full regression
stage temporarily enables only its isolated primary scanner and restores it
in `finally`. Run integration stages serially: TestMain cleanup is shared.

```powershell
./scripts/w2-closure-validation.ps1 -Stage Wire
./scripts/w2-closure-validation.ps1 -Stage Rollout -SkipBuild
./scripts/w2-closure-validation.ps1 -Stage Regression -SkipBuild
./scripts/w2-closure-validation.ps1 -Stage Mutations -SkipBuild
./scripts/w2-closure-validation.ps1 -Stage Gates -SkipBuild
./scripts/w2-closure-3dc-validation.ps1
```

The 3DC script restores hinted handoff, stops its fixture and removes only
its uniquely named disposable runner in `finally`. It never deletes volumes.
The mutation script edits a disposable image filesystem, never host source.

## Validation record (2026-09-30)

- Native wire/process matrix: 37/37 PASS, 113.579s (`tmp/w2-wire-crash-matrix-third.log`);
  later exact-attempt HEAD assertions are checked by the final regression run.
- Initial pinned rollout (before P2 correction; current-GC leg used direct primitives): PASS, 7.569s
  (`tmp/w2-rollout-first.log`).
- Real 3DC seed/readGuard/promote/readPermanent/unavailable/cleanup PASS;
  LOCAL_QUORUM repair-read mutation semantic RED. Logs: `tmp/w2-3dc-*.log`.
- Docker `go test -short -count=1 ./...`, `go vet ./...`, and
  `go test -race -short -count=1 ./...` PASS. Broad tests use the Docker source
  snapshot to avoid host node_modules traversal.
- Four evidence negative controls PASS (`tmp/w2-closure-gates.log`); pinned
  initial rollout rerun with archive identity verification PASS, 6.087s (`tmp/w2-rollout.log`).
  These initial logs do not prove the current productive worker path; see the P2 correction below.
- Supported full compose integration PASS, 409.953s (all 37 named legs, exact
  attempt HEAD assertions included): `tmp/w2-integration-full.log`.
- Integration-tag `go vet` and Docker Windows cross-compilation PASS.
- Mechanism mutations PASS: baseline 17 legs GREEN; three semantic D+HEAD
  mutants and two worker lifecycle mutants RED; filtered/unavailable controls
  fail closed (`tmp/w2-mechanism-mutations.log`).
- All 37 new named legs PASS with `-race`, 103.166s; no race reports
  (`tmp/w2-integration-race.log`).
- Native Windows 3DC runner PASS end-to-end: all six phases and semantic
  LOCAL_QUORUM negative control; hints restored, own fixture stopped, unique
  runner removed (`tmp/w2-3dc-final-runner.log`).
- Final scope audit and staged `git diff --check` PASS. All additions are
  evidence/adapters/docs/runners; no productive CQL/schema/protocol change.

Initial failing runs exposed fixture errors (UUID, capture timing, foreign
reference lifetime and Sync's conservative error semantics); these were
corrected and do not count as successful evidence. The first broad run exposed
an org-wide GC count assertion; the corrected test selects one actual queued
candidate and requires the productive liveness probe to execute. The legacy
worker mutation regex now accepts Windows CRLF. Cleanup was retried after
EU became healthy. No failing backend or filtered run counts as proof.

## P2 audit correction: productive oldWriterNewGC evidence

The audit of original HEAD `204208722d7a1bbd598540e384e093d754645e4a`
confirmed a THIS-PR evidence defect. `oldWriterNewGC` used
`x1CommitHandoffAfterZeroRefs`, which drives claim/reference/handoff primitives
without a queued candidate or the productive worker's checks. Its claim of
productive current-GC evidence was too broad. This was P2 evidence scope,
not a productive defect or evidence that mixed deployment was safe.

The corrected leg queues a real exact candidate through `w2Candidate` and
runs `w2Worker(...).ProcessOrgOnce` with grace=0. The existing discovery
wrapper selects only that real queue row; all candidate, topology, claim,
publication-liveness and handoff methods delegate to the real Cassandra store.
The test requires one retirement and observes the worker's pre-D liveness probe.
It independently requires canonical retirement, exact COMMITTED orphan authority,
durable physical continuation and unchanged HEAD before releasing the old INSERT.
After the legacy writer publishes its exact target HEAD, the same COMMITTED
continuation remains required. No direct handoff helper creates D in this leg.

The refs/repair probe distinction was not the primary blocker: while the real
INSERT is held, both pins and repair are absent. The corrected experiment
confirms that the productive current worker can reach D in that state.

Correction validation (Docker): both rollout legs PASS, 10.426s
(`tmp/w2-p2-rollout-runner.log`); both legs also PASS with `-race`, 12.249s
(`tmp/w2-p2-rollout-race.log`). In a disposable Docker copy, substituting the
worker call with the old direct handoff helper and a synthetic success count
fails specifically on the missing productive liveness probe; compilation
errors cannot satisfy this negative control
(`tmp/w2-p2-primitive-substitute-result.log`). No production or unrelated scope change.

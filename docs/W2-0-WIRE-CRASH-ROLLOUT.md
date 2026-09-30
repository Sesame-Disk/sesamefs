# W2-0: native wire loss, process death and mixed rollout

Base: `main@76d68c928f127f82fcea69a98401223051098911` (#234).
Branch: `codex/w2-0-wire-crash-rollout`.

## Disposition

**W2-0 remains OPEN.** The covered current-version mechanism survives real
HEAD wire ambiguity and OS process death. Both mixed-version configurations
have concrete D(P)+HEAD counterexamples. `GC_ENABLED=false` is an operational
requirement, not a version-aware fail-closed rollout gate; this evidence does
not justify calling the whole row CLOSED-GATED.

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
| Old writer + new GC | Old Sync finishes readiness; its real repair INSERT is held before acquisition; pins expire; new GC commits D; old INSERT and HEAD resume | D(P)+HEAD, unsupported |

Tests pass only by **requiring these counterexamples**. A green rollout test
therefore means the unsafe combinations were reproduced, not certified safe.
Upgrading readers first alone is insufficient. GC must stay disabled
fleet-wide while all destructive readers and covered writers are upgraded,
and old processes and in-flight old attempts must be drained or terminated.
The rollout exclusion still needs a reviewable enforceable mechanism or an
explicitly accepted deployment contract before W2-0 can close. A startup
version/A1 gate is not implemented here and must not be inferred from the
configuration default. Existing A1/PRE-GC prerequisites remain separate.

## Individual audit and limits

| Row / subset | New evidence | Closure impact |
|---|---|---|
| W2-1 exact SessionUpload | Native ambiguity, SIGKILL and independent recovery | Shared current-version premise strengthened; row remains OPEN |
| W2-2 foreign fs BorrowedFS | Same, with foreign ref removed after own repair acquisition | Shared premise strengthened; row remains OPEN |
| W2-6 UploadFile | Same, exact materialized P and attempt commit checked | Historical CLOSED-FIX pre-HEAD unchanged |
| W2-6a Office | Same plus after-HEAD SIGKILL | Row remains OPEN; no automatic full closure |
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
- Pinned mixed rollout: both required counterexamples PASS, 7.569s
  (`tmp/w2-rollout-first.log`).
- Real 3DC seed/readGuard/promote/readPermanent/unavailable/cleanup PASS;
  LOCAL_QUORUM repair-read mutation semantic RED. Logs: `tmp/w2-3dc-*.log`.
- Docker `go test -short -count=1 ./...`, `go vet ./...`, and
  `go test -race -short -count=1 ./...` PASS. Broad tests use the Docker source
  snapshot to avoid host node_modules traversal.
- Four evidence negative controls PASS (`tmp/w2-closure-gates.log`); pinned
  rollout rerun with archive identity verification PASS, 6.087s (`tmp/w2-rollout.log`).
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

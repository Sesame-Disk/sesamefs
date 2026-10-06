# E1-10e / W2-10 — unpublished historical source admission

## Frozen plan

Base main@5071d1701624b291a95db9ade8b5ff6abdbd63d7 (#263 merged).
Do not infer fs_object existence implies permanent fs: or close W2-10 from a
remover inventory alone. Cross existing PC-0/PC-D1B.4/identity inventories with
source acquisition and admission. Classify GC Phase4/library cascade as well
as Phase5/6; guards do not trace every new caller of inventoried wrappers.

Freeze productive counterexample first, without runtime change: real web block
upload -> Sync PutCommit/RecvFS -> unpublished commit/tree exists, no fs:/repair,
only temporary source up:. Lapse only owned temporary reference (explicit
expiry-state control with cleanup registered), never delete permanent fs: or
historical metadata. Real owned GC reaches exact COMMITTED and TERMINAL P1.
Before new PutBlock can rematerialize, call real RevertDirents with historical
commit_id and one root plaintext file/path/block, same repo/org, one DC.

Control live unpublished source plus retained-history control. Assert actual
response successes/failed, HEAD/tree/parent, exact historical file/block layout,
P1/K1, mapping, refs/pub:/fs:, repair, lifecycle and recovery root. No invented
GC/projection outcomes. Use existing integration hooks and manual-GC child on
sesamefs-e19; preserve all previous gates/selectors and standard daemon.
Actual handler with owner context certifies runtime, not external routing/auth.

If current code publishes dependency on retired P1, preserve RED evidence,
then choose minimum explicit source-admission or publication correction for
this RevertDirents single-file sequence. Fail closed on missing/unknown evidence;
record policy/compatibility limits. Do not invent a coordinator or fix all four
funnels/Phase5/6/R31. Counterexample must return by omitting only the new guard.
Keep W2-10 overall OPEN; other handlers/shapes need separate dispositions.

Register cleanup early for libraries/exact objects/mappings/block/GC artifacts
and temporary expiry; cleanup errors invalidate child certificate. Execute shared
keyspace binaries serially, never raise quotas or disable shared GC. Verify final
owned residue absent. Independent required evidence gate with named legs and
selector; negative filtered/unavailable/GC-active checks, restored-source race,
source hashes, existing inventory guards and five prior evidence groups.
Normal/integration vet, format/whitespace; actual Docker go-all-test last. Audit
all final changes, commit/push and create PR without merging. Production GC OFF.


## Observed result and correction

The report's proposed blanket CLOSED-EVIDENCE disposition is not supported.
The existing E1-7 productive precursor persists unpublished SHA-1 fs_objects
without permanent fs:. RevertDirents accepts that commit_id without verifying
that it ever won HEAD. On unchanged runtime, all three initial legs published
HEAD; the COMMITTED and TERMINAL legs reached the original historical file
while exact canonical P1 was absent. D came from the real owned block worker;
no permanent refs, history metadata, mapping or D were fabricated/deleted.
The owned temporary up: lapse is an explicit expiry-state control, not a test
of wall-clock TTL expiry/discovery. Corrected primary RED run: 9.121s.

The minimum correction is a **source-admission policy for non-directory
RevertDirents items**, not a general resurrection publication adapter:

1. Read historical file layout, resolve canonical blocks (including Sync SHA-1).
2. Require the exact own permanent fs:<repo>:<file> on every canonical block.
3. Capture current exact P through the existing read-only placement helper.
4. On every retry, recheck those refs and original exact-P authority immediately
   before HEAD CAS. Missing/unknown/contradictory evidence fails the item.

The existing HTTP 200 success/failed contract is preserved. Live unpublished
metadata with only somebody else's upload pin is now failed and HEAD untouched.
Previously settled Sync history still restores successfully after real Sync
PutBlock/HEAD and DeleteFile. Empty files have no physical dependency; no
ancestry claim, own pin, stage, repair, rematerialization or coordinator added.
No directories or other handlers are migrated. Concurrent Phase5/6 removal
between observation and HEAD remains outside this admission proof.

## Evidence and audit

- Final directed matrix 4/4 PASS (10.973s): live-unpublished rejection,
  exact COMMITTED rejection, exact TERMINAL rejection, published-sync-history
  success. Original historical SHA-1 layout/mapping remains unchanged; named
  HEAD/tree/P/K/refs and repair state are observed through Cassandra/SILO.
- Five race repetitions 20/20 PASS (64.300s), same instrumented binary/child.
- Mandatory named-leg gate: filtered live-only fails for missing remaining legs;
  unavailable endpoint fails backend setup; GC-active endpoint fails owned-worker
  isolation. Parent cannot certify a failed child's teardown.
- Omit only the two new source-admission call sites (capture + final recheck)
  in a disposable runner, then restore exact original source with EXIT trap:
  three rejected legs return to unsafe success/HEAD publication, including real
  COMMITTED/TERMINAL; published history remains successful. Host source unchanged.
- Exact K teardown is registered before GC, retains key after canonical removal,
  uses a fresh Background timeout and asserts absence. Initial RED teardown used
  canceled t.Context and left one known K; corrected before primary evidence,
  then that recorded exact K was deleted and absence verified separately.
  Inherited metadata cleanup remains shared test infrastructure, not a new
  proof that every legacy helper handles all cleanup failures.
- Existing direct PC-0 seam guard does not trace helper calls. Its comment and
  characterization now state that limitation and the new indirect exact-P gate.
  No stage/repair/own-pin migration is inferred from inventory classification.
- Permanent-ref removal uses the inventoried DB primitive called by GC workers.
  Library cascade/Phase4 and Phase5/6 must be classified independently; the
  wrapper inventory alone cannot prove absence of future non-GC callers or
  prove source admission. This counterexample does not need history destruction.
- Both normal and integration vet PASS; PC-0/PC-D1B.4/identity guards PASS (2.292s).
  Previous retained-history selectors and actual standard Docker go-all-test
  are recorded below when finished.

Disposition: measured RevertDirents single root file/plaintext block source
admission = CLOSED-FIX. W2-10 overall, other handlers/directories/batches,
concurrent cleanup/retention, W2-11..14/R31, E1/X1 remain OPEN. Production GC
remains OFF; shared dev daemon and quota settings are unchanged.

### Original #264@6f0855984 source snapshot (superseded below)

- `internal/api/v2/resurrection_admission.go` SHA-256 `79a4d3b531fee6b03d979452a4da9f2933d4af68fba1f5e2530320d22547ed3a`
- `internal/api/v2/trash.go` SHA-256 `7bd009f7e9686fe3d7571965358bcfeeca3dd4932d7056be3743f6a7cd724d76`
- `internal/integration/e110e_unpublished_resurrection_test.go` SHA-256 `4202cbdf396c49f62646e61229cc5552b79284c71277c63a9dad4c99a4336562`
- `internal/integration/integration_test.go` SHA-256 `40a1bd68ccb3b0088be93ab2a10a19f7a00193174d08cc605eccc67c01f36597`
- `internal/integration/e17_sync_recvfs_test.go` SHA-256 `846068045c74700296bc2e6775b77f17616b990073dd8284f3d8e32da3cf9060`


### Prior selectors and removal inventory

Required retained-history selectors all PASS in one serial run (52.943s):
RevertFile 6/6; RestoreTrashItem file 3/3; RestoreTrashItem directory 3/3;
RevertDirectory 3/3; RevertDirents 3/3, including existing real HEAD-conflict retry.
Normal/integration vet and inventory checks used restored final Go source.
Final image SHA-256 matches the source snapshot above (format-only import
regrouping after race; no runtime/test behavior changed).

Source trace: DB.RemoveBlockReference is the raw reference delete;
CassandraStore.RemoveBlockReference wraps it; Worker.removeFSObjectBlockReferences
resolves canonical IDs and invokes it for permanent fs: after beforeMutation.
processFSObject calls the guarded cleanup; processCommit queues FS children,
and processLibraryCascade handles deleted-library cascades. Phase4 library
participants have lease checks/fences, while history/trash Phase5/6 keep-set and
execution-time reachability gaps remain PRE-GC obligations. The inventoried
cleanupFailedPublishDeleteFSObjectFn still has no productive caller. This is
an observed current-call-site inventory, not a theorem that wrappers can never
get new callers and not a reason to close unpublished source acquisition.


### Runtime image audit

First standard go-all run passed (integration 546.815s) with new handlers
compiled into integration but pre-existing HTTP backend images. This is kept
as preliminary regression evidence, not the definitive updated-runtime run.
All four HTTP backends were then rebuilt from the final workspace and recreated
without changing Cassandra/MinIO volumes, quota or GC environment. Each running
binary has SHA-256
`c93598d7f0e89352253dbd63db1f1c715e98fd7de9e8c981b8c7e897a19c33b9`.
A second complete standard Docker go-all-test against that refreshed fleet is
required below. No integration binaries run concurrently against its keyspaces.


### Final-run discovery: existing W2-4 COMMITTED observation race

The refreshed-fleet run failed (integration 591.150s) only at the existing
W2-4 autoMerge/gcBeforeStage leg: writer correctly rejected and kept HEAD;
its later assertDUnrevoked expected the canonical COMMITTED row, which had
already disappeared. Main daemon log at 20:17:08.788 UTC records recovery of
the exact same block cd64efa0972ebed687cb37bc2a69f23c2dbfc8970b87b17a0c458819f052f25e.
This is a peer-recovery contamination of a controlled COMMITTED observation,
not evidence of unsafe Sync publication or an E1-10e runtime regression.
The failed run is retained; it is not relabeled PASS.

Necessary test-infrastructure correction discovered during the planned full
run: execute the unchanged ten W2-4 legs in a same-binary, required-gate child
on the existing manual-GC backend/keyspace. Check all child endpoints report
GC OFF; preserve -run subfilters; certify parent evidence only after complete
child teardown. Keep the shared daemon active, original statuses/HEAD/refs/D/K
assertions and explicit fullyRetired leg intact. Add early cancellation-independent
exact-K teardown so this newly isolated matrix does not leave condemned objects.
No Sync runtime, gate names, required legs or quotas are altered. This ancillary
isolation was not assumed in the frozen plan; the observed failure requires it.
The final standard full run is repeated after this correction.

- `internal/integration/w2_sync_no_putblock_test.go` SHA-256 `e24198e0b9a2ff14909871893c778103f2dc945cc936d884132a6e5cbaed44c5`
- `internal/integration/w24_isolated_evidence_test.go` SHA-256 `fa5ad66605fd4de45e717d1e48d605ba94eacdd0df513df951d46e66b4064b35`


W2-4 isolation verification: final serial race run 30/30 PASS (73.548s);
filtered direct/writerFirst, unavailable backend and active-GC backend all fail
the required child certificate. Integration vet repeated on final test sources
PASS. The final runner is rebuilt with these changes. HTTP backend production
sources are unchanged by this integration-only correction; their verified
runtime binary hashes above remain the final production snapshot.


## Original #264@6f0855984 validation result (superseded review below)

Final standard Docker go-all-test container sesamefs-e110e-audited-go-all
exited 0 against refreshed HTTP backends and final integration sources:
Go short/coverage PASS; required integration 628.223s PASS; API 20/20 suites
PASS; OIDC 25/25 tests PASS (no OIDC skips). New source admission 4/4,
W2-4 10/10 and all prior required matrices pass. All 13 compiled daemon-dependent
controls PASS; no GC-disabled skip. Existing legacy G3 physical-deletion and
optional multi-DC/soak/cgroup exclusions are not newly certified here.

After all tests, check-test-cleanup.sh reports clean on standard and manual
backends: no active/deleted test orgs, owned test libraries or test groups.
Both default organizations remain free/hard; the unchanged hard/free profile
still limits libraries to three. No quota uplift, shared GC shutdown or volume
reset occurred. Exact-K teardown assertions passed in every certified leg;
legacy metadata-cleanup limitations remain as stated above.

Final Go format/whitespace checks PASS; host/runner/final image source hashes
agree, frozen plan prefix unchanged. Final manual diff/source audit found no
unresolved introduced P0/P1/P2 in this scoped correction. The known broader
W2-10 P1, retained-reference TOCTOU, other resurrection handlers/directories,
Phase5/6, R31 and E1/X1 remain OPEN. No broad closure or activation is inferred.
Evidence logs retained outside Git under $TEMP/sesamefs-e110e-*.


## Cross-audit correction / final scope

Reviewed report against #264@6f0855984 and main@5071d170. Three P2 findings
are supported. The productive RED and source-admission policy remain valid;
no own up:/pub:/repair or broader resurrection protocol is added.

1. Config permits database.consistency=ONE, while permanent block references
   are inserted at LOCAL_QUORUM (db.BlockReferenceWriteConsistency). The new
   exact historical fs: read previously inherited the session and could miss
   a valid quorum-written reference. It now explicitly pins gocql.LocalQuorum.
   The source contract inspects that actual SELECT->Scan chain and rejects
   omission, ONE, or a later ONE override. This is a local quorum intersection
   guarantee, not a new SERIAL/EACH_QUORUM or multi-DC claim.
2. Updated the live W2-10 row/index, R3 resurrection summary and E1 subsequent
   dispositions, rather than only appending new status. They now distinguish
   executed COMMITTED/TERMINAL RED + measured RevertDirents file admission
   CLOSED-FIX from continuous liveness, directory/batch, other-handler,
   retention/Phase5/6, R31 and overall W2-10 OPEN. Historical dated retained-
   history evidence remains unchanged; its lack of retirement does not describe
   the later unpublished-source schedule.
3. Extracted preexisting W2-4 controlled-COMMITTED isolation into independent
   [PR #265](https://github.com/Sesame-Disk/sesamefs/pull/265), base main.
   #264 is stacked on codex/w24-controlled-committed-isolation; its diff excludes
   w24_isolated_evidence_test.go, w2_sync_no_putblock_test.go and W24 URL wiring.
   Merge #265 first, then retarget/rebase #264 onto main. No automatic merge.
   Original full-run discovery/combined validation above remains historical
   evidence, not a claim that W2-4 implementation belongs to this scoped PR.

### Preexisting source-supported follow-ups, not fixes in this PR

- Other resurrection handlers/directory shapes still need their own unpublished
  source characterization; this is already in the OPEN W2-10 disposition.
- RevertDirents parent traversal errors fall back to root; replacing by basename
  there can overwrite a different root entry. Dedicated path/overwrite test and
  fix belong to a separate GENERAL P1 follow-up.
- RestoreTrashItem/RevertDirents request coarse PermissionRW. Custom flags map
  upload OR modify OR delete to RW; these handlers do not additionally enforce
  granular modify. Register the AUTH P1 follow-up separately; no router/custom-
  share exploit test is claimed by the owner-context E1-10e fixture.
- After successful HEAD, RevertDirents only logs counter-adjustment failure then
  reports success. The multi-scope loop may have already updated earlier scopes.
  This source-supported GENERAL quota P2 needs separate failure/reconciliation
  evidence and fix; no counter semantics are changed here.
- Preexisting cross-repo characterization wording drift is independent of the
  new W2-10 disposition and remains a documentation follow-up.

Updated-source tests and final standard Docker go-all-test are recorded below
before ready status. Original frozen plan prefix remains byte-for-byte intact.

### Updated-source final validation and scoped audit

- New reference read contract: PASS on actual production source, plus omission,
  explicit ONE and later ONE override mutations all rejected. Pinning is proven
  by the source contract; this RF=1 fixture does not claim a three-replica stale
  read reproduction or multi-DC evidence.
- Updated E1-10e matrix: 20/20 named legs across five race repetitions PASS,
  134.913s. All original rejection and published-history assertions retained.
- Independent #265 source (main production without resurrection admission):
  W2-4 race 30/30 PASS, 76.527s; filtered, unavailable and GC-active required
  child controls exit 1. This isolation implementation is excluded from #264.
- Normal and integration go vet PASS; PC-0, PC-D1B4, identity-writer and permanent-
  reference writer consistency source contracts PASS. Formatting/whitespace PASS.
- FINAL standard Docker go-all-test exit 0: integration 829.290s, API 20/20 suites,
  OIDC 25/25 tests. Includes the complete required E1-10e and W2-4 matrices.
  All 13 compiled daemon-dependent controls PASS; no GC-disabled skips. Existing
  legacy physical-delete skips and optional multi-DC/soak/cgroup suites are not
  certified by this run.
- Rebuilt four backend images; all running binaries share SHA-256
  cc7a7c6de4649a413342b90b4dc6f75596048fa29686e7a14df4132b50ca62a5.
  Admission source and source-contract test hashes match the immutable test
  container. Standard development GC behavior remains unchanged.
- check-test-cleanup.sh: CLEAN on main and manual-GC backends; no active/deleted
  test organizations, test libraries or groups remain. Both default orgs remain
  free/hard; the configured free-plan library limit is unchanged. Exact-K
  cleanup assertions passed in the named legs; no quota increase or volume reset.
- Preserved original frozen plan prefix byte-for-byte. The live summaries now
  distinguish scoped source-admission CLOSED-FIX from broader OPEN work.

Scoped verdict: all three supported THIS-PR P2 are corrected; no unresolved
introduced P0/P1/P2 found. Merge prerequisite #265 first, then retarget/rebase
#264 onto main. This is not closure of continuous liveness, other handlers or
shapes, W2-10 overall, E1, X1, R31, or Phase5/6. Production GC remains OFF.

Logs on this workstation (TEMP, not tracked):
sesamefs-264-review-core-race.log, sesamefs-264-review-w24-independent-race.log,
sesamefs-264-review-w24-{filtered,active-gc,unavailable}.log,
sesamefs-264-review-{vet,contracts,cleanup,quota}.log,
sesamefs-264-reviewed-go-all.log.

### Prerequisite merged / main retarget

PR #265 merged at main@79c5e2d2d74ec4775f834f87100cf5c1a1c92f15.
Rebased the three scoped #264 commits onto that main and retargeted #264 to
main. The rebased tree matched validated b18d3c980 byte-for-byte in Git before
this documentation-only update. No runtime/test changes or new validation
claims; the final Docker go-all-test and cleanup results above still apply.
W2-4 files and URL wiring are excluded from the final #264 diff. The earlier
stacked merge instructions are superseded by this completed prerequisite.

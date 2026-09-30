# Greenfield W2-0 reconciliation audit

## Premise and scope

**Premise: CONFIRMED.** The pre-existing greenfield scope is explicit in
[DEPLOY](./DEPLOY.md#storage-namespace-contract), the
[metadata-identity authority deployment contract](./PC-D1B-METADATA-IDENTITY-AUTHORITY.md#deployment-contract-greenfield)
and [X1 §6](./X1-CRITICAL-PATH.md#6-pre-gc-list-a1-prerequisites)
(Technical Debt #24, no pre-migration-017 candidates on the empty server).
The [normative v1 contract](./X1-CRITICAL-PATH.md#first-production-deployment-contract)
now states compatible audited components before first traffic and no inherited
production data, historical attempts, old processes or old in-flight requests.
This is an operational obligation; no automatic version enforcement is claimed.

Final audit baseline: merged #241, origin/main@302d3418e6596774d48eb7ac98b6be170803ab83.
#240 remains in progress and is outside this PR. The updated work order opens
this docs-only reconciliation first; no dependency on its merge remains.

## Current-version continuity argument

The argument concerns the covered F1 Office, F2 UploadFile, F3 SessionUpload /
BorrowedFS and PutBlock-provenanced direct / merge Sync chains. Unadopted
funnels and settlement/liveness residuals retain their separate W2 rows.

1. A writer can lose temporary pins before repair acquisition. It queues a
   durable repair before its final validation of the captured exact physical
   placement. If GC already won destructive authority or retired that P,
   final validation rejects publication. Sync does not re-scope captured
   provenance after queueing. A late repair cannot revoke COMMITTED D.
2. After successful repair acquisition, TTL expiry cannot remove that row.
   Acquisition uses LOCAL_QUORUM; destructive repair scans use EACH_QUORUM
   across every organization bucket and all pages. Read errors are Unknown.
   A pre-D repair-only result releases the exact claim and preserves pending
   candidate/discovery/queue work, without destructive handoff.
3. HEAD timeout or writer death does not establish cleanup authority.
   UNKNOWN and definitely-not-reachable settlement outcomes retain repair;
   an advisory lease expiry does not authorize its deletion.
4. Reachable repair settlement first promotes permanent fs references before
   deleting repair. Destructive absence rechecks real refs after the negative
   repair scan, covering that handoff. R31's separate convergence/discovery
   and renewal/zero-ref contracts remain unclosed, rather than being inferred
   from this pre-HEAD proof.

Source reviewed: queuePendingPublishedFileRepairs /
queuePublishedBlockReferenceRepair, settlePublishedBlockReferenceRepair and
the productive sweep in
[publish_repair.go](../internal/api/v2/publish_repair.go);
finalizeStoredUploadMetadataOnce and CreateFile in
[files.go](../internal/api/v2/files.go);
queueSyncCommitBlockReferenceRepairsFn, captured exact-P revalidation in both
HEAD promotion paths and conservative ambiguous-outcome retention in
[sync.go](../internal/api/sync.go);
publicationLivenessBeforeDestruction and
BlockPublicationLivenessGlobal in
[publication_liveness.go](../internal/db/publication_liveness.go);
the pre-handoff liveness path in [worker.go](../internal/gc/worker.go).

No independent current-version W2-0 residual was identified within this scope.
Disposition: **CLOSED-EVIDENCE**, based on this ordering and existing evidence,
not on GC disablement or failure to reproduce an imagined race.

## Evidence and individual states

The [#239 proof](./W2-0-PUBLICATION-CONTINUITY.md) and
[#241 evidence](./W2-0-WIRE-CRASH-ROLLOUT.md) remain intact: native Cassandra
wire ambiguity, actual SIGKILL, independent recovery, real three-DC visibility,
productive worker execution, both mixed-version counterexamples and mutation
controls. No test, harness or recorded baseline result was changed.

Both pre-#239 mixed combinations still require D(P)+HEAD. They are a real
**P1 FOLLOW-UP / GENERAL** limitation, registered separately as
ISSUE-W2-INCOMPATIBLE-MIXED-ROLLOUT-01. They do not block v1 X1, PRE-GC or A1;
supporting such a later active-GC rolling upgrade needs its own compatibility /
drain contract and evidence.

| Row | Re-audit outcome | Actual remaining dependency |
|---|---|---|
| W2-0 | CLOSED-EVIDENCE for the covered current-version mechanism | None independent in this scope; R31 and unadopted funnels retain their own rows |
| W2-1 | OPEN; SessionUpload wire/SIGKILL/recovery supports covered pre-HEAD continuity | R31 W2-11..14: renewal-after-classify, known-loser durability, discovery bound, zero-ref transition |
| W2-2 | OPEN; BorrowedFS retains own repair after foreign ref removal | Same separately tracked R31 residuals |
| W2-6a | OPEN; materialized exact-P plus Office wire/SIGKILL/after-HEAD recovery | Same separately tracked post-HEAD R31 residuals |
| W2-6 | Historical CLOSED-FIX pre-HEAD retained | Post-HEAD R31 retained |
| Other W2 rows | Unchanged | Existing concrete gaps; no automatic closure |

## Documentary drift review

The repository search covered mixed-version/deployment/rollout, rollout
exclusion, pre-#239, old writer/GC, legacy and W2-0 OPEN wording.

Current contradictions corrected: X1, R3, OPEN-WORK-INDEX and CURRENT_WORK
summaries; X1 W2-0/1/2/6a exit states; the shared pin-expiry issue and PC-0
funnel-gap status in KNOWN_ISSUES and the active OPEN-WORK-INDEX row; the
continuity status comment in internal/db/block_references.go. #241's complete
original snapshot is preserved under a dated superseding reconciliation note.
DEPLOY now states the first-production contract explicitly.

Historical snapshots retained with explicit supersession links: #238/#239
entries, W2-0-PUBLICATION-CONTINUITY, W2-6A-CREATEFILE-EXACT-P,
PUBLICATION-PROTOCOL-CHARACTERIZATION tables and CHANGELOG. R3's old inventory
classifications are marked historical; today's exit states belong to X1.

Other matches are not this skew premise: migration 011's historical upgrade
procedure is already expressly inapplicable to greenfield; legacy locators,
single-region deployment terminology, schema compatibility and old witness
writers describe different contracts. Existing topology/authority guards
and genuine current-version PRE-GC findings are preserved.

Runtime / schema / CQL / guards / machinery changes: **NONE**.
All changes are documentation/comments only. The critical path remains
W2 remaining -> G4 -> G5 -> E1 -> X1 CLOSED -> PRE-GC -> A1 -> GC ON.
G4 is still blocked by open W2 rows. GC remains disabled.


## Final validation

- Remote main was fetched and matches the audited baseline.
- All 12 other W2 exit rows are byte-for-byte unchanged against that baseline.
- Current-version and three-DC evidence descriptions, both mixed-version
  counterexample definitions and reproduction/results sections are unchanged.
- The complete diff contains Markdown and one comment-only Go change; no
  executable code, tests, runners, schemas, migrations or configuration changed.
- Relative document links resolve and newly introduced section links were
  checked against their target headings. Whitespace checks pass.
- Repository-wide documentary drift was reviewed; retained old W2-0 states
  are explicitly historical/superseded. W2-1/2/6a remain OPEN through R31,
  and the full W2, G4/G5/E1 and PRE-GC/A1 gates remain in force.

No Go tests were rerun for this documentation-only change. Existing #239/#241
test results remain evidence from those PRs, not new executions claimed here.


## Cross-audit corrections (2026-09-30)

Two P2 findings were confirmed and corrected: live documentary reconciliation
(the active PC-0 index row and the productive source comment) and historical
evidence provenance (the retro-edited #241 verdict). The original #241 snapshot
is now preserved in full under an explicitly historical disposition, with a
dated note linking current status to this audit and X1. No W2 closure decision
or severity was changed by these corrections.

Validation now includes the source comments in the repository-wide drift
search. The sole Go diff changes comments only; executable lines match the
audited main baseline. Original #241 disposition/body/results match that
baseline in full apart from the explicit historical section heading.

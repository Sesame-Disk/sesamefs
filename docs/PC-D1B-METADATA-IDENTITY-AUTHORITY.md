# PC-D1B Metadata Identity Authority Decision

**Status as of 2026-09-22:** DECIDED; PR #231 wires the merged primitive into production writers and deleters, and PR #228 consumes those claims in the cold-path certifier. Mapping authority/promotion remains separate coverage work. This document owns
the reasoning, the rejected alternatives and the required evidence. The
current status of the finding lives in
[KNOWN_ISSUES.md](./KNOWN_ISSUES.md) and is deliberately not restated here.
**Issue:** `ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01`
**Decision branch:** `docs/pc-d1b-metadata-identity-authority-decision` (PR #229)
**Wiring branch:** `codex/pcd1b-identity-authority-wiring` (PR #231)
**Audited implementation:** PR #228, `feat/pc-d1b1-certified-baseline-certifier`

**Decision submitted for review:** require durable identity provenance for
whatever the certifier witnesses, and reject a certifier-local consistency
read as sufficient authority. An explicit, fenced, source-validated cutover is
the only admissible route to making an unproven identity authoritative, and
under the greenfield contract its one live form is the per-identity promotion
of a mapping. It is not a precondition for the certifier being correct; see
[Coverage versus minimum correctness](#coverage-versus-minimum-correctness).

**Scope:** the <code>commits</code> / <code>fs_objects</code>
metadata-identity authority contract consumed by the PC-D1B.1 certifier implemented in PR #228,
plus the logical-to-canonical <code>block_id_mappings</code> resolution that the
same proof depends on. The blocker is not about age: only rows written through
the durable identity-authority protocol have proven identity. PR #231 wires
supported writers and deleters through that protocol, while preexisting rows
without a matching claim and divergent or partial projections still fail closed.

**Baseline:** this design branch starts at <code>main</code>
<code>a09650b7a226</code>. The audited PR #228 HEAD is
<code>71ce28fd9</code>. Its certifier sources
(<code>internal/db/library_continuity_certifier.go</code> and the
<code>library_continuity_certifier_*</code> files) exist on that branch only;
a reader on the <code>main</code> baseline will not find them.

**Runtime status:** PR #230 added the schema and authority primitive. PR #231 added the scoped writer/deleter wiring and no-bypass fence. PR #228 adds read-only claim consumption to the certifier; mapping promotion and GC activation remain separate.

## PR #228 implementation closure (2026-09-22)

PR #228 implements the consumer side of this decision. It reconstructs strict
presence-aware source projections for the captured `HEAD -> root_fs_id` commit
and every reachable fs_object, verifies each projection against the durable
identity claim, and revalidates the claims/source projections before witness
settlement. The certifier never authorizes metadata or creates claims.

For paired files, canonical SHA-256 dependencies come directly from the
claim-bound canonical list. A compatibility `block_id_mappings` row may agree
but cannot override it; disagreement fails as `identity_conflict`. A SHA-1-only
identity whose dependency requires an unauthoritative mapping remains
`identity_unproven` and cannot reach physical/liveness work. Missing, partial,
conflicting, or unavailable identity proof creates no witness; unavailable
SERIAL authority returns UNKNOWN.

M14a bypasses reachable fs_object identity verification and M14b bypasses
commit H-to-R identity verification. M15a weakens SHA-1-only
`identity_unproven`; M15b permits a paired mapping to disagree. The 15 frozen
M1-M15 contracts execute as 17 targeted mutation legs, while M16/M17 retain
their claim-lifecycle meaning. Root and directory-entry fs_ids are consumed byte-for-byte or rejected as `malformed_tree`; the Cassandra zero-block file form is accepted only after verifying its exact durable identity claim. Seafile's `EMPTY_SHA1` is never uploaded and has no fs_objects row; when a verified commit or directory entry binds it, the certifier treats that subtree as empty. The isolated 3-DC certifier harness also covers
complete fs_object A/B and commit H-to-R1/R2 divergence, unavailable global
SERIAL identity authority, partial rows, exact-P bytes, EACH_QUORUM liveness, GC
authority, moving HEAD, and ambiguous witness settlement. These controls close the certifier correctness
blocker. They do not add mapping authority/M18-M19, a lifecycle fence, a
productive consumer, PC-2, historical backfill, or GC activation;
`GC_ENABLED=false` remains mandatory.

This is an addendum to the inherited-continuity decision in
[PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md](./PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md).
It does not reopen the fixes from PR #208: those fixes narrow the affected
current write paths, but do not retroactively establish provenance for stored
rows or create one authority protocol shared by every writer.

## PR #231 wiring status

The production gateway now owns semantic `commits` and `fs_objects` CQL. It
accepts typed projections, binds the exact V1 digest, pins the claim to global
`SERIAL`, and returns an opaque gateway capability only for `Established` or exact
`Idempotent` outcomes. Commit retries recover `claim.created_at` before digest
comparison, so the same canonical millisecond is used in the claim, digest and
source row. Existing source rows are read and compared against the claim; a
divergent complete row or partial semantic row fails closed. Pure metadata-only
placeholders may be completed.

The file layout is explicit: SHA-1-only rows store logical ids in `block_ids`
and leave `seafile_block_ids_sha1` null; paired rows store canonical SHA-256 in
`block_ids` and logical SHA-1 in `seafile_block_ids_sha1`. Sync accepts an
authoritative paired row when its logical list matches and refuses to replace it
with a SHA-1-only projection. Individual deletes verify the existing claim and
never delete it; whole unpublished-library rollback remains authorized by its
existing HEAD rollback protocol. The certification-window delete race, claim
retirement, mapping authority and all certifier behavior remain separate work.

The fence is mechanical: semantic source statements are confined to the gateway,
raw claim primitives have no production caller outside the gateway, dynamic CQL
seams are rejected in reviewed writers, and only the exact
`obj_name`/`full_path`/`mtime` display-only update shape remains outside.


## PR #231 re-audit closure (2026-09-22)

The fs_objects source readers share one NULL-presence contract. Typed nil Cassandra LIST values are absent, while non-nil empty lists are explicit. Nullable scalar readers preserve NULL separately from explicit zero and empty values, so a directory with size_bytes=NULL is valid while an explicit size_bytes=0 conflicts with a directory projection. Required commit fields such as description preserve the same distinction; parent_id alone intentionally canonicalizes NULL and empty to the same retry identity. Metadata-only rows are recognized centrally as placeholders. For a zero-block file, whose nullable lists may both arrive as typed nil, verification and deletion require its durable identity claim before proceeding.

The AST guard now rejects any production access to a capability's projection outside identity_gateway.go. Dynamic identity-query coverage includes concatenated fragments, strings.Join over a literal slice or local slice binding, and a local helper that returns identity CQL; mutation cases B22-B24 prove the added access and query seams are detected. The production writer/deleter inventory remains the audited boundary for current repository code.

Real-Cassandra evidence includes exact SHA1-only RecvFS replay, metadata-placeholder completion, directory RecvFS create and exact retry, directory deletion with claim retention, and gateway deletion with claim retention for directories, SHA1-only files and zero-block files. The final Docker go-integration-test profile passed in 313.641 seconds, go test ./... -short -cover passed, and the mutation runner passed all M16/M17 and B1-B24 expected-red cases. This standard local profile skips multi-DC cases that require dedicated host variables; it does not claim a new 3-DC run. The previous isolated multi-DC evidence remains separately recorded.
## PR #231 measured claim-cost characterization

The gateway cost is characterized per semantic identity, rather than inferred
from a count of call sites. The isolated 3-DC integration test
`TestIdentityAuthorityGatewayClaimCostCharacterization3DC` attaches the
Cassandra query and batch observers to a real keyspace session and records the
new, retry and conflict commit paths plus a new paired `fs_object` path, an exact `fs_object` retry and the mixed-funnel SHA-1-only compatibility path. The
measured core shape is:

| Gateway operation (one identity) | SERIAL reads | Global SERIAL LWTs | Ordinary source reads | Ordinary source writes | Ordering |
|---|---:|---:|---:|---:|---|
| New commit (PutCommit/initial-commit commit leg) | 1 | 1 | 1 | 1 LoggedBatch | sequential |
| Exact commit retry (PutCommit/auto-merge retry leg) | 1 | 0 | 1 | 1 LoggedBatch | sequential |
| Existing conflicting commit | 1 | 0 | 0 | 0 | sequential, fail closed |
| New file fs object (RecvFS/SeafHTTP/v2 object leg) | 0 | 1 | 1 | 1 LoggedBatch | sequential |
| Exact fs_object retry (same projection) | 0 | 1 | 1 | 1 LoggedBatch | sequential, exact claim re-claim |
| Mixed-funnel SHA-1-only to paired compatibility retry | 0 | 2 | 1 | 1 LoggedBatch | sequential, exact SHA-1-only re-claim before source verify |
| New directory or placeholder completion (object leg) | 0 | 1 | 1 | 1 LoggedBatch | sequential |
| Initial commit or auto-merge request | per commit row above; add one row for each object identity in the request | one per new identity | one per identity | one per identity | route remains sequential |
| SeafHTTP single or multiblock request | one row per commit/object identity above | one per new identity | one per identity | one per identity | block publication has no authority LWT per block |

For a new fs_object the gateway goes directly to the claim LWT after projection validation, so there is no claim pre-read; retries verify the existing claim. The first seven rows are the gateway contract; the route rows are compositions
of those measured rows and are kept per identity so a request with several
objects does not hide its multiplier. No path adds a claim LWT for each block.
The observer test is run by the isolated 3-DC validation script and fails if a
new or retry path changes this shape unexpectedly.
## Deployment contract: greenfield

This repository's deployment scope is greenfield and this decision is written
for it. <code>docs/DEPLOY.md</code> states it outright,
<code>docs/ARCHITECTURE.md</code> builds the storage-namespace contract on it,
and the registry records the posture as pre-production with an empty server and
no legacy-data preservation. The invariant this decision assumes is
therefore:

~~~text
SesameFS production is deployed greenfield.

Identity authority, and the migration of every current production writer and
deleter onto it, land before first production traffic.

No pre-authority production metadata has to be preserved, backfilled or
reconstructed.

Brownfield migration is outside the active roadmap unless the deployment
model is deliberately changed later.
~~~

That invariant is an operational obligation, not an automatic fact. It holds
only for a newly created keyspace and new buckets with no rows ever written by
a pre-authority binary; the repository already warns elsewhere that an empty
dashboard and a zero queue do not by themselves establish it. If a deployment
cannot assert it, the brownfield material in this document stops being
rationale and becomes required work.

What greenfield does **not** remove is the need to acquire authority for
identities the authority-aware system itself creates unproven. It removes the
history, not the protocol: see
[Coverage](#coverage-versus-minimum-correctness).

## Decision

The certifier may witness HEAD <code>H</code> only when the exact
commit-to-root mapping and every reachable filesystem-object identity are
backed by durable, immutable authority evidence, and only when the canonical
physical dependency of every reachable file is fixed by an authoritative
identity rather than by an unproven lookup. A row being present and complete is
not proof that it is the authoritative version of that identity.

The canonical block dependency resolves into two cases, and the distinction
matters because only one of them puts <code>block_id_mappings</code> on the
authority path:

- A file identity that carries an authority-bound canonical SHA-256 list
  already fixes its dependency. A mapping consulted for compatibility must
  agree with that list, but it is not an independent authority for the
  dependency set.
- A file identity that carries logical Seafile SHA-1 ids only does not fix its
  dependency: <code>block_id_mappings</code> alone decides which bytes the
  proof is about, so that mapping must carry its own authority evidence.

The authority evidence must bind the source identity key to a versioned digest
of the semantic fields used by tree traversal and dependency resolution. It
must be established by a write-once, globally serialized claim (or an
equivalent protocol with the same no-conflicting-writer guarantee), and all
production writers must honor that claim. *Globally serialized* here means the
protocol's canonical global <code>SERIAL</code> domain, pinned explicitly and
never inherited from a configurable per-DC default; the exact obligation is
stated in [New identities](#new-identities). A side-table marker that old or
unmodified writers can bypass is not sufficient.

What a witness must *mean* is fixed by this decision. What a witness must
*store* is not, and this document deliberately does not freeze it.

The meaning is PC-D1's statement — library X is certified through HEAD H under
contract V — with one added precondition: every identity the certification
traversed was authoritative when it was read, and the certifier revalidated
that authority before settling.

The obvious stronger stored shape is:

~~~text
(library X, HEAD H, captured root R, reachable-tree digest D,
 identity-authority epoch A, continuity-contract version V)
~~~

where <code>D</code> would be a deterministic digest over the authorized
semantic identity of every reachable object and the canonical block identities
used for the physical-liveness proof. That shape is **not adopted here**,
because three questions are open and each one changes what the stored field is
worth:

1. **Incremental semantics of <code>D</code>.** PC-D1 keeps the hot path at
   O(new blocks) instead of O(tree) precisely by not revisiting inherited
   dependencies. A digest over the whole reachable tree has no incremental
   successor unless <code>D</code> is defined as a composable commitment whose
   value for <code>H'</code> is derivable from <code>D</code> plus the
   published delta, with that composition rule itself versioned. Storing a
   non-composable <code>D</code> forces a full re-walk on every advance and
   silently repeals the PC-D1 cost argument.
2. **Meaning of an <code>A</code> bump.** If advancing the identity-authority
   epoch invalidates every existing witness, the bump is a fleet-wide
   re-certification event that needs an operator procedure and a cost bound.
   If it does not invalidate them, <code>A</code> proves nothing at
   revalidation time. One of the two must be chosen explicitly.
3. **Cost on the already-landed primitive.** PC-D1A's HEAD-fenced witness CAS
   and combined HEAD+witness advance are merged. Adding <code>R</code>,
   <code>D</code> or <code>A</code> to the stored witness means new columns
   *and* new <code>IF</code> predicates on both primitives — a change to
   landed authority code, not an additive migration.

Until those are answered, the certifier's obligation is the traversal-time and
revalidation-time gate below, which needs no new witness field. PC-D1A
currently stores the certified HEAD and continuity-contract version; any
additional witness field is a separate, separately audited schema prerequisite
and is out of scope for PR #228.

## Historical verified state before PR #231

The following table records source inspection on the pre-wiring <code>main</code>
baseline and the audited open PR #228 diff. It is historical context, not the
current writer/deleter state after PR #231:

| Area | Current behavior | What it establishes / does not establish |
|---|---|---|
| PC-D1B.1 certifier (#228) | <code>readContinuityCommitRootContext</code> and the tree walker read <code>commits.root_fs_id</code> and <code>fs_objects</code> through ordinary session reads. The baseline witness is keyed by HEAD. | Completeness and a locally observed tree; not a durable proof that the same historical mapping is authoritative in every DC or cannot change later. |
| Sync <code>PutCommit</code> | Uses <code>INSERT ... IF NOT EXISTS</code>, with global <code>SERIAL</code> for the Paxos phase; a conflicting retry is rejected. | First-writer-wins for this endpoint. The client-supplied commit ID is checked against the request path, not recomputed as a digest of the stored root mapping. |
| Sync <code>RecvFS</code> | Verifies SHA-1 over the exact decompressed JSON before parsing. <code>storeSyncFSObject</code> then does a <code>LOCAL_QUORUM</code> read followed by an ordinary <code>LOCAL_QUORUM</code> insert/update, without a per-object Paxos claim. | Validates the received payload on this path and reduces conflicting replay. It is not a global immutable claim shared with every writer, and it does not attest old rows. |
| Other production writers | Ordinary <code>commits</code> / <code>fs_objects</code> inserts remain in initializers and internal paths, including <code>createInitialCommit</code>, sync auto-merge/directory creation, SeafHTTP upload/directory creation, and v2 <code>FSHelper</code> / library creation. | These paths do not all go through the <code>PutCommit</code> or <code>RecvFS</code> authority checks. Their generated IDs may be content- or attempt-derived, but that alone is not a shared cross-DC write-once protocol. |
| <code>block_id_mappings</code> resolution | The certifier's <code>resolveBlockIDs</code> resolves each logical Seafile SHA-1 to a canonical SHA-256 through <code>GetBlockIDMappingContext</code>. When the row also carries a paired internal SHA-256 the resolved value is cross-checked against it; when the row carries SHA-1 ids only, the mapping row is the sole authority for which bytes the file depends on. | The physical-liveness proof inherits this table's provenance. <code>WriteBlockIDMapping</code> and the web-only <code>WriteVerifiedWebBlockMapping</code> are read-before-write followed by a plain <code>INSERT</code> — no LWT, no serial domain — and their contract documents a residual same-key race. Such a row is unproven rather than authoritative; only a SHA-1-only identity needs it promoted, per Scope of mapping authority. |
| Deletion of identity rows | <code>fs_objects</code> rows are deleted in production by library-creation rollback (whole partition) and by GC. <code>commits</code> rows are deleted by those two plus the failed-publish cleanup and two guarded v2 FS-helper discards. <code>cleanupFailedPublishDeleteFSObjectFn</code> exists but has **no production caller**: <code>CleanupFailedPublishArtifacts</code> receives <code>fsIDs</code> and never deletes them. All of these are ordinary deletes that consult no authority. | The writer inventory above is insert-only. A tombstoned identity can be re-created under the same key with different semantic fields, and a witness that already certified it is not invalidated by the delete. Which of these can reach a *currently reachable* identity is a different question, settled under the certification window below. |
| Legacy history | <code>docs/KNOWN_ISSUES.md</code> records that before #208, <code>PutCommit</code> could upsert a reused commit ID and <code>RecvFS</code> trusted the supplied fs ID while upserting object fields. | Current code cannot infer which complete historical row, if any, has trusted provenance merely because that row still exists. |

The canonical schema also keys both <code>commits</code> and
<code>fs_objects</code> with <code>library_id</code> as their partition key.
Any implementation that moves these identities to Paxos must account for the
resulting contention; it should not accidentally turn every filesystem object
in a library into one hot LWT partition.

The completeness checks remain necessary, as do exact physical-byte, liveness,
GC-authority, and HEAD-race checks. This decision adds an independent identity
authority prerequisite; none of those checks substitutes for it.

## Why read-time stabilization is rejected

<code>EACH_QUORUM</code> is a consistency level, not an identity lock or a
durable provenance record. Its quorum-intersection guarantees apply to the
consistency levels and replica topology of the operations in question; they do
not retroactively attest writes made under unknown historical levels, fence a
writer after the read, or prove that no delayed mutation can later arrive.

Apache Cassandra documents ordinary table writes as eventually consistent.
Read repair concerns replicas participating in that read; hints are best
effort. Consequently, even a cross-DC read that returns one complete version
cannot by itself prove that a different historical version is absent from all
replicas or that a later write cannot supersede it. A one-time
<code>EACH_QUORUM</code> read, a complete-tree walk, and the existing M1-M15
mutations do not establish that stronger invariant.

A writer-fenced, all-replica legacy migration may use repair and consistency
checks as *steps* in a cutover protocol. That is materially different from
promoting the result of a certifier-local read to durable identity authority.
The migration must account for all replicas, in-flight writes, pending hints,
and the post-cutover writer boundary; the selected row must also have an
authoritative source, not merely win timestamp reconciliation.

## Frozen identity contract

Authority digests cover the semantic projection, not incidental display or
update metadata:

| Identity | Minimum semantic projection |
|---|---|
| Commit | <code>(library_id, commit_id) -> root_fs_id</code>; also bind <code>parent_id</code> and any other immutable field consumed by history/ancestry readers. <code>root_fs_id</code> is the minimum needed to bind this baseline walk. |
| Directory fs object | <code>(library_id, fs_id)</code>, object type, and exact directory entries consumed by traversal. |
| File fs object | <code>(library_id, fs_id)</code>, object type, size, the exact ordered logical Seafile SHA-1 block ids, **and the exact ordered canonical SHA-256 block ids whenever the row carries them**. Both lists are inputs to the digest. <code>fs_id</code> is derived from the Seafile SHA-1 representation, so two complete rows can agree on <code>fs_id</code>, object type, size and logical list while naming different canonical ids; a digest that omits the canonical list would accept both under one claim. For a logical id whose canonical SHA-256 is not bound in the same row, the mapping identity below enters this file's authority in its place. |
| Logical-to-canonical block mapping | <code>(org_id, representation_id, external_id) -> internal_id</code> in <code>block_id_mappings</code>. Required whenever a file identity names logical SHA-1 ids without a paired canonical SHA-256, because the mapping alone then decides which physical bytes the liveness proof is about. Its authority is independent of the <code>fs_objects</code> row that consumes it: an authoritative file identity resolved through an unproven mapping is unproven. |

"Authority-bound" throughout this document means bound by the projection in
this table. A canonical SHA-256 list that is stored but not covered by the
digest is not authority-bound, and a file identity resting on it is unproven.

<code>obj_name</code>, <code>full_path</code>, and <code>mtime</code> are not
identity inputs for this certifier unless a future semantic reader makes them
so. The digest format itself must be versioned and deterministic. A content
hash is useful evidence only when the exact source bytes and the mapping from
those bytes to the persisted semantic projection are both validated; a
hash-shaped identifier alone is not authority.

The authority state is fail-closed:

| Observation | Certifier result | Side effects |
|---|---|---|
| Durable marker exists, its version and digest match the complete row, and the writer fence is active | Continue to the remaining tree walk and the physical-liveness proof | No witness until every later proof and final HEAD CAS succeeds |
| The marker is absent for a row, a complete row conflicts with the marker, or two complete identities are observed for one key | <code>NOT_CERTIFIED</code> (<code>identity_unproven</code> / <code>identity_conflict</code>) | Do not establish new baseline liveness and do not write a witness |
| Authority read, replica availability, the no-bypass writer/authority check, or marker settlement is unavailable or ambiguous | <code>UNKNOWN</code> | Do not establish new baseline liveness and do not write a witness |
| Row is missing, partial, malformed, or its physical bytes / GC authority fail the existing checks | Existing <code>NOT_CERTIFIED</code> / <code>UNKNOWN</code> contract | Do not write a witness |

All reachable metadata identities must pass this gate before the certifier
starts the physical-liveness handshake, under the two-case dependency rule
stated in the Decision: an authority-bound canonical SHA-256 list fixes the
dependency and any mapping consulted must merely agree with it, while a
SHA-1-only file identity requires an authoritative
<code>block_id_mappings</code> row. Before settling, the certifier must
revalidate the same <code>H</code>, the same captured root <code>R</code>, the
identity authority of everything it traversed, and continuity version
<code>V</code>, in addition to the existing exact-P / GC-authority rules.
Whether any of that is *stored* in the witness is the open question above;
performing the revalidation is required either way. An ambiguous final CAS
continues to require authoritative settlement; it never implies success.

## Coverage versus minimum correctness

Two things are decided here and they have different merge consequences.
Separating them is deliberate.

**Minimum correctness — what the certifier owes.** It must never issue a
witness it cannot back. Absence of authority evidence is
<code>identity_unproven</code>, conflicting complete identities are
<code>identity_conflict</code>, and unavailable or ambiguous authority reads
are <code>UNKNOWN</code>. This is entirely a property of the certifier and its
gate: the correct answer for an unproven identity is to refuse. A certifier
that fails closed on every identity it cannot prove is *correct*; it is merely
not yet *useful* for those identities.

**Coverage — how many libraries can be certified at all.** Coverage is a
separate problem, and under the greenfield contract it has exactly one live
form: identities the authority-aware system itself creates unproven. Concretely,
<code>storeSyncFSObject</code> persists a desktop-sync file object with the
wire SHA-1 list in <code>block_ids</code> and leaves
<code>seafile_block_ids_sha1</code> unset, so a brand-new library can hold a
SHA-1-only identity whose canonical dependency only
<code>block_id_mappings</code> decides. Those mappings are written
read-before-write and start unproven by design, so acquiring their authority on
the cold path is ordinary forward work, not history. Its absence bounds
coverage; it does not make the certifier wrong.

Consequently:

- PR #228 now verifies commit/fs-object claims read-only and fails closed. For a
  SHA-1-only identity, it can detect that dependency resolution requires
  mapping authority, but #230 adds no representation for that authority; the
  certifier must return <code>identity_unproven</code> and write no witness.
  For a paired identity, its claimed canonical SHA-256 list remains authoritative
  and any consulted mapping must agree with it.
- Cold-path mapping promotion is **not** a merge precondition for PR #228. It
  is what a library containing SHA-1-only identities needs before it can be
  certified at all, so it precedes a productive consumer that expects such a
  library to certify.
- A deployment may run with zero promotions performed. Every SHA-1-only
  identity then returns <code>NOT_CERTIFIED</code>/<code>identity_unproven</code>
  and <code>WorkSetScopeNewlyLive</code> stays inadmissible for its library,
  which is the PC-D1 fail-closed default rather than a regression.
- Historical cutover, backfill of pre-authority rows and forensic
  reconstruction are **non-goals** under the greenfield contract. They are
  retained below as rationale and as the protocol a brownfield deployment
  would need, not as stages of this roadmap.

## Selected provenance protocol

### Updated implementation sequence

The policy below is the complete target. PR #230 implements only the
authority-only claim primitive for semantic <code>commits</code> and
<code>fs_objects</code> identities. Its claim key follows
<code>fs_objects</code>' row identity, so one <code>fs_id</code> has one
<code>fs_object</code> claim whether its projection is a file or a directory;
the subtype is bound in the digest.

PR #230 does not add mapping-authority representation. A separate follow-up
must add that representation and the cold-path promotion protocol (M18/M19)
before a mapping can serve as dependency authority. Until then, a reachable
SHA-1-only file whose canonical dependency is resolved solely by
<code>block_id_mappings</code> remains <code>UNPROVEN</code>, and PR #228 must
not issue a baseline witness for a library whose certification walk reaches
such an identity. This is an implementation split; it does not change the
architecture's full target.

### New identities

The complete implementation, across its scoped follow-ups, must provide one
identity-authority protocol used by every path that creates, changes or removes
semantic <code>commits</code> or <code>fs_objects</code> fields, and by the
<code>block_id_mappings</code> promotion path defined under
[Scope of mapping authority](#scope-of-mapping-authority):

1. Canonicalize the semantic projection and calculate its versioned digest.
2. Make a durable, no-TTL per-identity claim in the protocol's **canonical
   global <code>SERIAL</code> domain**, pinned explicitly on the statement.
   This is the domain PC-D1 already fixed for the HEAD protocol under
   "Canonical SERIAL domain prerequisite", exposed in this repository as
   <code>db.LibraryHeadSerialConsistency</code> (<code>gocql.Serial</code>)
   and pinned that way by the landed PC-D1A primitives. The claim must **not**
   inherit the session default or <code>database.serial_consistency</code> /
   <code>CASSANDRA_SERIAL_CONSISTENCY</code>: a supported multi-DC deployment
   may legitimately set those to <code>LOCAL_SERIAL</code>, and a per-DC Paxos
   domain provides no cross-DC no-conflicting-writer guarantee, which is the
   whole point of this decision. Configurable serial consistency remains valid
   for other LWTs and is never a substitute here. A claim that cannot pin
   global <code>SERIAL</code> fails closed. The first claim fixes the digest;
   an identical retry is idempotent and a different digest is a conflict.
3. Materialize or complete a <code>commits</code> or
   <code>fs_objects</code> row only under that claim. For a
   <code>block_id_mappings</code> row that already exists as an unproven
   compatibility write, the corresponding step is the separate promotion
   protocol under Scope of mapping authority, not a fresh materialization. A
   crash or ambiguous claim leaves an unverified/pending identity, never a
   certifiable one. Before HEAD
   publication or baseline certification, verify that the stored row matches
   the claimed digest and is visible in the required multi-DC authority
   domain.
4. Leave no bypass. Every supported production writer, semantic updater and
   deleter of these identities participates in the protocol, and a source or
   contract test must keep a future writer from being added outside it. A claim
   table alone is insufficient if any route can upsert or remove identity
   fields without consulting it. Under the greenfield contract this is a
   property of one release, not a migration: the authority-aware release is
   deployed before first production traffic, so no compatibility with
   pre-authority binaries is required, and no mechanism needs to be introduced
   solely to survive a mixed-version fleet. Where real concurrency still needs
   fencing, an epoch, a generation, a lease or an equivalent protocol are all
   admissible; this decision fixes the property and chooses none of them.

A dedicated authority table whose **partition** key is the full triple
<code>((library_id, identity_kind, identity_id))</code> is a candidate that
keeps LWT scope per identity. The triple must not be split into a
<code>library_id</code> partition with clustering columns: that is the layout
<code>commits</code> and <code>fs_objects</code> already use, and it would
reproduce exactly the one-hot-partition contention warned about above.
<code>block_id_mappings</code> is the existing precedent in this repository,
with the full <code>((org_id, representation_id, external_id))</code> triple as
its partition key. The exact schema and write ordering belong to the
prerequisite implementation PR, but the per-identity isolation, immutability,
no-TTL, global-serial claim, and no-bypass properties are fixed by this
decision.

### Scope of mapping authority

A mapping row is dependency authority only for a SHA-1-only file identity. For
a file whose canonical SHA-256 list is authority-bound by its own digest, the
mapping is a compatibility lookup that must agree, not a second authority. This
decision therefore does **not** require a claim at the moment every mapping is
created.

**Not chosen:** giving every new <code>block_id_mappings</code> row a global
<code>SERIAL</code> claim at write time. That would add a second per-block
Paxos round to the upload hot path, on top of the existing per-block
<code>blocks</code> metadata LWT. <code>docs/WEB-BLOCK-UPLOAD.md</code> records
"No Paxos on the hot path" as a deliberate decision, because per-block Paxos
causes latency, contention and timeouts in multi-DC/multi-node deployments.
Nothing here reverses that, and no follow-up PR may read this document as
silent authorization to do so; reversing it would need its own measured cost
argument.

**Chosen:** a mapping carries authority only when it must serve as dependency
authority. Creation stays exactly as it is today — read-before-write with a
fail-closed remap guard, through <code>WriteBlockIDMapping</code> or the
web-only <code>WriteVerifiedWebBlockMapping</code>, with no per-block Paxos.

A mapping written that way is *unproven*, not authoritative, and an unproven
mapping cannot certify a SHA-1-only identity. Making one authoritative is a
**promotion**.

Because the row already exists and was written ordinarily, promotion inherits
the exact hazard this document rejects everywhere else: a snapshot in which
every reachable replica agrees does not prove that no earlier mutation is still
queued for later delivery. Cassandra stores a hint carrying the original
mutation's timestamp and replays it best-effort afterwards, so a pre-fence
write of <code>K -> B</code> whose timestamp is later than the selected
<code>K -> A</code> can land *after* the claim is committed. The claim would
still name A while the source row resolves to B, and for a SHA-1-only identity
that is the difference between one set of bytes and another. Point-in-time
convergence is therefore not a promotion.

Stability is not the only thing a promotion owes. For a SHA-1-only identity
the mapping row is the sole selector of which bytes the file depends on, so
promoting whatever value the cluster currently agrees on decides the dependency
from the very artifact whose provenance is in question. That is circular, and
it is the same move this document forbids for <code>commits</code> and
<code>fs_objects</code>. Convergence establishes that every replica agrees;
it does not establish what they were entitled to agree on.

A promotion is therefore a **per-identity cutover** and inherits the legacy
cutover's source rule. It must satisfy at least:

1. Fence new writers for the key.
2. Drain or settle its in-flight application writes.
3. Establish independent trusted evidence for the value to be claimed, under
   the rule the legacy cutover already states: “first row read,” majority,
   latest timestamp and blind repair are not authority. A reproducible content
   proof is admissible where the representation allows the external SHA-1 to be
   recomputed over exactly the bytes that identifier is defined on; hashing the
   stored object as-is is not that proof unless the representation makes the
   two identical.
4. If no trusted source establishes the value, stop. The mapping stays
   unproven and its SHA-1-only identity stays <code>identity_unproven</code>.
   A converged cluster is not a substitute for provenance, and a divergent
   history that no source can adjudicate is not promotable.
5. Neutralize every pre-fence mutation that could still be delivered,
   including pending hints and anything repair may yet carry, **or** use an
   equivalent protocol under which no pre-fence mutation can supersede the
   claimed value.
6. Establish the required replica visibility of that value.
7. Write the immutable claim in the canonical global <code>SERIAL</code>
   domain.
8. Release the writer fence, after which only protocol-aware writers resume.

Steps 3-4 are *semantic provenance*: the claimed value is justified. Step 5 is
*temporal authority*: nothing older can come back and displace it. Neither
substitutes for the other, and an implementation may skip neither.
Re-materializing the claimed value under the fence with an ordering that makes
every earlier mutation inert is an admissible way to satisfy step 5. The
mechanisms stay open; the properties do not.

Promotion is therefore a cold-path, per-identity cost paid only where a
SHA-1-only identity actually needs certification, never a hot-path cost paid by
every upload. What the fence itself costs the write path is a property the
implementation PR must measure and state: this decision freezes only that no
per-block Paxos round is added to upload, not that fencing one key is free.

Promotion is **coverage, not minimum correctness**, and under the greenfield
contract it is ordinary forward work rather than history. The authority-aware
system keeps creating unproven mappings on purpose — that is the point of
keeping Paxos off the upload path — and
<code>storeSyncFSObject</code> keeps creating SHA-1-only file identities that
depend on them, so a library created after launch can need a promotion.

PR #228 does not have a mapping-authority representation to read. It must
recognize that a SHA-1-only identity depends on a mapping, classify that
identity as <code>identity_unproven</code>, and write no witness. A later
mapping-authority work item adds the representation and cold-path promotion;
once it exists, the certifier can read that authority state. Whether promotion
is driven on demand or in bulk is an implementation choice.

### Pre-authority rows (non-goal under the greenfield contract)

**This subsection is rationale, not active work.** Under the deployment
contract above there are no pre-authority production rows to rescue, so nothing
here is a stage of the roadmap. It is kept because it states the protocol a
brownfield deployment would need, and because the per-identity promotion above
inherits its source rule.

Absence of a marker means <code>UNPROVEN</code>, not “probably old but safe.”
Were such rows in scope, they could be marked authoritative only by an explicit
cutover that:

1. Fences all identity writers and drains in-flight requests before examining
   the affected identities; no pre-cutover writer may resume after release.
2. Establishes convergence for every relevant replica, accounting for
   pending hints and repair. A quorum response alone is not the all-replica
   cutover proof.
3. Validates the selected semantic projection against a trusted source.
   Divergent complete rows are not resolved by “first row read,” majority,
   latest timestamp, or a blind repair. If no trusted source can establish
   which identity is correct, that identity remains unproven and its library
   cannot receive a baseline witness.
4. Writes an immutable, versioned cutover marker under the same authority
   protocol while the writer fence remains held, then permits only
   protocol-aware writers to resume.

For an fs object, recomputing the content hash may participate only when the
exact canonical source bytes can be recovered and the persisted projection is
shown to represent them. For a commit, the current row does not itself prove
that the client-supplied commit ID cryptographically commits to its
<code>root_fs_id</code>; a trusted historical source or an explicit reviewed
reconstruction is required. No backfill may mint provenance from an arbitrary
complete row.

A brownfield cutover runbook would have to prove the writer fence, replica
convergence, drained mutations/hints, and source validation for the deployed
Cassandra topology; absent that proof, rows without authority markers stay
ineligible. An <code>EACH_QUORUM</code> read or a routine repair does not
satisfy it. None of this is scheduled work here: under the greenfield contract
the roadmap never reaches it.

### Deletion and re-creation

An identity can also leave. Production deletes <code>fs_objects</code> rows in
library-creation rollback and in GC, and <code>commits</code> rows in those two
plus the failed-publish cleanup and two guarded v2 FS-helper discards, and
Cassandra allows the same key to be written again afterwards. A protocol
that fences only the creation of new rows does not cover this, so the
following is part of the decision:

1. **A claim is not garbage of its row.** Deleting a <code>commits</code> /
   <code>fs_objects</code> row does not delete, reset, expire or weaken its
   authority claim. No cleanup, rollback, repair or GC path may remove a claim
   as a side effect of removing the row it describes, and a claim carries no
   TTL that could make it lapse into absence.
2. **Re-creating a deleted key is a write under the existing claim**, never a
   fresh first claim. It succeeds only when its digest equals the claimed
   digest; a different digest is <code>identity_conflict</code>, exactly as if
   the row had never been deleted. A surviving claim with an absent row is
   <code>identity_unproven</code> for certification, never an invitation to
   re-mint provenance from whatever is written next.
3. **Retiring a claim is its own fenced protocol** with its own evidence, and
   is out of scope here. Until it exists, no path may retire a claim, and the
   correct behavior for an identity that will never return is to leave the
   claim in place. The consequence is accepted debt, registered as
   <code>ISSUE-PCD1B-AUTHORITY-CLAIM-RETIREMENT-01</code>: claims accumulate
   for failed initializations, losing commits, deleted identities and deleted
   libraries. This is a greenfield cost too, not historical residue, and
   bounding it belongs to the retirement protocol rather than to this
   decision.
4. **No covered identity may vanish inside the certification window.** The
   dangerous case is not a witness that goes stale after settlement; it is a
   witness born false. Rule 1 makes the claim survive a delete, which is
   correct for ABA, but it also means nothing about the claim changes when the
   row disappears. Meanwhile the landed PC-D1A witness CAS predicates only
   <code>head_commit_id</code> and <code>deleted_at</code> on the
   <code>libraries</code> row, so it still applies:

   ~~~text
   T1  final identity revalidation  -> every covered identity is authoritative
   T2  DELETE fs_objects(F)         -> F is gone; its claim survives by rule 1
   T3  witness CAS
         IF head_commit_id = H AND deleted_at = null   -> APPLIED
   ~~~

   The certifier has then issued a witness it cannot back, which is exactly
   what this document forbids. Re-reading <code>F</code> immediately before the
   CAS does not close the hole, because that read carries the same TOCTOU
   window. The invariant is therefore part of this decision:

   > Between the certifier's final identity validation and the settlement of
   > its witness, no identity covered by that certification may disappear or
   > change without either (a) making the witness CAS fail, or (b) atomically
   > invalidating the authority state that witness validity is checked
   > against.

   The mechanism is deliberately not chosen here: a generation or epoch carried
   in the CAS predicate, a delete fence held across the window, frontier
   invalidation on identity removal, or an equivalent protocol are all
   admissible.

   Its **gating** is scoped to what is reachable today. Each current production
   delete was checked against the certified live tree:

   - *Library-creation rollback* — no. It tears down a library that never
     published a HEAD, and it removes the canonical row the witness CAS
     predicates on, so that CAS cannot apply.
   - *Failed-publish cleanup in publish repair* — no. It deletes the failed
     attempt's commit and does not delete <code>fs_objects</code> at all,
     because <code>CleanupFailedPublishArtifacts</code> ignores the
     <code>fsIDs</code> it is handed.
   - *v2 FS-helper initial-commit discards* — no. One runs only where the CAS
     definitively did not apply, so the id is attempt-unique and never became
     HEAD; <code>DiscardLosingInitialCommit</code> refuses outright when the id
     equals the winning HEAD.
   - *GC expired-version cascade* — yes, and it is the only one.
     <code>ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01</code> is a registered
     P0-latent PRE-GC bug, and its trigger is dormant under
     <code>GC_ENABLED=false</code>.

   The invariant is therefore **mandatory before destructive GC activation and
   before the first productive witness consumer**, and on this evidence it is
   not a merge precondition for PR #228: that PR activates no GC, changes no
   Phase 5 behavior, and has no consumer that could read a false witness. It
   becomes a #228 blocker the moment any non-GC path is shown to delete an
   identity inside the current reachable tree. Fixing the Phase 5 cascade is
   separate work and is not in scope here.
5. **After settlement a witness is still not self-maintaining.** If a certified
   identity is deleted once the witness exists, the witness is stale in fact
   while still matching HEAD and contract version. Any consumer must treat
   deletion inside the certified tree as witness-invalidating; naming and
   implementing that consumer-side mechanism remains a prerequisite for the
   first productive consumer, distinct from the in-window fence above.

## Certifier protocol and required evidence

The admissible order is:

~~~text
capture HEAD H
  -> prove authoritative commit identity H -> root R
  -> walk the complete tree and prove every fs-object identity
  -> fix each file's canonical dependency: from its authority-bound canonical
     ids, or from an authoritative mapping when it carries logical ids only
  -> run the existing exact-P / physical-bytes / liveness / GC handshake
  -> revalidate H, R, every traversed identity's authority, and GC authority
  -> write and settle the witness
~~~

If a stored <code>D</code> or authority epoch <code>A</code> is adopted later,
it is computed after the tree walk and revalidated in the same step; the order
above does not change.

The implementation must extend the existing M1-M15 mutation suite with at
least M14-M19, and they do not all land in the same PR. M16 and M17 are
properties of the authority primitive itself — claim lifecycle across a
delete, and what the digest binds — so they land with it. M14 and M15 are
certifier-gate properties and land with PR #228. M18 and M19 belong to the
mapping promotion path.

Each mutation's red assertion is stated at the layer that owns it, so no PR
depends on a consumer that has not landed yet. M16 and M17 are provable against
the claim and the digest alone, with no certifier and no witness in the picture;
PR #228 then proves that its certifier consumes those outcomes correctly, which
is integration evidence rather than a repeat of the primitive's own contract:

| Mutation | Required red assertion |
|---|---|
| M14 removes or weakens the identity-authority marker/digest check and accepts a complete row based only on the ordinary read | The targeted contract test turns RED with a complete divergent <code>fs_objects</code> identity and with a divergent <code>H -> R</code> commit mapping; the certifier must otherwise refuse to witness either case. |
| M15 accepts a logical-to-canonical block mapping without proving its authority, or trusts the mapping when the row also carries a paired canonical SHA-256 that disagrees | The targeted contract test turns RED for an unproven mapping on a SHA-1-only file identity and for a mapping that resolves to a different canonical id than the row names; neither may reach the physical-liveness handshake. |
| M16 lets a claim be removed, reset or bypassed when its source row is deleted, or lets a re-created key take a fresh first claim | At the claim layer, with no certifier involved: the targeted contract test turns RED when a claim disappears or resets after its source row is deleted, when a cleanup/rollback/GC path clears it, and when a re-created key with a different digest is admitted instead of being refused as a conflict. |
| M17 drops the canonical SHA-256 block-id list from the file authority digest, or compares only the logical SHA-1 list | At the digest layer, with no certifier involved: for two complete rows agreeing on <code>fs_id</code>, object type, size and logical SHA-1 list but naming different canonical SHA-256 block ids, the targeted contract test turns RED unless the two produce different authority digests and the second cannot satisfy or reuse the first row's claim. |
| M18 promotes a mapping on a point-in-time convergence check alone, skipping the neutralization of pre-fence mutations | The targeted contract test turns RED when a conflicting pre-fence write for the same <code>(org_id, representation_id, external_id)</code> is delivered after the claim settles and the resolved value changes, and when the certifier accepts a promoted mapping whose source row no longer resolves to the claimed <code>internal_id</code>. |
| M19 promotes the currently converged mapping row without independent trusted-source validation | The targeted contract test turns RED when a SHA-1-only identity whose mapping converged on B is promoted although trusted evidence establishes A (it must be <code>identity_conflict</code>), and when a mapping converged on A is promoted with no independent evidence at all (it must stay <code>identity_unproven</code>). Neither case may do liveness work or write a witness. |

Isolated real 3-DC evidence must then prove:

- Same <code>(library_id, fs_id)</code> with complete semantic identity A in
  one DC and B in another is <code>NOT_CERTIFIED/identity_conflict</code> or
  <code>NOT_CERTIFIED/identity_unproven</code>, with no new liveness work and
  no witness.
- Same <code>(library_id, commit_id=H)</code> with <code>H -> R1</code> in one
  DC and <code>H -> R2</code> in another is rejected while canonical library
  HEAD remains H.
- A concurrent or stale protocol-aware writer, or a mutation already in
  flight, cannot change a semantic identity after its marker is established;
  if the test can bypass the fence, no witness may remain usable. This leg is
  about live concurrency between supported writers. It is **not** about
  coexisting with a pre-authority binary: the greenfield contract puts the
  authority-aware release before first production traffic, so no evidence of
  mixed-version compatibility is required or wanted.
- Two complete <code>fs_objects</code> rows agreeing on <code>fs_id</code>,
  object type, size and logical SHA-1 list but naming different canonical
  SHA-256 block ids resolve to different authority digests in every DC, and the
  second cannot satisfy the first's claim. That much is the primitive's own
  leg. Once the certifier exists, the same pair must classify as
  <code>NOT_CERTIFIED</code>/<code>identity_conflict</code> with no liveness
  work and no witness, which is #228's integration leg.
- A SHA-1-only file identity is refused when its
  <code>block_id_mappings</code> row has no authority evidence, and a file
  identity with an authority-bound canonical list is refused when a consulted
  mapping disagrees with it — including when two DCs resolve the same
  <code>(org_id, representation_id, external_id)</code> to different canonical
  ids. Neither case does new liveness work and neither writes a witness.
- Deleting a certified identity row and re-creating the same key with a
  different semantic projection is refused; the claim survives the delete, and
  no path clears it as a side effect.
- A delete of a covered identity injected between the final identity
  revalidation and the witness settlement does not produce a settled witness:
  either the witness CAS fails or the authority state checked by witness
  validity is invalidated in the same step. A test that can settle a witness
  in that window is a failure of the gate, not of the consumer.
- A conflicting pre-fence mapping write for a promoted key, held back and
  delivered only after the claim settles (a replayed hint, or a write to a
  replica that was unreachable during the fence), does not change the
  authoritative value. A promotion a test can defeat this way is not a
  promotion.
- A SHA-1-only identity whose mapping has two complete candidate values, with
  the replicas driven to converge on one and no independent provenance for it,
  stays <code>identity_unproven</code>. Convergence never authorizes a
  promotion on its own.
- A claim write that cannot pin global <code>SERIAL</code> — including a
  deployment configured with <code>LOCAL_SERIAL</code> — fails closed rather
  than silently claiming in a per-DC domain.
- Missing markers, partial rows, digest mismatch, unavailable authority reads,
  ambiguous marker settlement, and each required DC outage fail closed with
  the specified <code>NOT_CERTIFIED</code> versus <code>UNKNOWN</code> result.
- M14-M19 are each shown to fail for their specific identity assertion, not
  merely for a compile error or an unrelated test failure.

Those legs do not all belong to the same stage, and the split is exact:

**With the authority primitive.** Delete/re-create claim survival; the
canonical-SHA-256 digest divergence; a concurrent or stale protocol-aware
writer against an established marker; and the global-<code>SERIAL</code>
pinning behavior. All stated at the claim and digest layer, provable without a
certifier. M16-M17.

**PR #228, with the certifier gate.** Divergent <code>(library_id, fs_id)</code>
and divergent <code>H -> R</code>; the SHA-1-only refusal and the
paired-mapping disagreement; the missing/partial/ambiguous fail-closed matrix;
and the certifier-side classification of the conflicts the primitive already
proves, which must reach <code>NOT_CERTIFIED</code> with no liveness work and
no witness. All of it integration evidence against the already-proven
primitive. M14-M15.

**With the mapping promotion path.** The pre-fence write delivered after the
claim settles, and the converged-but-unproven mapping. M18-M19.

**Before destructive GC and before the first productive consumer.** The delete
of a covered identity injected between the final identity revalidation and the
witness settlement. That leg proves the certification-window fence, which the
fence's own section scopes out of PR #228, so it cannot be required of a
certifier that does not yet implement it.

A brownfield cutover matrix is not listed at all: under the greenfield contract
there is no stage that runs one. See
[Coverage versus minimum correctness](#coverage-versus-minimum-correctness).

The runner must own an isolated Cassandra keyspace/network/volumes and
prefixed containers, like the existing PC-D1B evidence. It must not attach to
or stop either active application stack. Existing physical-byte, exact-P,
GC-authority, moving-HEAD, ambiguous-witness settlement, and
<code>EACH_QUORUM</code> outage evidence remains required and is not replaced
by this matrix.

## Implementation sequence and non-goals

1. Review and merge this architecture decision without runtime changes.
2. PR #230 landed the authority-only claim/schema primitive for semantic
   <code>commits</code> and <code>fs_objects</code> identities, with M16-M17
   and isolated 3-DC evidence. This is historical pre-wiring state.
3. PR #231 wires every inventoried commit/fs-object writer and deleter through
   the authority protocol, freezes the no-bypass fence, preserves recoverable
   <code>created_at</code>, and characterizes mixed-funnel reuse, blind-DC
   deletes, per-operation claim cost and gateway-level 3-DC evidence.
4. PR #228 implements the certifier gate and M14-M15. Until mapping authority
   exists, a reachable SHA-1-only identity whose canonical dependency comes
   solely from <code>block_id_mappings</code> is
   <code>identity_unproven</code>; #228 must not issue a witness for it. This
   permits the certifier to land fail-closed before mapping promotion.
5. Specify and audit the separate mapping-authority representation and
   cold-path promotion path (M18-M19), including its operational runbook. A
   library with SHA-1-only dependencies needs successful promotion before it
   can be certified; promotion is not a prerequisite for #228 to fail closed.
6. Specify the certification-window fence before destructive GC activation
   and before the first productive consumer, and re-scope it to PR #228 if a
   non-GC reachable delete is demonstrated.
7. Answer the stored-witness questions — <code>D</code> composition,
   <code>A</code> semantics, and the cost of new <code>IF</code> predicates on
   the landed PC-D1A primitives — before any consumer relies on a witness
   across identity deletion.
8. Add the first productive consumer only after writer/deleter wiring and its
   no-bypass fence are complete, the certifier is fail-closed, the certification
   window is fenced, and every mapping-dependent identity it expects to certify
   has authoritative mapping coverage.
9. Historical cutover, backfill of pre-authority rows and forensic
   reconstruction have no stage in this sequence under the greenfield
   contract. This decision does not authorize lifecycle serialization, PC-2,
   funnel migration or GC activation. <code>GC_ENABLED=false</code> remains
   mandatory.

The current status of PR #228 and of this finding lives in
<code>ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01</code>, not in this document.

## Primary references

- Repository certifier and identity writer paths (the certifier sources exist
  on the PR #228 branch only, not on the <code>main</code> baseline):
  <code>internal/db/library_continuity_certifier.go</code>,
  <code>internal/api/sync.go</code>, <code>internal/api/seafhttp.go</code>,
  <code>internal/api/v2/fs_helpers.go</code>,
  <code>internal/api/v2/libraries.go</code>,
  <code>internal/api/v2/admin_libraries.go</code>.
- Identity deletion paths:
  <code>internal/api/v2/library_rollback.go</code>,
  <code>internal/api/v2/publish_repair.go</code>,
  <code>internal/api/v2/fs_helpers.go</code>,
  <code>internal/gc/store_cassandra.go</code>.
- Logical-to-canonical block mapping:
  <code>internal/db/block_references.go</code>
  (<code>WriteBlockIDMapping</code>, <code>GetBlockIDMappingContext</code>) and
  <code>internal/db/migrations/009_block_representation_mappings.cql</code>.
- Canonical global <code>SERIAL</code> domain:
  <code>internal/db/library_head_serial.go</code>
  (<code>LibraryHeadSerialConsistency</code>), the PC-D1A primitives in
  <code>internal/db/library_continuity.go</code>, and PC-D1
  "Canonical SERIAL domain prerequisite".
- Historical fixes and their limited scope: <code>docs/KNOWN_ISSUES.md</code>,
  <code>ISSUE-SYNC-PUTCOMMIT-NOT-WRITE-ONCE-01</code> and
  <code>ISSUE-SYNC-RECVFS-NOT-WRITE-ONCE-01</code>.
- Canonical table keys: <code>internal/db/migrations/001_initial_schema.cql</code>.
- Apache Cassandra:
  [Dynamo consistency levels](https://cassandra.apache.org/doc/stable/cassandra/architecture/dynamo.html),
  [consistency guarantees](https://cassandra.apache.org/doc/stable/cassandra/architecture/guarantees.html),
  [4.1 read repair](https://cassandra.apache.org/doc/4.1/cassandra/operating/read_repair.html),
  and [hints](https://cassandra.apache.org/doc/latest/cassandra/managing/operating/hints.html).

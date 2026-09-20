# PC-D1B Metadata Identity Authority Decision

**Status as of 2026-09-20:** DECIDED, documentation only. This document owns
the reasoning, the rejected alternatives and the required evidence. The
current status of the finding lives in
[KNOWN_ISSUES.md](./KNOWN_ISSUES.md) and is deliberately not restated here.
**Issue:** `ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01`
**Branch:** `docs/pc-d1b-metadata-identity-authority-decision` (PR #229)
**Audited implementation:** PR #228, `feat/pc-d1b1-certified-baseline-certifier`

**Decision submitted for review:** require durable identity provenance for
whatever the certifier witnesses, and reject a certifier-local consistency
read as sufficient authority. The legacy cutover specified below is the only
admissible route to certifying historical rows, but it is not a precondition
for the certifier being correct; see
[Minimum correctness versus legacy reach](#minimum-correctness-versus-legacy-reach).

**Scope:** the historical <code>commits</code> / <code>fs_objects</code>
identity blocker for the PC-D1B.1 certifier in open PR #228, plus the
logical-to-canonical <code>block_id_mappings</code> resolution that the same
proof depends on.

**Baseline:** this design branch starts at <code>main</code>
<code>a09650b7a226</code>. The audited PR #228 HEAD is
<code>71ce28fd9</code>. Its certifier sources
(<code>internal/db/library_continuity_certifier.go</code> and the
<code>library_continuity_certifier_*</code> files) exist on that branch only;
a reader on the <code>main</code> baseline will not find them.

**Runtime status:** no schema, writer, consistency-level, certifier, or GC
change is included here.

This is an addendum to the inherited-continuity decision in
[PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md](./PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md).
It does not reopen the fixes from PR #208: those fixes narrow the affected
current write paths, but do not retroactively establish provenance for stored
rows or create one authority protocol shared by every writer.

## Decision

The certifier may witness HEAD <code>H</code> only when the exact
commit-to-root mapping, every reachable filesystem-object identity, and every
logical-to-canonical block mapping those objects resolve through are backed by
durable, immutable authority evidence. A row being present and
complete is not proof that it is the authoritative version of that identity.

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

## Verified current state

The following is source inspection on the <code>main</code> baseline and the
audited open PR #228 diff, not an inference from the prior mutation suite:

| Area | Current behavior | What it establishes / does not establish |
|---|---|---|
| PC-D1B.1 certifier (#228) | <code>readContinuityCommitRootContext</code> and the tree walker read <code>commits.root_fs_id</code> and <code>fs_objects</code> through ordinary session reads. The baseline witness is keyed by HEAD. | Completeness and a locally observed tree; not a durable proof that the same historical mapping is authoritative in every DC or cannot change later. |
| Sync <code>PutCommit</code> | Uses <code>INSERT ... IF NOT EXISTS</code>, with global <code>SERIAL</code> for the Paxos phase; a conflicting retry is rejected. | First-writer-wins for this endpoint. The client-supplied commit ID is checked against the request path, not recomputed as a digest of the stored root mapping. |
| Sync <code>RecvFS</code> | Verifies SHA-1 over the exact decompressed JSON before parsing. <code>storeSyncFSObject</code> then does a <code>LOCAL_QUORUM</code> read followed by an ordinary <code>LOCAL_QUORUM</code> insert/update, without a per-object Paxos claim. | Validates the received payload on this path and reduces conflicting replay. It is not a global immutable claim shared with every writer, and it does not attest old rows. |
| Other production writers | Ordinary <code>commits</code> / <code>fs_objects</code> inserts remain in initializers and internal paths, including <code>createInitialCommit</code>, sync auto-merge/directory creation, SeafHTTP upload/directory creation, and v2 <code>FSHelper</code> / library creation. | These paths do not all go through the <code>PutCommit</code> or <code>RecvFS</code> authority checks. Their generated IDs may be content- or attempt-derived, but that alone is not a shared cross-DC write-once protocol. |
| <code>block_id_mappings</code> resolution | The certifier's <code>resolveBlockIDs</code> resolves each logical Seafile SHA-1 to a canonical SHA-256 through <code>GetBlockIDMappingContext</code>. When the row also carries a paired internal SHA-256 the resolved value is cross-checked against it; when the row carries SHA-1 ids only, the mapping row is the sole authority for which bytes the file depends on. | The physical-liveness proof inherits this table's provenance. <code>WriteBlockIDMapping</code> is a read-before-write followed by a plain <code>INSERT</code> — no LWT, no serial domain — and its own contract documents a residual same-key race. It is therefore exactly the writer class this decision rejects, sitting on the certifier's critical path. |
| Deletion of identity rows | Production deletes <code>commits</code> / <code>fs_objects</code> rows on four paths: whole-partition teardown in library-creation rollback, per-row known-loser cleanup in publish repair, commit deletion in the v2 FS helpers, and GC's own commit/fs-object removal. All are ordinary deletes that consult no authority. | The writer inventory above is insert-only. A tombstoned identity can be re-created later under the same key with different semantic fields, and a witness that already certified the deleted identity is not invalidated by the delete. |
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
<code>EACH_QUORUM</code> read, a complete-tree walk, and the existing M1-M13
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
| File fs object | <code>(library_id, fs_id)</code>, object type, size, ordered logical Seafile block IDs, and, for every logical id not paired with a canonical SHA-256 in the same row, the mapping identity below. |
| Logical-to-canonical block mapping | <code>(org_id, representation_id, external_id) -> internal_id</code> in <code>block_id_mappings</code>. Required whenever a file identity names logical SHA-1 ids without a paired canonical SHA-256, because the mapping alone then decides which physical bytes the liveness proof is about. Its authority is independent of the <code>fs_objects</code> row that consumes it: an authoritative file identity resolved through an unproven mapping is unproven. |

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
| Marker is absent for a legacy row, a complete row conflicts with the marker, or two complete identities are observed for one key | <code>NOT_CERTIFIED</code> (<code>identity_unproven</code> / <code>identity_conflict</code>) | Do not establish new baseline liveness and do not write a witness |
| Authority read, replica availability, writer-epoch check, or marker settlement is unavailable or ambiguous | <code>UNKNOWN</code> | Do not establish new baseline liveness and do not write a witness |
| Row is missing, partial, malformed, or its physical bytes / GC authority fail the existing checks | Existing <code>NOT_CERTIFIED</code> / <code>UNKNOWN</code> contract | Do not write a witness |

All reachable metadata identities — including every logical-to-canonical block
mapping the walk resolved — must pass this gate before the certifier starts the
physical-liveness handshake. Before settling, the certifier must revalidate the
same <code>H</code>, the same captured root <code>R</code>, the identity
authority of everything it traversed, and continuity version <code>V</code>, in
addition to the existing exact-P / GC-authority rules. Whether any of that is
*stored* in the witness is the open question above; performing the revalidation
is required either way. An ambiguous final CAS continues to require
authoritative settlement; it never implies success.

## Minimum correctness versus legacy reach

Two things are decided here and they have different merge consequences.
Separating them is deliberate.

**Minimum correctness — what the certifier owes.** It must never issue a
witness it cannot back. Absence of authority evidence is
<code>identity_unproven</code>, conflicting complete identities are
<code>identity_conflict</code>, and unavailable or ambiguous authority reads
are <code>UNKNOWN</code>. This is entirely a property of the certifier and its
gate. It requires no cutover, no backfill and no forensic recovery of any
historical row, because the correct answer for an unproven identity is to
refuse. A certifier that fails closed on every library that exists today is
*correct*; it is merely not yet *useful* for them.

**Legacy reach — how many libraries can be certified at all.** Making
historical rows certifiable is a separate problem with a separate protocol
(writer fence, replica convergence, trusted-source validation, cutover marker)
and its own operational runbook. Its absence bounds coverage; it does not make
the certifier wrong.

Consequently:

- The authority gate, the fail-closed classification, the mapping authority
  and M14-M16 are the correctness contract for PR #228.
- The cutover protocol below is **not** a merge precondition for PR #228. It
  is a precondition for a productive consumer that expects existing libraries
  to certify, and for PC-2.
- A deployment may run with zero cutover performed. Every pre-existing library
  then returns <code>NOT_CERTIFIED</code>/<code>identity_unproven</code> and
  <code>WorkSetScopeNewlyLive</code> stays inadmissible for it, which is the
  PC-D1 fail-closed default rather than a regression.
- Forensic reconstruction of a divergent historical identity is a third,
  separate activity. It is never on the certifier's path and never a blocker
  for it.

## Selected provenance and cutover protocol

### New identities

The follow-up implementation must provide one identity-authority primitive
used by every path that creates, changes or removes semantic
<code>commits</code>, <code>fs_objects</code> or <code>block_id_mappings</code>
fields:

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
3. Materialize or complete the source row only under that claim. A crash or
   ambiguous claim leaves an unverified/pending identity, never a certifiable
   one. Before HEAD publication or baseline certification, verify that the
   stored row matches the claimed digest and is visible in the required
   multi-DC authority domain.
4. Fence every old or bypass writer. A claim table alone is insufficient if
   any route can later upsert identity fields without consulting it.

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

### Existing identities

Absence of a marker means <code>UNPROVEN</code>, not “probably old but safe.”
Historical rows may be marked authoritative only by an explicit cutover that:

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

The cutover runbook must define how it proves the writer fence, replica
convergence, drained mutations/hints, and source validation for the deployed
Cassandra topology. Until that operational proof exists, legacy rows without
authority markers remain ineligible. This ADR does not claim that an
<code>EACH_QUORUM</code> read or a routine repair alone satisfies the cutover.

### Deletion and re-creation

An identity can also leave. Production deletes <code>commits</code> and
<code>fs_objects</code> rows on four paths — whole-partition teardown in
library-creation rollback, per-row known-loser cleanup in publish repair,
commit deletion in the v2 FS helpers, and GC's own commit/fs-object removal —
and Cassandra allows the same key to be written again afterwards. A protocol
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
   claim in place.
4. **A witness is not self-maintaining.** If a certified identity is deleted
   afterwards, the witness that covered it is stale in fact while still
   matching HEAD and contract version. Until the stored-witness questions
   above are answered, any consumer must treat deletion inside the certified
   tree as witness-invalidating. Naming and implementing the mechanism that
   enforces this is a prerequisite for the first productive consumer, not for
   PR #228, whose certifier fails closed on the missing row at its next
   certification anyway.

## Certifier protocol and required evidence

The admissible order is:

~~~text
capture HEAD H
  -> prove authoritative commit identity H -> root R
  -> walk the complete tree and prove every fs-object identity
  -> prove the authority of every logical-to-canonical block mapping used
  -> run the existing exact-P / physical-bytes / liveness / GC handshake
  -> revalidate H, R, every traversed identity's authority, and GC authority
  -> write and settle the witness
~~~

If a stored <code>D</code> or authority epoch <code>A</code> is adopted later,
it is computed after the tree walk and revalidated in the same step; the order
above does not change.

The implementation must extend the existing M1-M13 mutation suite with at
least M14-M16:

| Mutation | Required red assertion |
|---|---|
| M14 removes or weakens the identity-authority marker/digest check and accepts a complete row based only on the ordinary read | The targeted contract test turns RED with a complete divergent <code>fs_objects</code> identity and with a divergent <code>H -> R</code> commit mapping; the certifier must otherwise refuse to witness either case. |
| M15 accepts a logical-to-canonical block mapping without proving its authority, or trusts the mapping when the row also carries a paired canonical SHA-256 that disagrees | The targeted contract test turns RED for an unproven mapping on a SHA-1-only file identity and for a mapping that resolves to a different canonical id than the row names; neither may reach the physical-liveness handshake. |
| M16 lets a claim be removed, reset or bypassed when its source row is deleted, or lets a re-created key take a fresh first claim | The targeted contract test turns RED when a deleted-and-re-created identity with a different digest is accepted, and when a cleanup/rollback/GC path clears the claim. |

Isolated real 3-DC evidence must then prove:

- Same <code>(library_id, fs_id)</code> with complete semantic identity A in
  one DC and B in another is <code>NOT_CERTIFIED/identity_conflict</code> or
  <code>NOT_CERTIFIED/identity_unproven</code>, with no new liveness work and
  no witness.
- Same <code>(library_id, commit_id=H)</code> with <code>H -> R1</code> in one
  DC and <code>H -> R2</code> in another is rejected while canonical library
  HEAD remains H.
- A delayed or old-version writer cannot change an identity after its marker
  is established; if the test can bypass the fence, no witness may remain
  usable.
- A file identity whose logical SHA-1 list resolves through
  <code>block_id_mappings</code> is refused when that mapping has no authority
  evidence, and refused when two DCs resolve the same
  <code>(org_id, representation_id, external_id)</code> to different canonical
  ids, with no new liveness work and no witness.
- Deleting a certified identity row and re-creating the same key with a
  different semantic projection is refused; the claim survives the delete, and
  no path clears it as a side effect.
- A claim write that cannot pin global <code>SERIAL</code> — including a
  deployment configured with <code>LOCAL_SERIAL</code> — fails closed rather
  than silently claiming in a per-DC domain.
- Missing markers, partial rows, digest mismatch, unavailable authority reads,
  ambiguous marker settlement, and each required DC outage fail closed with
  the specified <code>NOT_CERTIFIED</code> versus <code>UNKNOWN</code> result.
- M14-M16 are each shown to fail for their specific identity assertion, not
  merely for a compile error or an unrelated test failure.

The cutover matrix — an authorized legacy cutover marks only identities that
passed its writer, replica and source-validation checks, while
divergent/unverifiable cases remain unproven — belongs to the cutover work
item, not to PR #228; see
[Minimum correctness versus legacy reach](#minimum-correctness-versus-legacy-reach).

The runner must own an isolated Cassandra keyspace/network/volumes and
prefixed containers, like the existing PC-D1B evidence. It must not attach to
or stop either active application stack. Existing physical-byte, exact-P,
GC-authority, moving-HEAD, ambiguous-witness settlement, and
<code>EACH_QUORUM</code> outage evidence remains required and is not replaced
by this matrix.

## Implementation sequence and non-goals

1. Review and merge this architecture decision without runtime changes.
2. Implement and audit the per-identity authority schema/primitive — covering
   commit, fs-object **and** logical-to-canonical block-mapping identities —
   its writer inventory, its delete/re-create rules, and its no-bypass epoch
   fence, with every claim pinned to the canonical global <code>SERIAL</code>
   domain. Do not infer safety from the choice of consistency level.
3. Return to PR #228 with the certifier gate, the fail-closed classification,
   M14-M16 and the isolated 3-DC matrix. Its correctness does not wait on the
   legacy cutover.
4. Specify and audit the legacy cutover and its operational runbook as a
   separate work item: it is the precondition for a productive consumer that
   expects existing libraries to certify, and for PC-2, not for #228.
5. Answer the stored-witness questions — <code>D</code> composition,
   <code>A</code> semantics, and the cost of new <code>IF</code> predicates on
   the landed PC-D1A primitives — before any consumer relies on a witness
   across identity deletion.
6. This decision does not authorize historical backfill, a productive
   consumer, lifecycle serialization, PC-2, funnel migration, or GC
   activation. <code>GC_ENABLED=false</code> remains mandatory. The current
   status of PR #228 and of this finding lives in
   <code>ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01</code>, not in this
   document.

No Docker commands were run for this documentation-only design change.

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

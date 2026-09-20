# PC-D1B Metadata Identity Authority Decision

**Decision submitted for review:** require durable identity provenance plus an
explicit legacy cutover; reject a certifier-local consistency read as
sufficient authority.

**Scope:** the historical <code>commits</code> / <code>fs_objects</code>
identity blocker for the PC-D1B.1 certifier in open PR #228.

**Baseline:** this design branch starts at <code>main</code>
<code>a09650b7a226</code>. The audited PR #228 HEAD is
<code>71ce28fd9</code>.

**Runtime status:** no schema, writer, consistency-level, certifier, or GC
change is included here. PR #228 remains OPEN / NO MERGE until the selected
authority gate and its evidence exist.

This is an addendum to the inherited-continuity decision in
[PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md](./PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md).
It does not reopen the fixes from PR #208: those fixes narrow the affected
current write paths, but do not retroactively establish provenance for stored
rows or create one authority protocol shared by every writer.

## Decision

The certifier may witness HEAD <code>H</code> only when the exact
commit-to-root mapping and every reachable filesystem-object identity are
backed by durable, immutable authority evidence. A row being present and
complete is not proof that it is the authoritative version of that identity.

The authority evidence must bind the source identity key to a versioned digest
of the semantic fields used by tree traversal and dependency resolution. It
must be established by a write-once, globally serialized claim (or an
equivalent protocol with the same no-conflicting-writer guarantee), and all
production writers must honor that claim. A side-table marker that old or
unmodified writers can bypass is not sufficient.

The effective baseline witness contract is:

~~~text
(library X, HEAD H, captured root R, reachable-tree digest D,
 identity-authority epoch A, continuity-contract version V)
~~~

<code>D</code> is deterministic and covers the authorized semantic identity of
every reachable object and the canonical block identities used for the
physical-liveness proof. The witness must bind <code>H -> R -> D</code>;
storing only <code>H</code> and <code>V</code> is insufficient while
historical row identities can vary. PC-D1A currently stores the certified
HEAD and continuity-contract version, so materializing this stronger witness
contract is a separate schema prerequisite, not a silent change to this
documentation-only PR.

## Verified current state

The following is source inspection on the <code>main</code> baseline and the
audited open PR #228 diff, not an inference from the prior mutation suite:

| Area | Current behavior | What it establishes / does not establish |
|---|---|---|
| PC-D1B.1 certifier (#228) | <code>readContinuityCommitRootContext</code> and the tree walker read <code>commits.root_fs_id</code> and <code>fs_objects</code> through ordinary session reads. The baseline witness is keyed by HEAD. | Completeness and a locally observed tree; not a durable proof that the same historical mapping is authoritative in every DC or cannot change later. |
| Sync <code>PutCommit</code> | Uses <code>INSERT ... IF NOT EXISTS</code>, with global <code>SERIAL</code> for the Paxos phase; a conflicting retry is rejected. | First-writer-wins for this endpoint. The client-supplied commit ID is checked against the request path, not recomputed as a digest of the stored root mapping. |
| Sync <code>RecvFS</code> | Verifies SHA-1 over the exact decompressed JSON before parsing. <code>storeSyncFSObject</code> then does a <code>LOCAL_QUORUM</code> read followed by an ordinary <code>LOCAL_QUORUM</code> insert/update, without a per-object Paxos claim. | Validates the received payload on this path and reduces conflicting replay. It is not a global immutable claim shared with every writer, and it does not attest old rows. |
| Other production writers | Ordinary <code>commits</code> / <code>fs_objects</code> inserts remain in initializers and internal paths, including <code>createInitialCommit</code>, sync auto-merge/directory creation, SeafHTTP upload/directory creation, and v2 <code>FSHelper</code> / library creation. | These paths do not all go through the <code>PutCommit</code> or <code>RecvFS</code> authority checks. Their generated IDs may be content- or attempt-derived, but that alone is not a shared cross-DC write-once protocol. |
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
| File fs object | <code>(library_id, fs_id)</code>, object type, size, ordered logical Seafile block IDs, and the canonical physical block identities used by the liveness proof (or a separately versioned, independently certified mapping to them). |

<code>obj_name</code>, <code>full_path</code>, and <code>mtime</code> are not
identity inputs for this certifier unless a future semantic reader makes them
so. The digest format itself must be versioned and deterministic. A content
hash is useful evidence only when the exact source bytes and the mapping from
those bytes to the persisted semantic projection are both validated; a
hash-shaped identifier alone is not authority.

The authority state is fail-closed:

| Observation | Certifier result | Side effects |
|---|---|---|
| Durable marker exists, its version and digest match the complete row, and the writer fence is active | Continue to tree-digest and physical-liveness proof | No witness until every later proof and final HEAD CAS succeeds |
| Marker is absent for a legacy row, a complete row conflicts with the marker, or two complete identities are observed for one key | <code>NOT_CERTIFIED</code> (<code>identity_unproven</code> / <code>identity_conflict</code>) | Do not establish new baseline liveness and do not write a witness |
| Authority read, replica availability, writer-epoch check, or marker settlement is unavailable or ambiguous | <code>UNKNOWN</code> | Do not establish new baseline liveness and do not write a witness |
| Row is missing, partial, malformed, or its physical bytes / GC authority fail the existing checks | Existing <code>NOT_CERTIFIED</code> / <code>UNKNOWN</code> contract | Do not write a witness |

All reachable metadata identities must pass this gate before the certifier
starts the physical-liveness handshake. The final witness attempt must recheck
the same <code>H</code>, <code>R</code>, <code>D</code>, authority epoch
<code>A</code>, and continuity version <code>V</code>, in addition to the
existing exact-P / GC-authority rules. An ambiguous final CAS continues to
require authoritative settlement; it never implies success.

## Selected provenance and cutover protocol

### New identities

The follow-up implementation must provide one identity-authority primitive
used by every path that creates or can change semantic <code>commits</code> or
<code>fs_objects</code> fields:

1. Canonicalize the semantic projection and calculate its versioned digest.
2. Make a durable, no-TTL per-identity claim under the configured global
   <code>SERIAL</code> domain. The first claim fixes the digest; an identical
   retry is idempotent and a different digest is a conflict.
3. Materialize or complete the source row only under that claim. A crash or
   ambiguous claim leaves an unverified/pending identity, never a certifiable
   one. Before HEAD publication or baseline certification, verify that the
   stored row matches the claimed digest and is visible in the required
   multi-DC authority domain.
4. Fence every old or bypass writer. A claim table alone is insufficient if
   any route can later upsert identity fields without consulting it.

A dedicated authority table keyed by
<code>(library_id, identity_kind, identity_id)</code> is a candidate that keeps
LWT scope per identity. The exact schema and write ordering belong to the
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

## Certifier protocol and required evidence

The admissible order is:

~~~text
capture HEAD H
  -> prove authoritative commit identity H -> root R
  -> walk the complete tree and prove every fs-object identity
  -> compute deterministic reachable-tree digest D
  -> run the existing exact-P / physical-bytes / liveness / GC handshake
  -> revalidate H, R, D, authority epoch A, and GC authority
  -> write and settle witness (X, H, R, D, A, V)
~~~

Before this authority prerequisite lands, PR #228 remains open and blocked.
The follow-up implementation must extend its M1-M13 mutation suite with at
least M14:

| Mutation | Required red assertion |
|---|---|
| M14 removes or weakens the identity-authority marker/digest check and accepts a complete row based only on the ordinary read | The targeted contract test turns RED with a complete divergent <code>fs_objects</code> identity and with a divergent <code>H -> R</code> commit mapping; the certifier must otherwise refuse to witness either case. |

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
- Missing markers, partial rows, digest mismatch, unavailable authority reads,
  ambiguous marker settlement, and each required DC outage fail closed with
  the specified <code>NOT_CERTIFIED</code> versus <code>UNKNOWN</code> result.
- An authorized legacy cutover marks only identities that passed its writer,
  replica, and source-validation checks; divergent/unverifiable cases remain
  unproven.
- M14 is shown to fail for the specific identity assertion, not merely for a
  compile error or unrelated test failure.

The runner must own an isolated Cassandra keyspace/network/volumes and
prefixed containers, like the existing PC-D1B evidence. It must not attach to
or stop either active application stack. Existing physical-byte, exact-P,
GC-authority, moving-HEAD, ambiguous-witness settlement, and
<code>EACH_QUORUM</code> outage evidence remains required and is not replaced
by this matrix.

## Implementation sequence and non-goals

1. Review and merge this architecture decision without runtime changes.
2. Implement and audit the per-identity authority schema/primitive, writer
   inventory, no-bypass epoch fence, and legacy cutover procedure as explicit
   prerequisites. Do not infer safety from the choice of consistency level.
3. Return to PR #228 only after those prerequisites exist. Add the certifier
   gate, bind the witness to <code>R</code> and <code>D</code>, extend M1-M13
   with M14, and run the isolated 3-DC matrix.
4. Keep #228 OPEN / NO MERGE until its own P1 gate and all existing full gates
   pass. This decision does not authorize historical backfill, a productive
   consumer, lifecycle serialization, PC-2, funnel migration, or GC
   activation. <code>GC_ENABLED=false</code> remains mandatory.

No Docker commands were run for this documentation-only design change.

## Primary references

- Repository certifier and writer paths:
  <code>internal/db/library_continuity_certifier.go</code>,
  <code>internal/api/sync.go</code>, <code>internal/api/seafhttp.go</code>,
  <code>internal/api/v2/fs_helpers.go</code>,
  <code>internal/api/v2/libraries.go</code>,
  <code>internal/api/v2/admin_libraries.go</code>.
- Historical fixes and their limited scope: <code>docs/KNOWN_ISSUES.md</code>,
  <code>ISSUE-SYNC-PUTCOMMIT-NOT-WRITE-ONCE-01</code> and
  <code>ISSUE-SYNC-RECVFS-NOT-WRITE-ONCE-01</code>.
- Canonical table keys: <code>internal/db/migrations/001_initial_schema.cql</code>.
- Apache Cassandra:
  [Dynamo consistency levels](https://cassandra.apache.org/doc/stable/cassandra/architecture/dynamo.html),
  [consistency guarantees](https://cassandra.apache.org/doc/stable/cassandra/architecture/guarantees.html),
  [4.1 read repair](https://cassandra.apache.org/doc/4.1/cassandra/operating/read_repair.html),
  and [hints](https://cassandra.apache.org/doc/latest/cassandra/managing/operating/hints.html).

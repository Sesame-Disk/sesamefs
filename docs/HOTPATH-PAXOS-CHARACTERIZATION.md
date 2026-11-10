# Hot-path Paxos characterization (2026-11-09)

**Status:** characterization only; no runtime or schema change. The options at the end need a
maintainer decision. **Issues:** `ISSUE-HOTPATH-GLOBAL-PAXOS-01` (registered here) and
`ISSUE-LIBRARY-HEAD-ORG-PARTITION-CONTENTION-01`. The per-block install stays with X4,
`ISSUE-UPLOAD-PER-BLOCK-PAXOS-01`, and its [X1/X4 record](./UPLOAD-PAXOS-HOT-PATH-X1-CHARACTERIZATION.md),
which owns its design constraints; this record only measures its count. **Test:**
`internal/integration/hotpath_paxos_characterization_test.go`, gate
`SESAMEFS_REQUIRE_HOTPATH_PAXOS_CHARACTERIZATION`.

Every conditional write (LWT) and every `SERIAL` read runs a Paxos round. Under global `SERIAL`
on three DCs, each round waits on another DC. This record counts those rounds on the two upload
routes clients use, says what each one protects, and lists the ways to take the rounds other
than the block install off the hot path.

## Measured shape

The test runs the production handlers in-process on a session with a query observer, against
real Cassandra 5.0.9 and MinIO. The count does not depend on topology, so one node is enough.
Two consecutive runs gave the same numbers; driver retries were 0.

| Route | Paxos rounds per request |
|---|---|
| Web upload, one request (`/seafhttp/upload-api`), new file in the root | 8 |
| Same, file two directories deep | 10 |
| Web upload, chunked (resumable.js), N new blocks, root | 7 + N |
| Desktop push (Sync), M new files with one block each, root | 2M + 4 |

In general, with N new blocks and a file d directories below the root:

| Operation | Web upload | Desktop push | Table and partition | Paxos domain |
|---|---|---|---|---|
| Block install (`InstallBlockMetadata`) | N | one per `put-block` | `blocks ((org_id, block_id))` | `SERIAL`, pinned |
| Finalize lease, acquire and release | 2 | 0 | `gc_leases`, row `upload-finalize:<repo>` | session default (`SERIAL` in prod) |
| Identity claim, per new fs object | d + 2 (file, each directory, root) | one per object in `recv-fs` | `identity_authority_claims ((library_id, kind, id))` | `SERIAL`, pinned |
| Identity claim and `SERIAL` read of the commit | 2 | 2 (`put-commit`) | same | `SERIAL`, pinned |
| HEAD CAS | 1 | 1 | `libraries ((org_id), library_id)` | `SERIAL`, pinned |

The HEAD CAS of every library of an organization runs in one Paxos state, the organization's
partition. The second test shows it: publications in two libraries of one organization
advance the same `system.paxos` row, keyed by `org_id`, and no library has a row of its own.
The other Paxos tables have one partition per block, object or library, so their rounds do
not contend with each other; they cost latency, not contention.

## What each round protects

| Round | Protects | Consumer today |
|---|---|---|
| Block install | First-writer arbitration: concurrent materializations of one new block settle on one canonical placement while deduplication stays organization-wide; X1 builds on it. The X1/X4 record finds that removing it needs an equivalent global arbitration, and that keeping it is a valid outcome | Runtime placement; GC, which is off |
| Finalize lease | Serializes upload finalizations in one library across processes | Runtime |
| Identity claims | Durable identity provenance for commits and fs objects ([PC-D1B](./PC-D1B-METADATA-IDENTITY-AUTHORITY.md)) | The continuity certifier, which has no production caller; frozen since the [X1 reset](./X1-CRITICAL-PATH.md) and not an X1 prerequisite |
| HEAD CAS | A HEAD that never forks or goes back | Runtime; the Seafile desktop protocol needs the answer in the request |

## Ordering, from the code

These facts are code-verified, not timed:

- Chunked finalization stores up to 8 blocks in S3 in parallel, then installs each block's
  metadata under a permit with capacity 1 for the whole process
  (`finalizeUploadBlockMetadataConcurrency` in `internal/api/seafhttp.go`). The N
  block-install rounds of a large upload run one after another, and other chunked
  finalizations in the same process wait behind them. Already characterized under X4; the
  one-request path does not take this permit.
- The identity gateway runs its claims sequentially
  ([PC-D1B claim-cost table](./PC-D1B-METADATA-IDENTITY-AUTHORITY.md#pr-231-measured-claim-cost-characterization)).
- A one-request web upload stores the whole file, up to 1 GiB, as one block. The comment
  above it in `HandleUpload` still describes splitting large files; only chunked uploads
  split into 8 MiB blocks.
- The Seafile desktop protocol needs the HEAD answer in the request: OK, or not a fast-forward,
  after which the client merges. Publishing HEAD from a background worker would break it.

## Cost

Not measured on the real hosts. On three DCs with one replica each, global `SERIAL` needs two
of three replicas, so every phase of a round waits for the nearest other DC. A Paxos v1 round
has four phases (prepare, read, propose, commit). Measured contention at 80 ms simulated RTT:
see `ISSUE-LIBRARY-HEAD-ORG-PARTITION-CONTENTION-01`.

## Options

None is decided. Each changes code that X1 evidence or a maintainer decision depends on. The
block install is not reopened here: its options belong to X4. The X1/X4 record rejects
switching a multi-DC deployment to `LOCAL_SERIAL` as a performance shortcut, because two DCs
could each accept a decision; option C below differs only in that every writer of one
identity goes through the same DC, which is what has to be designed and proved.

| Option | What changes | Rounds after it | Cost and risk | Schema |
|---|---|---|---|---|
| **A. Identity claims off the hot path** | Writers stop claiming. If the certifier is ever activated, it uses the fenced cutover PC-D1B already allows for unproven identities | Web N + 3 (blocks, lease, HEAD); push M + 1 (blocks, HEAD) | Reverses part of PC-D1B and PR #231 for a consumer that does not run. The certifier stays frozen until that cutover exists | None |
| **B. `libraries` keyed `((org_id, library_id))`** | One Paxos state per library; point reads and LWTs keep their `WHERE` clause | Same count, no contention between libraries | At least 12 whole-organization reads (`WHERE org_id = ?`) need an organization index table, and some of them need it complete, not a projection. Touches the HEAD path, so X1 evidence must be re-run | Primary key: only cheap while `001` is editable |
| **C. A home DC per library** | HEAD and the lease run with `LOCAL_SERIAL` in the library's home DC; other regions forward the publication | Same count, but those rounds stay in one DC; a remote upload pays one forward round trip | A library cannot be written while its home DC is down, until a fenced handover. Contradicts the current decision that HEAD writers use global `SERIAL`. Blocks are per organization, not per library: extending this to the block install is X4's placement-domain question. X1 evidence must be re-run | One added column, possible after `001` freezes |

A and B together leave, per web upload, N block installs (X4), the lease and one HEAD CAS
without contention between libraries. C is a separate design. Only B depends on when `001`
freezes.

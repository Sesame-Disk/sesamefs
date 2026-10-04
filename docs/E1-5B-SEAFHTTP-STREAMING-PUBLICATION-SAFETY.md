# E1-5b / W2-7b: SeafHTTP streaming publication safety

## Frozen plan

Base: main@8ce5fd8c70acf8709d7518daee1634f2bddf3b5d.

Characterize productive chunked HandleUpload (multipart + Content-Range,
Cassandra token/permission checks and pinned SILO) before runtime changes.
Use at least two distinct internal blocks (8 MiB block boundary).
Capture every original internal SHA-256/storage-class/storage-key after
confirmed registration and before publication repair. Observe exact up:, pub:,
fs:, HEAD/commit/tree, pending owner, repair, D lifecycle/root and physical key.
Hooks only schedule/observe; never fabricate query outcomes.

Required real-service legs: normal multi-block; victim COMMITTED; victim
TERMINAL/key absent; TERMINAL same-tracker retry legitimately rematerializes
only the victim to a different P while preserving valid blocks; real HEAD CAS
conflict validates every original placement again. Remove only the victim's
owned up: as explicit expiry-state control; never remove repair to force D.
Freeze baseline RED before production fix. Omission of only the new gate must
restore retirement REDs while normal remains GREEN. Required evidence gate
rejects missing named legs or unavailable infrastructure.

If RED, retain placements atomically with per-position accounting, restore
original placements on retries, pass them through every metadata HEAD attempt,
and validate all distinct internal blocks with the existing publication
authority primitive after durable repair, immediately before HEAD. Missing,
contradictory, malformed or unknown placement/authority fails closed.
Blocked/Changed preserves retryable 409 and identifies rejected block IDs.
Clean only owned failed-attempt state. Invalidate all positions of rejected
block IDs, then rerun productive materialization on same-tracker retry;
never replace cached P by merely reading a new canonical tuple. Preserve
valid positions. Bound validation concurrency, preserve SHA-1 wire identities.

Cover duplicate coherent/contradictory positions, missing placements and
Unknown authority, cleanup and retry identity in meaningful tests. Run Docker
unit/integration checks, race repetitions, omission and incomplete-run gates;
audit final source and evidence before commit/push/PR.

Closure is only measured streaming pre-HEAD safety/retry recovery. Single-shot
behavior, post-HEAD/R31, W2-9/10, W2-11..14, natural TTL/candidate discovery,
process restart/distributed tracker, multi-DC certification, Phase 5/6,
PRE-GC/A1 and GC activation are excluded. E1/X1 remain open. Production GC OFF.

## Execution and measured contract

Frozen plan commit: 818e0084b. Baseline runtime: main@8ce5fd8c70acf8709d7518daee1634f2bddf3b5d
with integration-only scheduling/observation hooks. No authority result was
fabricated. Productive multipart HandleUpload uses Content-Range and a stable
resumableIdentifier, Cassandra token/permission lookup, actual SILO IO, and an
8 MiB first block plus a distinct second block. SHA-1 remains the wire identity;
physical authority is internal SHA-256/class/key.

Baseline normal passed. COMMITTED and TERMINAL each returned 200, advanced HEAD
and installed permanent fs: for both blocks while the exact victim P1 was already
retired. The initial directed run inherited unrelated required suite gates;
those also rejected the filter, but both explicit post-D violations were observed.
The later omission control clears unrelated gates and independently reproduces
both violations with normal GREEN.

The fix atomically retains per-position original placements with accounting.
Already-accounted retries recover the original tuple without canonical recapture.
Incomplete snapshots and duplicate digests naming different P fail closed.
Every metadata retry validates distinct placements after durable repair, before
HEAD, with at most eight concurrent authority reads. Any non-Authorized outcome
or read error rejects. Blocked/Changed carries the existing retryable sentinel
and rejected block IDs through owned cleanup. All duplicate positions of those
IDs are invalidated; unknown/permanent failures authorize no rematerialization.
A later same-tracker request rereads temp bytes and repeats productive
probe/verify/PUT/registration for invalidated positions only.

| Leg | Observed result |
|---|---|
| normal | 200/plain file SHA-1; reachable paired canonical file layout; both exact original P and correct bytes; fs: for each block; repair and pending owner settled |
| committed | exact zero-proof, claim, COMMITTED handoff/root/orphan and existing K1 before resume; 409; unchanged HEAD; neither block gains fs:; attempt commit/pub:/repair/pending owner removed; D not revoked |
| terminal | exact terminal recovery/K1 deletion before resume; 409 and unchanged HEAD/no fs:; old canonical absent and K1 absent |
| terminal-retry | same ChunkUpload pointer and upload operation; exactly one additional victim registration, none for healthy block; new victim P/key with correct bytes, original healthy P unchanged; 200 and reachable file/fs:; old D remains TERMINAL/K1 absent |
| head-conflict | real empty CreateFile wins first CAS; two owned repairs and two productive exact-P reads per original block; retry commit parents competitor HEAD; fs: acknowledged before repair deletion |

Query acknowledgments observe actual repair INSERT, per-block LOCAL_QUORUM
publication authority reads and fs:/repair-delete settlement. Tests assert
positional internal SHA-256/external SHA-1 file layout, HEAD/tree, both physical
objects, attempted commit cleanup, pending owner and exact retirement identity.
The mandatory gate requires normal, committed, terminal, terminal-retry and
head-conflict; filtered normal-only run fails with four missing legs.

Unit controls additionally reject missing/malformed tracker state, failed
registration, contradictory duplicate placements, Unknown/Permanent outcomes,
and Authorized+error. Coherent duplicates are validated once. Selective
invalidation drops all rejected digest positions while preserving valid
positions and immutable snapshots. A blocked worker control proves bounded
parallelism and canceled-context rejection.

## Limits and disposition

CLOSED-FIX only for measured SeafHTTP streaming pre-HEAD rejection and
same-process tracker recovery. Combined with E1-5a, both exercised SeafHTTP
pre-HEAD slices are fixed. E1-07 remains PARTIAL; broader W2-7/post-HEAD/R31,
W2-11..14 and X1 remain OPEN. This does not certify encrypted/cross-class,
multiple datacenters, process restart, distributed trackers, concurrent owner
cleanup, every post-HEAD crash point, natural TTL expiry or candidate discovery.
Removing only the victim's owned up: models expiry state; it is not a 48-hour
TTL/discovery test. No repair was deleted to force retirement. COMMITTED may
legally advance to TERMINAL after resume; assertions preserve old exact D and
reject its revocation without requiring canonical P1 to remain forever.

Production docker-compose.prod.yml/.env.prod.example remain GC_ENABLED=false.
The local Docker proof stack intentionally has one recovery-capable GC worker;
this PR neither activates production GC nor authorizes PRE-GC/A1.

## Validation

Validation results and final source fingerprints are appended after the full
Docker checks and final audit. Logs are outside Git under
%TEMP%/sesamefs-e15b-evidence (baseline, omission, directed, filtered gate,
unit, full suite, race and vet).

### Final source fingerprints

Host and rebuilt full-suite Docker image SHA-256 agree for all eight Go files.

| File | SHA-256 |
|---|---|
| internal/api/seafhttp.go | 94656403dbea9249dec09211aba586405e677545114e537f24972b2b53c72ae6 |
| internal/api/seafhttp_test.go | 6347a88a6b0e24756497a2cecae37917fee55a6c0fc68dcc69849b4a6c53a7a7 |
| internal/api/seafhttp_streaming_publication.go | c6a041fb55551472606a4f0683733c065c1fbf69cd89bcad131e0d49b1be2201 |
| internal/api/seafhttp_streaming_publication_test.go | 5f953bf297f6bcb2df19247767ad222565da045a9374ec115377afe7734217bb |
| internal/api/seafhttp_streaming_barriers.go | 9552dfca5415ae3ef0b56990c78ed0e54ab33f2da1bb1715dcb2966cc468104d |
| internal/api/seafhttp_streaming_barriers_integration.go | dc7788885d73184b9dffde6362761962dfa6df3f730810ad36e17fc3777d624b |
| internal/integration/e15b_seafhttp_streaming_test.go | 197fdedb5aacf17b59cfb995052f9ac664be9e94360645b8b1bd75688fc0e408 |
| internal/integration/integration_test.go | a31ebea7a9a5dab7912f485d52971285fca21efa092e4a6c06f542cc775bbddc |

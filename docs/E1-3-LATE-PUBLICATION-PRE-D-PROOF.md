# E1-3: late publication against GC pre-D proof

Status: PASS for the named orderings on codex/e1-3-late-publication-pre-d-proof.
Base: main@abb558358, merged #251. Plan frozen before implementation,
2026-10-03. X1 remains OPEN; production GC activation unchanged.

## Scope and harness

Two deterministic interleavings against the current Office CreateFile/shared
repair path, real Cassandra 5.0.9 and pinned SILO
`RELEASE.2026-09-16T00-00-00Z`. Reuse the productive G5 worker, real
candidate/queue, settled claim, global zero-proof, exact-P validation and
repair visitor. Observe driver SELECT responses on a dedicated worker session;
pause only at the chosen real boundary. Never substitute liveness results.

Use existing request barriers to prepare actual P1 and publication state.
Remove only fixture-owned temporary references through the production API as
an explicit expiry-state control, following the existing W2 harness. No TTL
policy changes or claims of elapsed 48h/35d TTL/scanner coverage. No CQL writes
manufacture HEAD, repair, claim, physical life or D authority.

## Leg A: GC claim wins before late repair acquisition

1. Start a real Office writer and pause after staging but before repair queue.
2. Model expiry of its exact temporary references; verify zero real references
   and no repair. Drive a real candidate through G5's settled claim.
3. After the final negative repair-domain read, hold GC before its second refs
   read. Resume the writer while that exact claim remains active.
4. Observe actual late repair acquisition and final exact-P rejection; require
   HEAD unchanged and no permanent fs: reference.
5. Resume GC. Accept either a safe new D or a liveness-driven veto, inspecting
   exact P/D identity and canonical/object/ref/repair state. Never require D if
   newly staged liveness legitimately vetoes it.

A no-GC writer control must succeed. Negative control: omit the productive
final exact-P validation only inside the runner; this leg must fail its
no-HEAD assertion. No mutation is a production counterexample.

## Leg B: reachable settlement wins during the proof

1. A real Office writer wins HEAD and stops before permanent promotion, leaving
   its durable repair. Model only temporary-reference expiry.
2. Drive the real worker through its settled claim and first refs=0 response.
   Pause before scanning the repair domain.
3. Run the real reachable repair visitor on a separate session. Require exact
   fs: installation before its repair disappears, unchanged HEAD/tree/P1/K1.
4. Continue GC: observe a negative repair scan and the second real refs read.
   Require the second read to see the permanent reference, claim released, no
   D, canonical life/object retained and repair settled.

Negative control: omit the second productive refs read inside the runner;
this leg must detect destructive progress despite its reachable fs:.

## Validation and disposition

Run directed real-service legs, then the complete standard Docker Go/API/OIDC
suite, normal/integration vet, the two isolated RED mutations, restored-source
race repetitions, and a final whole-diff review. Keep stacks sequential; wait
for an existing integration runner to finish before testing.

GREEN: record only the named gates/interleavings and covered funnel; no runtime
change. RED: preserve the actual counterexample, implement the minimal fix and
prove restored safety with relevant controls. Update E1/X1/current-work evidence
without closing all pre-D handoff, W2-11/14 or X1 by non-reproduction.

Excluded: schema, TTL policy, repair scheduler, W2-11 redesign, W2-14 scanner
candidate discovery, Phase 5/6, stale-claim, clock-health, PRE-GC and A1.

## Execution evidence — 2026-10-03

The plan was frozen in `52a4742ba`. `TestE13LatePublicationPreDProof` adds
three real-service legs without production runtime edits:

| Leg | Directed result | Exact observation |
|---|---|---|
| noGCWriterControl | PASS | Productive Office CreateFile wins HEAD, installs permanent fs:, settles repair and retains exact bytes. |
| claimWinsBeforeLateRepair | PASS | G5 holds exact P1 claim; first refs=0 and all 32 repair buckets empty. The writer then acknowledges a real repair INSERT, but final exact-P returns 409, HEAD stays unchanged and cleanup removes its repair/pins. The second refs read returns zero, G5 commits D1 and terminal recovery retires P1/K1. |
| reachableSettlementWinsDuringProof | PASS | G5 holds exact P1 claim after its first refs=0 response. Productive REACHABLE repair acknowledges fs: INSERT before repair DELETE. GC then reads 32 empty repair buckets and its second refs read returns one. The claim is released, no delete lifecycle/root exists, and HEAD/tree/P1/K1 remain intact. |

The driver observer scopes by exact org/block and EACH_QUORUM statements on a
dedicated worker session. It pauses after a completed response, leaving those
real rows unchanged. The late-writer barrier is after staging but before repair
queue; the settlement barrier is after the first zero-reference response,
before any repair bucket read. Recovery uses a separate real session. Channel
release/join precedes session and fixture teardown, including on failures.

One initial harness assumption was corrected: ProcessOrgOnce's processed count
also includes settlement of a re-referenced live queue item. Both successful
legs report one processed item. Safety assertions therefore inspect actual
canonical P1, claim, handoff, lifecycle/root, refs and bytes instead of treating
that count as a delete count.

The final test source copied into the Docker runner has SHA-256
`6b405a3f8d963fbfc64994771ff2d70b96fa86a71ff2eefd4a8a8e052f25af15`;
the host and runner hashes match. This follows the image build while correcting
only the test harness; production source remains the base version.

These results cover the two measured orderings for Office CreateFile/shared
repair. They do not close every pre-D schedule, any other funnel, concurrent
cleanup, W2-11/14 or X1. No OS restart or isolated multi-DC failure leg is
claimed. Temporary-reference removal is explicitly an expiry-state control;
no real TTL wait, expiry scanner discovery or TTL-policy change is claimed.

## Full validation — PASS, 2026-10-03

All service executions were sequential in the same Docker runner, after the
previous runner finished. The standard Compose go-all-test command passed Go
unit/integration, API and OIDC with required evidence gates. Integration
completed in 437.115s; all three E1-3 legs passed within that suite.
`go vet ./...` and `go vet -tags integration ./...` passed.

Two isolated container-only mutations were RED at their behavior assertions:

- Omitting the productive final exact-P check allowed the late writer to
  advance HEAD under the held GC claim. The test failed at
  `late writer published HEAD under settled GC claim`.
- Omitting the second productive refs read allowed G5 to retire canonical P1
  after the real reachable repair installed fs:. The test failed at
  `E1-3 settlement destroyed canonical P1: exists=false err=<nil>`.

Mutation setup/compilation failures were not counted as RED evidence. Both
source files were restored and byte-compared before the final race run; host
production files were never mutated. These are negative controls, not
counterexamples against the unchanged production runtime.

`go test -tags integration -race -v -count=10 -timeout 5m ./internal/integration
-run '^TestE13LatePublicationPreDProof$'` passed all ten repetitions in 33.261s
without race reports. Final formatting and whole-diff checks passed. Optional
isolated 3DC/proxy/saturation legs are not claimed as passed. E1 remains partial,
W2-11/14 and X1 OPEN, and no production GC activation gate changed.

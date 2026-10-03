# E1-3: late publication against GC pre-D proof

Status: planned on codex/e1-3-late-publication-pre-d-proof.
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

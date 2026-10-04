# E1-6 / W2-10a: RevertFile publication characterization

## Frozen plan

Base: main@f2837397d211df02a011c74a693ce0332a8d611f (#255).

Use the real RevertFile handler with Cassandra and SILO. First publish historical
content using the productive upload path, remove its entry through the productive
DeleteFile handler, and pause RevertFile after its historical entry read before
HEAD. Observe historical fs_object/block layout, exact P, all refs, repair,
HEAD/commit/tree and physical bytes. Never delete historical fs: to manufacture
zero liveness. An absent current HEAD dependency is not an absent historical pin.

Run normal, retained-history GC attempt, multi-block retained-history GC attempt,
and real competing HEAD/CAS retry controls. Drive the productive GC worker with
fixture-scoped candidate discovery; do not substitute authority/zero-proof query
results. Prove that the worker actually attempts the proof and that retained fs:
explains refusal if D is unreachable. Distinguish this result from full safety.

If a supported, extant-history path reaches COMMITTED/TERMINAL before revert
publication, freeze its RED before changing runtime. Only then implement a
BORROWED adapter retaining the original exact placements across all HEAD retries,
owned liveness, pub:, durable repair, final strict exact-P validation and
settlement. Preserve no-op/conflict policies, shared historical metadata and
cleanup ownership. Add stage/repair failure and gate-omission controls for that
fix. Do not rematerialize historical bytes automatically.

If the measured retained-history path prevents D, publish characterization and
its precise limits without a speculative runtime fix or CLOSED-FIX claim.
The source-of-record explicitly permits this outcome (E1-10). COMMITTED/TERMINAL
legs remain unexecuted unless their prerequisites are established productively.

Wire a mandatory named-leg gate into TestMain and standard Docker suites; verify
filtered runs fail closed. Run directed and race checks in Docker, appropriate
full regression/vet, audit source and documentation, then commit/push/create PR.
W2-10, E1 and X1 remain open unless narrower supported evidence warrants a claim.
Other resurrection funnels, W2-11..14/R31, Phase 5/6 changes, PRE-GC/A1 and GC
activation are out of scope. Production GC remains OFF.

## Measured outcome and disposition

Frozen plan commit: cef35d669. The retained-history schedule is GREEN; no
extant-history COMMITTED/TERMINAL retirement RED was reproduced. This result
selects the characterization-only branch of the frozen plan. The early worker
exit is earlier than the initial harness expected: it never claims P1 or starts
the global zero-proof. Initial probe assertions were corrected to the observed
productive path, without changing worker/query responses or removing fs:.

| Named leg | Actual observation |
|---|---|
| normal | productive upload/delete/revert; 200; HEAD reaches original historical fs_object; original P/K/bytes and permanent fs: survive |
| retained-history-gc | same historical read; upload up: explicitly lapsed; real worker positive local fs: read settles controlled candidate before claim/global proof; no D; revert succeeds |
| multiblock-retained-history-gc | productive chunked upload creates two distinct blocks; both own up: lapsed; each real candidate takes the same positive-reference skip; ordered internal SHA-256/external SHA-1 layout and both original P/K remain |
| head-conflict | retained historical pin blocks real GC; productive empty CreateFile wins first CAS; actual RevertFile retries, publishes from competing HEAD and preserves both entries |
| same-content | productive RevertFile reports same-content no-op; historical read occurs but no HEAD attempt or HEAD change |
| skip | productive empty-file replacement plus conflict_policy=skip; historical read occurs but no HEAD attempt or HEAD change |

The fixture starts with upload through HandleUpload, Cassandra token/permission
lookup and SILO IO. DeleteFile and RevertFile run in-process with real Cassandra
and storage, using the actual library owner context. Revert/delete calls do not
certify external HTTP auth/routing. The multi-block case uses one full-range
Content-Range multipart request through the productive chunked upload path.

Only isolated upload-owned up: references are explicitly removed as an expiry-
state control, before delete/revert. No elapsed TTL behavior is inferred.
Historical fs: is never removed to force a race. Its exact referrer is the sole
remaining reference before and after publication. Revert's historical hook is
after oldEntry capture; the harness separately observes historical file layout.
It does not claim that runtime already captures block placements at that point.

Candidate discovery is controlled through EnsureBlockGCCandidateExact and the
real queue, scoped to each fixture block. Dequeue filtering prevents unrelated
work from running; claims, references and queue settlement remain productive.
The completed worker LOCAL_QUORUM SELECT returns one historical reference;
it processes/skips one candidate, consumes that candidate, and never calls the
global proof or repair scan. An independent EACH_QUORUM liveness observation
reports RealReference; that observation is not a worker zero-proof interleaving.
SERIAL canonical readback, lifecycle lookup and orphan-root discovery show no
claim, handoff, retirement lifecycle or destructive recovery root. P1/K1 and
physical bytes remain unchanged. No claim-release or postponed-candidate
behavior is inferred from this early exit.

Writer observation begins after historical upload/delete/competitor setup.
Revert creates no repair or fs: settlement writes. The exact permanent fs:
reference remains inherited rather than newly installed by the revert. HEAD
attempt hooks, winning commit parent and both entries demonstrate the actual
CAS conflict/retry. Historical layout is observed before/after the publication.

## Limits and next prerequisite

W2-10 / E1-10 remain OPEN. No CLOSED-FIX, full pre-HEAD safety, E1 PASS, X1 CLOSED,
PRE-GC, A1 or GC activation is asserted. The other three resurrection handlers
are unchanged. Current RevertFile still lacks its own pin, pub:, durable repair
and final exact-P. Positive retention evidence does not certify those absent
mechanisms or every reachable historical state.

COMMITTED/TERMINAL, stage/repair failures and exact-P-gate omission remain
unexecuted because this slice establishes neither a supported zero-reference
extant-history prerequisite nor a new runtime gate/staging sequence. A distinct
supported source-publication or retention/cleanup interleaving is needed before
proposing that fix.
Deleting the historical fs: alone would fabricate the deciding precondition.
Phase 5/6 metadata/ref deletion and their separate shared-fs/TOCTOU risks are not
exercised or closed; cross-repo, R31, multi-DC and post-HEAD behavior are separate.
No automatic historical rematerialization or BORROWED adapter was added.
Production GC remains OFF; fixture-scoped manual GC only tests the early skip.

## Validation and final audit

All checks below ran in Docker against Cassandra 5.0.9 (NTS/datacenter1 RF1)
and pgsty/silo:RELEASE.2026-09-16T00-00-00Z. Required gate SESAMEFS_REQUIRE_E16_REVERTFILE_CHARACTERIZATION rejects missing named
legs and unavailable infrastructure. Both standard Docker integration commands
include the gate; TestMain checks availability and all six completed legs.


- Directed six-leg characterization plus named-leg contract: PASS (22.200s).
- Ten productive -race repetitions: PASS (316.615s), 60 named subtests.
- Standard go-all-test command: PASS, full Go short/coverage regression,
  mandatory integration evidence, API and OIDC. E1-6 also passes in the full run.
- Normal and integration go vet: PASS.
- Required gate with normal-only filter: expected failure, five named legs missing.
- Required gate with unavailable backend: expected failure before tests run.
- Omit only the historical scheduling hook in the isolated runner: expected
  failure in one/two-block GC legs (historical visits=0). This is a harness
  sensitivity control, not an exact-P-gate omission or a retirement RED.
  Source restored afterward, verified by SHA-256.
- Host, final rebuilt image and validation runner agree on all five Go hashes
  below. Docker gofmt check and git diff whitespace check: PASS.

Logs are outside Git under $TEMP/sesamefs-e16-{directed,race,unit,vet,filtered,
unavailable,omission,full-suite,final-build}.log. No backend image was redeployed:
new productive handler evidence executes the current source in the Go runner;
external API/OIDC regressions use the existing dev stack. GC settings were not
changed. Optional multi-DC evidence is not certified by this single-DC run.

| Go source | SHA-256 |
|---|---|
| internal/api/v2/files.go | 13be6f0f2eb831da9beaa8fecfe6e39dc8a9d9517cfbd395d4359792718300ed |
| internal/api/v2/revertfile_publication_barriers.go | 1b3de77f54f3911cbc4e5c651fb360e132c8d2db4b53dd0c32e60a709ea9db0e |
| internal/api/v2/revertfile_publication_barriers_integration.go | ff795b468aa24319fdaa6fe20ba9da0129c226d655517d5a55a6af1f9355236c |
| internal/integration/e16_revertfile_publication_test.go | 43fea75247f60becc49d1da1bd407cf5b1d05ba04c17c12e19321a35b4e40a74 |
| internal/integration/integration_test.go | 193f51632b732c8861fb05f9c817bc79307838e42a21652edf4c793f04dda321 |

Final scoped audit covered actual worker early-exit/queue settlement, inherited
reference identity, physical bytes/layout, real HEAD conflict/no-ops, driver
observation, hook isolation/build tags, completion gates and source-of-record
claims. No unresolved introduced P0/P1/P2 was found. The existing W2-10 P1 and
separate Phase 5/6 risks remain open; this review does not close them.

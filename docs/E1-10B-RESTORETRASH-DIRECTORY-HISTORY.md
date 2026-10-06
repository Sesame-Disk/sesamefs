# E1-10b / RestoreTrashItem directory retained-history

## Frozen plan

Base: main@2f5635621f1a0dafafd24e0916fcdf88e8d0f149 (#259 merged).
Measure one historical directory with one child file, one plaintext block,
same repo/org, retained commit/fs_objects, pre-HEAD and one DC. No runtime fix
before a supported retirement RED.

Build the tree productively using CreateDirectory and HandleUpload into that
parent, then DeleteDirectory. Observe historical commit/root/DIR1/FILE_FS1,
canonical SHA-256/external SHA-1 layout, exact P/K/bytes and sole permanent
fs:<repo>:<FILE_FS1>. Explicit lapse of only the own upload up: is an expiry-
state control, not elapsed TTL. Register owned expiry cleanup before removal.
Never remove historical fs: or invent D. Synchronize observed asynchronous
DeleteDirectory work before restore/teardown; do not sleep to assume completion.

Required independent legs: directory-normal, directory-retained-history-gc,
directory-head-conflict. Reuse #259 hooks without adding production code.
Pause after oldEntry == DIR1; run the actual block worker with exact fixture-
controlled candidate discovery and productive liveness/settlement queries.
Require positive local historical fs:, candidate settled, no global zero-proof,
claim/D/recovery root, original P/K/bytes. Resume and verify full current HEAD
root -> named DIR1 -> named FILE_FS1 -> canonical block -> exact P/K/bytes.
Force a real competing HEAD before first CAS; require one historical hook, two
HEAD attempts, winning commit parent == competitor and both entries intact.

Use a separate gate and test prefix that does not match #259's ^TestE110 selector.
Reuse manual-GC sesamefs-e19/sesamefs_e19 through a dedicated child process;
required child completion and subtest filters remain mandatory, all other gates
stay in the parent. Leave #259's three-leg contract/helper unchanged.

If historical retention blocks D, CLOSED-EVIDENCE only for the measured subset;
COMMITTED/TERMINAL remain unexecuted. If productive D and retired-P1 publication
are reproduced, freeze RED and implement only the minimal necessary funnel fix.
No directories deeper than one level, general multiblock, retention cleanup,
Phase 5/6, R31/W2-11..14, multi-DC, #258 follow-ups or activation.
RevertFile evidence is not reclassified; W2-10/E1-10 overall and E1/X1 stay OPEN.
Production GC OFF; normal dev daemon behavior unchanged.

Run directed matrix, race repetitions, filtered/unavailable/active-daemon and
historical-hook omission rejection controls, both vet modes and final Docker
go-all-test. Audit source, cleanup, gate independence and claim limits; commit,
push and create PR. Update CURRENT_WORK, E1 ledger, KNOWN_ISSUES, CHANGELOG and
X1-CRITICAL-PATH without closing W2-10 or declaring all residual risk Phase 5/6.

## Measured outcome

Frozen plan: 265268ce3. Initial directed three-leg matrix and independent
completion/TestMain wiring checks PASS (18.901s). No production changes were
needed: existing RestoreTrashItem hooks schedule the same productive handler.

| Leg | Actual observation |
|---|---|
| directory-normal | Real CreateDirectory/HandleUpload(parent old-dir)/DeleteDirectory/RestoreTrashItem returns success; HEAD reaches original named DIR1 and child FILE_FS1; canonical/external block layout, exact P/K and bytes remain |
| directory-retained-history-gc | After actual oldEntry == DIR1 capture, sole child fs: is visible to real worker LOCAL_QUORUM; candidate settles before claim/global zero-proof/D; restore resumes and preserves full subtree |
| directory-head-conflict | Same positive GC attempt; productive empty CreateFile wins first HEAD; restore actually retries; winning commit parent is competing HEAD and both the competitor and historical subtree survive |

Productive CreateDirectory/DeleteDirectory/restore run in-process with actual
library-owner context and permission checks; upload uses HandleUpload with real
token/permission lookup and SILO IO. This is not external route/auth evidence.
Only /old-dir/<unique file> is uploaded, through a parent-bound token and multipart
parent_dir. The root upload helper remains a wrapper using exactly '/' so E1-6,
E1-10/#259 and E1-09 fixture behavior is preserved. #259's suite, gate, selector
and child helper are unchanged.

After productive directory deletion, the harness observes the final empty subtree
file_tags scan and final negative library/day counter update. No tags were added
to this fixture; these are the last DB operations in the two async paths. Counter
readback is zero before restore. No fixed sleep certifies async completion, and
no counter/tag query outcome is replaced. These observations synchronize fixture
housekeeping, not certify general accounting or arbitrary tagged directories.

The historical commit/root and named root->DIR1->FILE_FS1 path are re-read before
and at the historical barrier and after publication. DIR1 contains exactly one
named non-directory child; its internal SHA-256/external SHA-1 IDs, file size and
canonical exact P are unchanged. Current winning HEAD's own commit/root is read
and the complete named subtree is traversed again, not inferred from historical
metadata existence. SILO read verifies original bytes. Runtime captures DIR1 at
the hook; the harness separately observes its descendant layout/P.

Exactly one own upload up: is explicitly lapsed as an expiry-state control, not
elapsed TTL. Its canonical/by-day expiry cleanup is registered before removal.
Sole permanent fs:<repo>:<FILE_FS1> is unchanged throughout; directory fs_id is
not mistaken for the block referrer. No historical fs: or metadata is deleted to
manufacture zero. Actual exact-candidate/queue discovery is controlled; worker
reference/claim/settlement queries remain productive. One positive local worker
read, candidate removal, no global worker proof/repair scan, no SERIAL canonical
claim/handoff, lifecycle or recovery root are required. Independent global
RealReference readback is observation, not a claimed worker zero-proof.

Writer observations show no repair rows or repair/fs: settlement writes. The
historical pin is inherited, not newly installed. HEAD-conflict requires one
historical visit and two HEAD attempts plus competing commit parent and entry.
Fixture cleanup owns unique block/key/refs/mapping/expiry and library artifacts.

## Independent gate and scope

SESAMEFS_REQUIRE_E110B_RESTORETRASH_DIR_CHARACTERIZATION requires the three
named directory legs. TestRestoreTrashDirectory* does not match #259's ^TestE110
selector. Dedicated same-binary child ^TestRestoreTrashDirectory preserves race
instrumentation and subtest filters; its own TestMain must prove all legs before
parent records evidence. Child removes inherited gate and process-bypass flags;
all other mandatory suites remain required in normal parent. Existing manual-GC
sesamefs-e19/sesamefs_e19 is reused without a new service/schema or global GC
change; authenticated status must explicitly report GC disabled for each child
endpoint. E1-10/#259 and E1-09 still execute their own unchanged matrices.

CLOSED-EVIDENCE only for this retained-history one-directory/one-child/one-block
plaintext single-DC pre-HEAD schedule. COMMITTED/TERMINAL remain UNEXECUTED: the
supported retained state blocks their prerequisite. No runtime fence or migration
was introduced. RevertFile earlier evidence is not retroactively reclassified.
W2-10/E1-10 overall, E1/X1, other resurrection handlers, broader directory cases,
retention cleanup/Phase5/6 and R31/W2-11..14 remain OPEN. Measuring narrow examples
of every handler would not prove all residual risk belongs exclusively to
Phase5/6. Production GC remains OFF; normal dev daemon stays available.

## Validation and audit

Final directed restored-source matrix plus named-leg and TestMain wiring checks:
PASS (14.565s). Five -race repetitions: PASS (95.499s), 15 measured named legs;
parent/child top-level PASS lines are not extra legs. Normal and integration vet,
Docker gofmt and diff whitespace checks PASS.

Required normal-only filter fails with two child legs missing and no parent
certificate. Required unavailable backend fails before tests. Active-GC endpoint
fails authenticated isolation before fixture creation. Remove only the historical
hook call in the isolated runner: all three directory legs fail with historical
visits=0 (HEAD visits remain 1/1/2), including a real CAS retry. This is scheduling
sensitivity, not a retirement RED or exact-P-gate omission. Runner source restored.
The original ^TestE110 selector with only its own required gate passes exactly
three single-file legs and executes no directory tests; #259 is not expanded.

Host, rebuilt go-all-test image and restored runner agree on these current Go
source hashes (including unchanged production/legacy files):

| Source | SHA-256 |
|---|---|
| internal/integration/e110b_restoretrash_directory_test.go | db17d78ea6619511e11453f72934c2fcd0a0529e591540a5c9043eda2b1e64ba |
| internal/integration/e16_revertfile_publication_test.go | 4f111ac4e4dcd9317e86c0106cfcedf185f959dc825edd69e73539759e4e3e54 |
| internal/integration/integration_test.go | 0482df2c1a47778d5f4c6d79ec2241a54cb3f1ca7f2f6300218e93f5184c30cb |
| internal/api/v2/trash.go | 01330d445dfa6168c48044f2a43497da65d68b43f72eb84ee9a9ce83b31a9ef9 |
| internal/integration/e110_restoretrash_retained_history_test.go | 6ab7a37f1d0b55dbf1bf7349afb2337ae80dbd8103b5d0a68037b679cc4136af |

Validation logs are outside Git under $TEMP/sesamefs-e110b-*.log. Production
handlers run from current test source against real Cassandra/SILO; external
regressions use the existing dev backend images. No new backend was deployed.
Optional multi-DC/cgroup/independently tagged gcsoak evidence is not certified.
Final actual `docker compose --profile test run --rm go-all-test`: exit 0.
Go short/coverage and mandatory integration PASS (857.566s); directory 3/3,
legacy single-file 3/3 and cross-repo 14/14 named legs PASS. All 14 daemon
controls PASS, zero GC-disabled skips. API 20/20 suites and OIDC 25/25 checks
PASS (zero OIDC skips). Optional evidence remains outside this certificate.

Final scoped audit: no introduced P0/P1/P2 findings. Production diff is empty;
original #259 test source/selector unchanged, frozen plan unchanged, mandatory
gate and invalid-evidence rejection controls verified. The existing content
resurrection P1 remains OPEN outside the measured retained-history subset.

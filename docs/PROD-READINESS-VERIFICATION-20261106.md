# Production Readiness Verification — first internal deployment

**Date:** 2026-11-06
**Source:** `main@a21f4203d`, exported with LF endings; no local changes
**Question:** is SesameFS ready for a first deployment for internal use, with
destructive GC off, where issues are closed progressively from real use?

This is a dated snapshot. Status of record stays in
[KNOWN_ISSUES.md](./KNOWN_ISSUES.md); this document supersedes no gate. It closes
no issue. The decisions below are the maintainer's.

## Verdict

**Go for a disposable internal pilot on three DCs, by maintainer decision, after two
fixes.** The [three-DC rehearsal](#three-dc-rehearsal) found that the first bootstrap
fails, and that with Cassandra's shipped timeouts and simulated WAN latency, even one
user's sequential uploads mostly fail. Data integrity held in every run. This is not a
production certification, and it supplies no evidence for external users or for data
that must survive. The purpose of the pilot is real testing: much has changed since X1
and X2 closed, and none of it has run on a real deployment.

### Maintainer decisions (2026-11-06)

| Decision | Consequence |
|---|---|
| **Pilot data is disposable** | No upgrade or legacy-data obligation yet. The environment may be rebuilt at any time. The pilot does not trigger the `001` freeze ([schema contract](./DATABASE-GUIDE.md#v1-baseline-fold-2026-10-19)) |
| **Three DCs from the first day** | The goal is real multi-DC evidence. The known failures below are expected to be exercised, not avoided |
| **The two registered go-live blockers are accepted for the pilot** | They stay OPEN with their severity and are fixed along the way. External users stay out until both close |
| **Backup and restore may wait** | [`ISSUE-BACKUP-RESTORE-MISSING-01`](./KNOWN_ISSUES.md#issue-backup-restore-missing-01-no-backup-or-restore-procedure-exists) stays OPEN and listed. It becomes a prerequisite the moment data must survive. Until then, assume any data can be lost |

These decisions scope this pilot. They close no issue and no gate. PRE-GC/A1 remain
OPEN and GC stays OFF on every replica in every DC.

### Before first traffic

| Item | Why |
|---|---|
| **Fix or work around the first bootstrap** | [`ISSUE-CASSANDRA-BOOTSTRAP-3DC-ROLE-01`](./KNOWN_ISSUES.md#issue-cassandra-bootstrap-3dc-role-01-first-three-dc-bootstrap-fails-before-creating-the-application-role): failed 2 of 2 fresh runs. Run it again until the role exists |
| **Raise Cassandra's write and CAS timeouts on every node** | [`ISSUE-CASSANDRA-TIMEOUTS-WAN-01`](./KNOWN_ISSUES.md#issue-cassandra-timeouts-wan-01-shipped-cassandra-timeouts-fail-uploads-under-wan-latency): the production compose has no setting for them yet |
| Build the release from an exact commit | No CI exists, so images are built and pushed by hand. Merge #324 first; see [Dependencies](#dependencies) |
| Set memory limits per host | [`ISSUE-PROD-COMPOSE-NO-MEMORY-LIMIT-01`](./KNOWN_ISSUES.md#issue-prod-compose-no-memory-limit-01-production-compose-sets-no-container-memory-limit). `pack-fs` amplification can otherwise take Cassandra down on the same host |
| One backend per regional hostname | Three pieces of state are per process; see [Topology](#topology) |
| Central nginx and user provisioning per region | See [Operational notes](#operational-notes) |
| Run DEPLOY Step M4 and M5 | Expect many backend restarts on first boot (`ISSUE-MIGRATOR-FIRST-BOOT-3DC-RESTARTS-01`). Health-check with `/api2/ping`, because `/ping` answers 200 even with the backend down (`ISSUE-HEALTH-PING-FALSE-OK-01`) |

### Accepted go-live blockers

Both are pre-existing, re-verified by reading `main`, and remain OPEN.

| Issue | Confirmed in code | Accepted exposure |
|---|---|---|
| `ISSUE-SYNC-FSID-WORK-AMPLIFICATION-01` | `PackFS` does not deduplicate, reads one row per id and buffers the whole response; `parseBoundedIDList` accepts about 409k ids | Any user or leaked token with read access can exhaust node memory |
| `ISSUE-APIKEY-READ-SCOPE-UPLOADLINK-FILESHARE-01` | Upload-link creation and all three file-share mutations use bare `HasLibraryAccess`; upload-link update/delete compare only the creator | A read-scoped key can create or change links and shares up to the owner's authority |

### Topology

Three DCs, RF 1 per DC, matching `.env.prod.example`.

- **Availability failures reproduce.** Concurrent uploads return HTTP 500 with the
  signature in the [October ledger](./E1-THREE-API-RESIDUALS.md); see the
  [rehearsal](#three-dc-rehearsal). A failed upload can in principle have published
  (`ISSUE-V2-UPLOAD-HEAD-CAS-TIMEOUT-500-01`), although no run here observed it.
- **WAN latency is simulated, not measured.** Per-block and HEAD LWTs use global `SERIAL`
  (`ISSUE-UPLOAD-PER-BLOCK-PAXOS-01`). Real numbers are the pilot's first job.
- **Repair is now required.** No scheduled `nodetool repair` runbook exists
  ([`ISSUE-CASSANDRA-REPAIR-RUNBOOK-01`](./KNOWN_ISSUES.md#issue-cassandra-repair-runbook-01-no-scheduled-repair-procedure-for-multi-dc)).
- **Do not place two backends behind one hostname.** Chunked uploads
  (`ISSUE-UPLOAD-CHUNK-MULTINODE-01`), desktop SSO
  (`ISSUE-SSO-PENDING-TOKEN-NODE-LOCAL-01`) and web login PKCE state
  (`ISSUE-OIDC-PKCE-STATE-NODE-LOCAL-01`, new) are per process.
- **The DC set should stay stable.** Changing it with populated reference state needs a
  separately certified migration before GC can be considered (X2 contract). A rebuild
  is the simple alternative while data is disposable.

### What the pilot should measure

These are the questions the rehearsal cannot settle on one host: upload and finalize
500 rates, and upload latency, over a real WAN; read-after-write across DCs
(`ISSUE-READ-AFTER-WRITE-CROSS-DC-01`); real desktop GUI and mobile clients; desktop
conflict handling; OnlyOffice; metadata retention with GC off, using the report in open
PR #320.

### GC off: what it means

The kill switch held in this run: logs report `[GC] Garbage collection is disabled`,
status reports `enabled:false`, and `POST /admin/gc/run` answers 503.

- **Space is never reclaimed.** Measured: objects of a permanently deleted library
  remain in the bucket. Storage and metadata only grow, so monitor bucket size.
- **Delete does not erase bytes.** Permanent delete removes metadata rows, not
  objects. Do not put data in the pilot that must be erasable.
- **Quota still works.** Measured: deleting a 3 MB file took usage from 3,000,000 to 0.
- **Timed lifecycles do not run:** library trash purge, user and org hard delete,
  audit retention and `auto_delete_days`. Their UI text can overpromise
  (`ISSUE-AUTO-DELETE-EXPIRY-CONTRACT-01`).
- **The GC queue accumulates work with no drain.** Observed: `queue_size` 125 after
  the suites on a GC-off stack.
- **`GC_ENABLED=false` does not stop every destructive path.** `PermanentDeleteRepo`
  still runs, so the P1 restore race remains live
  (`ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`).
- **GC activation does not inherit this environment.** The
  [first-production contract](./X1-CRITICAL-PATH.md#first-production-deployment-contract)
  assumes no inherited candidates, repairs or attempts. A1 needs a rebuilt environment,
  or a separate carry-forward assessment if that is ever preferred.

## What was run

Runs used a private Docker project with fresh volumes on one host: 7 CPUs, 14 GB.

| Check | Result | Notes |
|---|---|---|
| Go unit (`go test ./... -short`) | **PASS** | All packages |
| Full integration, GC off on every daemon | **PASS**: 555 pass, 0 fail, 89 skip (883.7 s) | The 89 include 13 needing a GC-enabled daemon and the 3-DC legs (`X2_DC_HOSTS` unset). Named evidence gates passed |
| API suites | **19 PASS, 1 FAIL** | `test-gc.sh`: 11 of 23 fail. All expect GC enabled; the 503 refusal is the correct behaviour under this posture |
| OIDC suite | **FAIL by environment** | OIDC disabled in this stack: 2 failures, login leg skipped. The real login flow is covered by the rehearsal below; this suite was not run with OIDC enabled |
| `go-all-test` runner | **Exit 1** | Consequence of the `test-gc.sh` result above; remaining suites were run explicitly. The standard GC-on daemon configuration was deliberately not run |
| Real `seaf-cli` sync | **PASS** 11/11 | Plain and encrypted. Desktop GUI, SeaDrive and mobile not run |
| Frontend unit | **PASS** 34 suites, 471 tests | |
| Deploy rehearsal, one DC | **37 pass, 2 harness fails, both re-tested as pass** | [Below](#deploy-rehearsal) |
| Three-DC rehearsal | **Failures found; integrity held** | [Below](#three-dc-rehearsal) |
| Simultaneous migrators on 3 DCs | **Converged in 2 of 2 completed trials** | [Below](#simultaneous-first-boot) |
| `govulncheck` | **FAIL: 10 reachable** | [Dependencies](#dependencies) |
| `npm audit --omit=dev` | **FAIL: 2 critical, 16 high** | [Dependencies](#dependencies) |

An earlier integration run from an unmodified `.env.example` failed 5 tests. Cause:
`ACCOUNTS_DISABLE_ORG_USER_WRITES` is absent there, so an authority test really
updated the shared default user, leaving it `admin` and `deactivated` as read from
Cassandra, and the others then failed. Environment, not a product regression; tracked
as `ISSUE-TEST-ENV-EXAMPLE-ACCOUNTS-AUTHORITY-01`.

### Deploy rehearsal

Followed [DEPLOY.md](./DEPLOY.md): preflight, images built from this commit, Cassandra,
explicit bootstrap, then application, using `docker-compose.prod.yml` and
`configs/config.prod.yaml` unmodified. Only external systems were stood in through a
compose override, which also attached the backend to their networks and fixed the
internal subnet for `SERVER_TRUSTED_PROXIES`: S3-compatible storage with SSE-S3, a
throwaway RS256 OIDC IdP, and a central nginx.

Passed through central nginx, frontend nginx and Go:

- `prod-preflight.sh --init-env`; validation refused an unfilled env, and correctly
  refused the rehearsal's `http://` issuer. The rehearsal deliberately continued
  past that single refusal, because the stand-in IdP has no TLS.
- Bootstrap, then `001` applied in about 41 s. Restart was idempotent, `/ready` OK.
- Dev tokens refused; `/ready`, `/metrics` and pprof not exposed publicly; a foreign
  `Origin` refused; forged client-IP headers not honoured behind the central nginx.
- OIDC login with PKCE, nonce, signature and audience; superadmin through
  `FIRST_SUPERADMIN_EMAIL`.
- Upload and download with matching SHA-256, plain and encrypted; share link including
  anonymous download; trash and restore; library delete, restore and permanent delete.
- With S3 missing, `/ready` returned 503, while the container stayed `healthy` and
  uploads returned `500 failed to store block metadata`. Monitor `/ready`.

The two smoke failures came from the harness: it requested the `/d/<token>/` URL shape
and did not follow an `https` redirect. The canonical `/d/<token>` URL and download
passed on re-test. Examining the first failure found
`ISSUE-SHARELINK-DIR-TRAILING-SLASH-404-01`.

### Simultaneous first boot

DEPLOY Step M4 starts every region's backend together, and the migrator has no lock.
Three real migrators started at once against an empty keyspace on a Cassandra 5.0.9
three-DC fixture, one coordinator per DC:

| Trial | Exit codes | Result |
|---|---|---|
| 1 | 0, 0, 0 | 115 tables, identical table ids on all DCs, one schema version |
| 2 | 0, 1, 1 | Two processes timed out on seed statement 101 of 118. A later boot exited 0 on every DC; 115 tables, identical ids |
| 3 | not counted | Cut by the harness time budget |

So after the v1 fold the crash loop seen in the earlier chain did not reproduce, and
a failed process recovers on restart. Two trials are not a proof, and the single-node
case was not re-run. Starting the first backend alone remains the simple option and
DEPLOY now says so.

## Three-DC rehearsal

**Setup.** One host (7 CPUs, 14 GB) stood in for three servers: `docker-compose.prod.yml`
and `configs/config.prod.yaml` unmodified, three compose projects, Cassandra 5.0.9 with
authentication, RF 1 per DC, `LOCAL_QUORUM` and `SERIAL`, one backend per regional
hostname, GC off. Deviations, all in a rehearsal-only override: a Docker network with
fixed IPs replaced the private network and host port publishing, so
`CASSANDRA_LISTEN_ADDRESS` was set and the backend attached to that network; named
volumes; 1 GB heap; stand-ins for S3, IdP and central nginx. Latency was added with
`netem` between Cassandra nodes only, equal between all DCs, so client and S3 paths had
none added. **It is a simulation on one host; most configurations ran once.**

### What failed

**The first bootstrap, 2 of 2 fresh runs.** Step M4.5 exits 2:
`ALTER KEYSPACE system_auth` raises replication, then in the same session `CREATE ROLE`
answers `User cassandra does not have sufficient privileges`, so the application role
does not exist and every backend crash-loops with bad credentials. Running the bootstrap
again succeeded both times, 30 seconds and about 17 minutes later, without a repair. The
cause was not isolated. The one-DC rehearsal does not show this, and the existing three-DC
fixture runs without authentication.

**Uploads under simulated WAN latency, with shipped timeouts.** Each cell is successes
of attempts for the upload shape, then the median time of successful uploads:

| Shape | Shipped 1 s / 2 s, no latency | Shipped, about 80 ms RTT | Shipped, about 200 ms RTT | 5 s / 5 s, about 80 ms RTT |
|---|---:|---:|---:|---:|
| Sequential, one user, 64 KB | 12/12, 276 ms | **5/12**, 2.8 s | **5/12**, 9.5 s | 12/12, 2.7 s |
| Sequential, other region, 64 KB | 12/12, 296 ms | 12/12, 2.9 s | 11/12, 6.9 s | 12/12, 2.7 s |
| Sequential, 8 MB | 4/4, 306 ms | **2/4**, 5.7 s | 3/4, 6.7 s | 4/4, 3.4 s |
| One region, 3 parallel, same library | 15/15, 411 ms | **7/15**, 5.7 s | 11/15, 15.2 s | 14/15, 5.2 s |
| Three regions, different libraries | 30/36, 356 ms | 23/36, 3.9 s | **11/36**, 9.4 s | 25/36, 4.9 s |

Raising `cas_contention_timeout` and `write_request_timeout` clearly helps sequential
use at 80 ms. Upload time follows the number of Paxos round trips, not file size.
Directory listings stayed at about 25–30 ms at every latency.

**Heavy contention on one library.** 45 uploads from three regions into the same
library, with concurrent `mkdir` from every region:

| Configuration | Successes of 45, parallel 6 with `mkdir` | Successes of 45, parallel 3 without |
|---|---:|---:|
| Shipped timeouts, no latency | 22, 15, 17 | 44 |
| 5 s timeouts, no latency | 39, 20, 42 | 45, 42 |
| `paxos_variant: v2`, shipped timeouts, no latency | 6 | 30 |
| Shipped timeouts, about 80 ms RTT | not valid | 11 |
| 5 s timeouts, about 80 ms RTT | 3 | 9 |

Timeouts do not solve this shape, and `paxos_variant: v2` made it worse. The errors
arrive from Cassandra: a direct same-partition CAS probe with no SesameFS gets 1–5%
`CAS operation result is unknown` or timeouts at no added latency, while uncontended
`SERIAL` LWTs took about 5 ms. SesameFS returns these as HTTP 500 from the per-library
finalize lease, the HEAD CAS and the block install.

**Smaller findings.**

- First boot: `001` needed 20 backend restarts over about 3.5 minutes with one backend
  starting alone, because individual statements timed out and the process exits. It
  converged: identical table ids on all DCs, one schema version. The other two backends
  then started with no restarts. In an earlier run on a slow bind mount, all three
  backends crash-looping together also reported `INCOMPATIBLE_SCHEMA` and a transient
  table difference.
- `/ping` through the frontend nginx returns the SPA with HTTP 200, even with every
  backend down. `/api2/ping` is correct.
- First LWT on a table: 2 of 24 sequential uploads failed with an ambiguous block install
  right after setup, then the same shape succeeded. A raw probe saw the same on its first
  statements.
- Immediately after a library is created in another region, uploads there returned 503
  or 500 (`failed to get root fs_id`, `invalid UUID ""`). A library soft-deleted at NA
  still read as not deleted at EU two seconds later. One acked upload was not listed at
  one region for over 3 seconds, then was.
- Permanent delete always fails its tag cleanup
  (`ISSUE-TAG-CLEANUP-COUNTER-BATCH-01`), and all 170 upload-failure lines in one
  backend log were `level=INFO` (`ISSUE-LOG-PRINTF-LEVEL-01`).
- Login started on one region and finished on another fails with
  `invalid state: state not found`, which reproduces
  `ISSUE-OIDC-PKCE-STATE-NODE-LOCAL-01`.

### What held

- **Integrity, in every comparable run.** Across 14 concurrency runs, all 345 uploads
  acknowledged with 200 were listed, none lost; none of the 285 failed uploads was
  listed, checked seconds after each run. Region views matched, apart from the lag above.
  This checks listings, not a formal consistency proof.
- **One DC down.** With Asia's Cassandra stopped, account, listing, download, upload,
  library creation and share links all succeeded in all three regions. Asia's backend can
  only have used another DC's coordinator, which needs CQL reachable across the private
  network as in this setup. After restart, files converged everywhere.
- **Two DCs down.** Reads and downloads succeeded. Uploads and library creation failed
  fast with 500 and published nothing. After restart, everything recovered with no
  backend restarts.
- **Cross-region use.** Login in each region, a session valid everywhere, read and write
  across regions, encrypted libraries needing unlock per region and share links. A library
  created at EU stored its bytes in the EU bucket; Asia's placement was not confirmed,
  because its test content was deduplicated.
- **Real `seaf-cli` across regions, at no added latency and at about 80 ms RTT.** Two
  clients on different regions with an OIDC-issued token: 200 KB in 11–44 s, 20 MB in
  about 20 s, 40 small files in about 45 s. **Not settled:** after both clients edited
  one file, neither showed a conflict copy within 45 s and one reported a network error,
  with a 503 on `PUT commit/HEAD`.

## Dependencies

- **Go, 10 reachable (`govulncheck`):** `golang.org/x/net` v0.56.0 (4, fixed in
  v0.60.0); `excelize` v2.11.0 (6, up from the 2 registered on 2026-10-21; fixes only
  in pseudo-versions).
- **Frontend, 40 production findings:** 2 critical, 16 high, 18 moderate, 4 low.
  The critical pair is `seafile-js` and `form-data`, addressed by **open PR #324**,
  with #325 for the nested dependency. Direct highs: `@seafile/sdoc-editor`, `postcss`
  and `socket.io-client`.

Reachability in the browser build was not analyzed. Merge #324 before building the
release, after a frontend build check.

## New findings

All are pre-existing. No severity of an existing issue changes here.

| Issue | Sev | Summary | Evidence |
|---|---|---|---|
| `ISSUE-BACKUP-RESTORE-MISSING-01` | P1, deferred for the pilot | No backup or restore exists | Repo search |
| `ISSUE-CASSANDRA-REPAIR-RUNBOOK-01` | P2 ops, multi-DC | No scheduled repair procedure | Repo search |
| `ISSUE-CASSANDRA-BOOTSTRAP-3DC-ROLE-01` | P1 deploy | First three-DC bootstrap fails before creating the application role | Reproduced 2 of 2 |
| `ISSUE-CASSANDRA-TIMEOUTS-WAN-01` | P1 availability | Shipped timeouts fail uploads under simulated WAN latency | Measured |
| `ISSUE-OIDC-PKCE-STATE-NODE-LOCAL-01` | HIGH, multi-instance | Login state is an in-process map | Reproduced across two regions |
| `ISSUE-HEALTH-PING-FALSE-OK-01` | P2 | `/ping` answers 200 through nginx with the backend down | Reproduced |
| `ISSUE-MIGRATOR-FIRST-BOOT-3DC-RESTARTS-01` | P2 | First boot on three DCs restarts repeatedly, then converges | Measured |
| `ISSUE-TAG-CLEANUP-COUNTER-BATCH-01` | P2 | Permanent delete always fails its tag cleanup | Reproduced, code read |
| `ISSUE-LOG-PRINTF-LEVEL-01` | P2 ops | Upload failures log at INFO, so level-based alerts miss them | Measured |
| `ISSUE-PROD-COMPOSE-NO-MEMORY-LIMIT-01` | P2 config | The admission budget falls back to the reference value | Measured in startup log |
| `ISSUE-SHARELINK-DIR-TRAILING-SLASH-404-01` | P2 | Links the UI emits for a shared folder return JSON 404 | Reproduced through both nginx hops |
| `ISSUE-DEPLOY-CENTRAL-NGINX-REFERENCE-01` | P2 docs | No reference central nginx config; rate limiting and optional nginx claims were stale | Repo search; stale text corrected here |
| `ISSUE-FRONTEND-SECURITY-HEADERS-01` | P3 | SPA responses have no security headers | Measured |
| `ISSUE-TEST-ENV-EXAMPLE-ACCOUNTS-AUTHORITY-01` | P3 test | The template cannot pass the suite | Reproduced |
| `ISSUE-TRACKED-BINARY-01` | P3 hygiene | A 49 MB `bin/sesamefs` from 2026-03 is tracked | `git ls-files` |

`.aws/`, removed by #326, held only an empty `credentials` and a blank `config`, so
history shows no credential there. A pattern scan of the tracked tree found no AWS,
GitHub, Slack or Stripe tokens and no private keys. That is not a full history audit.

## Operational notes

- **Users.** `OIDC_AUTO_PROVISION=false` refuses unknown users; measured:
  `user not found and auto-provisioning is disabled`. Provision through Accounts,
  whose runbook is missing (`ISSUE-ACCOUNTS-PROVISIONING-RUNBOOK-01`), or decide
  explicitly on auto-provisioning.
- **IdP address.** The OIDC client refuses issuers that resolve to private addresses,
  with no production override. An IdP reachable only on a private network cannot log
  anyone in. The rehearsal needed a non-private test range for this reason.
- **Monitoring.** `/metrics` and `/ready` exist, but no scrape or alert stack ships.
  At minimum alert on `/ready`, disk, and bucket growth.
- **Not present, acceptable for a pilot if accepted:** antivirus, persistent audit log
  (`ISSUE-AUDIT-TRAIL-INCOMPLETE-01`), CI, application rate limiting beyond the auth,
  share-link, ZIP and upload-link limiters.

## Not exercised

Real TLS; a real S3 provider; the real IdP; OnlyOffice; a real browser; desktop GUI,
SeaDrive or mobile clients; load; a backup or restore; a real WAN, or per-DC S3 latency;
race detector and mutation gates; suites with a GC-enabled daemon; OIDC suite with OIDC
enabled; the Go integration suite against the three-DC stack. Results from one Docker
host indicate failure modes, not real-network rates.

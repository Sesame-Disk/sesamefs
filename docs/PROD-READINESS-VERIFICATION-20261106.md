# Production Readiness Verification — first internal deployment

**Date:** 2026-11-06
**Source:** `main@a21f4203d`, exported with LF endings; no local changes
**Question:** is SesameFS ready for a first production deployment for internal
use, with destructive GC off, where issues are closed progressively from real use?

This is a dated snapshot. Status of record stays in
[KNOWN_ISSUES.md](./KNOWN_ISSUES.md); this document supersedes no gate. It closes
no issue, and the go-live decision belongs to the maintainer.

## Verdict

**Not yet, but the remaining work is short and mostly operational.** The tested
application path works on the production compose. A pilot with trusted internal
users is reasonable once items 1–5 below are done. This is not a general production
certification, and it supplies no evidence for external users.

### Required before storing data that matters

| # | Item | Why | Evidence |
|---|---|---|---|
| 1 | **Backup and a tested restore** ([`ISSUE-BACKUP-RESTORE-MISSING-01`](./KNOWN_ISSUES.md#issue-backup-restore-missing-01-no-backup-or-restore-procedure-exists)) | Nothing exists. The documented layout is RF 1 per DC, so losing a single-DC Cassandra node loses all metadata; the S3 blocks alone cannot rebuild libraries | No script or runbook in the repo; [roadmap §10](./V1-PRODUCTION-ROADMAP.md) still open |
| 2 | **Decide whether pilot data is disposable** | The first deploy freezes `001`; schema changes then go in `002+`. If the data must survive, "recreate the keyspace" stops being available and later fixes need an upgrade path. It also decides whether GC can ever activate on this environment, see [below](#gc-off-what-it-means) | [Schema contract](./DATABASE-GUIDE.md#v1-baseline-fold-2026-10-19), [first-production contract](./X1-CRITICAL-PATH.md#first-production-deployment-contract) |
| 3 | **Choose the topology** | See [Topology](#topology). The guide's default is multi-region, the path with the weakest current evidence | Dated three-API failures; no WAN measurement |
| 4 | **Decide on the two registered go-live blockers** | Both still reproduce in code. Trusted users reduce exposure; they do not remove it | [Below](#registered-go-live-blockers) |
| 5 | **Build the release from an exact commit, and set memory limits** | No CI exists, so images are built and pushed by hand. Merge #324 first. The production compose sets no memory limit | [Dependencies](#dependencies), [`ISSUE-PROD-COMPOSE-NO-MEMORY-LIMIT-01`](./KNOWN_ISSUES.md#issue-prod-compose-no-memory-limit-01-production-compose-sets-no-container-memory-limit) |

Also needed on the day, not defects: a user provisioning path, and a central nginx
configuration. See [Operational notes](#operational-notes).

### Registered go-live blockers

Both are pre-existing and were re-verified by reading `main`.

| Issue | Confirmed in code | Exposure in a trusted pilot |
|---|---|---|
| `ISSUE-SYNC-FSID-WORK-AMPLIFICATION-01` | `PackFS` does not deduplicate, reads one row per id and buffers the whole response; `parseBoundedIDList` accepts about 409k ids | Any user or leaked token with read access can exhaust node memory. Without a memory limit the blast radius includes Cassandra on the same host |
| `ISSUE-APIKEY-READ-SCOPE-UPLOADLINK-FILESHARE-01` | Upload-link creation and all three file-share mutations use bare `HasLibraryAccess`; upload-link update/delete compare only the creator | A read-scoped key can create or change links and shares up to the owner's authority |

Recommendation: accept both explicitly and in writing for a trusted pilot, fix them
next as separate small PRs, and do not admit external users until both are closed.
The trade-off is a known availability and least-privilege risk during the pilot.

### Topology

| Option | Benefit | Cost |
|---|---|---|
| **One region, one backend** (recommended for a pilot, if data is disposable) | Avoids the node-local and cross-DC failures below | No HA: a node loss is an outage until restored. Adding a DC to populated reference state needs a separately certified migration before GC can be considered (X2 stable-topology contract) |
| Three DCs from the first day | Final topology; no later DC-set change | Dated three-API runs of upload plus concurrent activity failed: SeafHTTP 3/3 and V2 2/3 with HTTP 500 on a single Docker host ([ledger](./E1-THREE-API-RESIDUALS.md), 2026-10-16, **not re-run here**). Per-block and HEAD LWTs use global `SERIAL`; WAN latency has never been measured. Needs scheduled `nodetool repair`, for which no runbook exists |

Do not place two backends behind one hostname. Three pieces of state are per process:
chunked uploads (`ISSUE-UPLOAD-CHUNK-MULTINODE-01`), desktop SSO
(`ISSUE-SSO-PENDING-TOKEN-NODE-LOCAL-01`), and web login PKCE state
(`ISSUE-OIDC-PKCE-STATE-NODE-LOCAL-01`, new). One backend per regional hostname does
not hit them.

### GC off: what it means

The kill switch held in this run: logs report `[GC] Garbage collection is disabled`,
status reports `enabled:false`, and `POST /admin/gc/run` answers 503.

- **Space is never reclaimed.** Measured: objects of a permanently deleted library
  remain in the bucket. Storage and metadata only grow, so monitor bucket size.
- **Delete does not erase bytes.** Permanent delete removes metadata rows, not
  objects. State this to users if any compliance expectation applies.
- **Quota still works.** Measured: deleting a 3 MB file took usage from 3,000,000 to 0.
- **Timed lifecycles do not run:** library trash purge, user and org hard delete,
  audit retention and `auto_delete_days`. Their UI text can overpromise
  (`ISSUE-AUTO-DELETE-EXPIRY-CONTRACT-01`).
- **The GC queue accumulates work with no drain.** Observed: `queue_size` 125 after
  the suites on a GC-off stack.
- **`GC_ENABLED=false` does not stop every destructive path.** `PermanentDeleteRepo`
  still runs, so the P1 restore race remains live
  (`ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`). A backup is the mitigation.
- **Activation later is an open question.** The first-production contract assumes
  no inherited candidates, repairs or attempts, and PRE-GC/A1 remain OPEN. Whether GC
  may activate on data accumulated by this pilot, across later releases, has not been
  assessed. If it must, that assessment joins the A1 gate; if data is disposable, the
  environment can be rebuilt instead.

The pilot is useful for that gate: the retention report in open PR #320 needs a
representative GC-off deployment to measure.

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
| Deploy rehearsal | **37 pass, 2 harness fails, both re-tested as pass** | [Below](#deploy-rehearsal) |
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
| `ISSUE-BACKUP-RESTORE-MISSING-01` | P1 go-live | No backup or restore exists | Repo search |
| `ISSUE-OIDC-PKCE-STATE-NODE-LOCAL-01` | HIGH, multi-instance | Login state is an in-process map | Code read, **not reproduced** |
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
SeaDrive or mobile clients; load or performance; a backup or restore; three-DC
application traffic or any WAN; race detector and mutation gates; suites with a
GC-enabled daemon; OIDC suite with OIDC enabled. Results from a single Docker host say
nothing about latency or failure behaviour in a real network.

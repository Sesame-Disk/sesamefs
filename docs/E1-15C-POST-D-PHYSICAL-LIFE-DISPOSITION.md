# E1-15C — post-D references, terminal retirement and P2 rematerialization

Frozen base: main@304f6da8f7c30e4ad816d17d061dcde6816c0854 (#272 merged).

## Question

E1-15A/B showed that current code can leave references on block L that were
written after exact COMMITTED D(P1). Do they endanger the physical life P1, or
any later legitimate life of L? Or is the demonstrated defect retention and
convergence? Concretely:

1. Do post-D `pub:` and a dead durable repair R block or revert terminal
   retirement of P1, or reinstall P1?
2. After P1 is TERMINAL, can a real rematerialization install P2 and publish
   a HEAD that depends only on P2 while R is still present?
3. Can L's later lives be collected once unreferenced while R is present?

## Contracts to reconcile

- D0 §13 ([GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md](./GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md#13-late-refs))
  is physical: after irreversible D, late `up:`/`pub:` "must not make P1 live
  again". It also says post-D refs "remain a contradiction/veto (CURRENT /
  TRANSITIONAL)". Current code forbids exactly that:
  `BlockPublicationLivenessGlobal` "must not be used as a post-COMMITTED
  contradiction read".
- The E1 ledger is stricter: no current operation may "durably add a P1
  reference" after D.

## Static analysis (main@304f6da8f)

- References are keyed by logical block L, not by physical life P. A pin on L
  after a new install pins that new life.
- R classifies by commit reachability. Its commit c1 is a definitive loser
  that never becomes an ancestor of HEAD, so R stays UNKNOWN indefinitely and
  renews on every visit.
- GC's destructive proof scans pending repairs for L at EACH_QUORUM. A dead R
  for L should therefore veto D of every later life of L in that org.
- Expected: P1 stays retired (no physical RED). The demonstrated defect is
  retention/convergence, with dead R as its unbounded part. Hypothesis only.

## Classification (frozen)

- P1 PRE-X1 confirmed (physical): any path that reinstalls P1, prevents or
  reverts its terminal retirement, or publishes a HEAD that depends on
  retired P1.
- FOLLOW-UP (retention/convergence): P1 stays retired, and the only defect is
  references, repairs or space retained, including a veto that blocks
  collection of later lives. The strict-E1 reading keeps its PRE-X1 severity
  until the contract is explicitly reconciled.
- OPEN/UNKNOWN: not reproduced and not positively excluded.

## Plan

1. Office/CreateFile, real Cassandra/SILO, existing e19 GC-OFF isolation;
   one owned org/library per leg. No production change. No CQL insert/delete
   of references or repairs, no harness candidate, no fabricated HEAD/P/D/P2.
2. Reuse the E1-15B subprocess writer, the E1-15A natural-D helper and the
   E1-15B conditional finalizer. Every leg records P1 and, where present, P2
   as exact `(storage_class, storage_key)`.
3. Legs:
   - `post-d-pub-terminal` (A): crash after staging after D. Productive
     continuation to TERMINAL with the post-D `pub:` present. Assert
     lifecycle TERMINAL, K1 absent, orphan/root absent, canonical still
     absent, the post-D `pub:` still present, HEAD unchanged.
   - `post-d-repair-terminal` (B): crash after queueing after D, then two
     productive UNKNOWN sweeps renew post-D. TERMINAL with `pub:` and R
     present; same assertions, R retained.
   - `p2-published-with-dead-repair` (C1): B after TERMINAL, then a real
     in-process CreateFile of the same content in the same library. Require
     201, P2 key ≠ P1 key, K1 absent, P2 canonical and bytes present, HEAD
     tree with the new file only, its fs: present, and no fs: for R's fs_id.
     Then a productive sweep of R: record classification and writes.
   - `dead-repair-blocks-unreferenced-p2` (C2): B after TERMINAL, then a
     real rematerialization whose writer dies after staging (in-process
     abrupt stop, E1-14 pattern). Every remaining real reference (P2 `up:`,
     P2 `pub:`, R's renewed pin) is shortened through its productive write
     API and retired by Cassandra TTL, giving global EQ zero refs. Owned-scope
     Phase 0 creates the natural P2 candidate; Phase 1 enqueues it; the
     productive worker runs. Record whether D(P2) is vetoed, the candidate is
     retained and P2 stays present, then a further R sweep.
4. Teardown: E1-11/12/13/14/15A verifiers, now applied to the current
   target (P2 where installed), plus explicit P1/K1 absence.
5. Completeness gate, gate negatives, race repeats, vets, standard
   go-all-test, cleanup/quota checks, scoped audit, PR.

## Out of scope

Fences, `AddPublishAttemptReferences`, repair worker, tables/generations/
leases/witnesses, GC Phase 0–6, hot-path checks, Sync/SeafHTTP/cross-repo,
GC activation, and any rewrite of D0. The reconciliation is a written
proposal for an explicit decision, not a change to the contract.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.

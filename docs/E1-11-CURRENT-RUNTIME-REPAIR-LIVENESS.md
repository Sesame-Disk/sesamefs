# E1-11 / W2-11: current-runtime repair liveness

## Frozen plan

Base: main@4f7c29923fb6022c4b7e48996b50918011781679 (#264 merged).
Branch: codex/e1-11-current-runtime-repair-liveness.

Evidence first. One real Office CreateFile, one canonical SHA256 block and
exact P1/K1, plaintext, isolated org/repo. No repair row, HEAD, P or D is
manufactured by CQL. Real TTL expiry uses the existing reference API with a
short test TTL. Explicit candidate injection exercises worker safety, not
candidate discovery (W2-14). Existing e19 keyspace/GC-OFF backend isolates the
schedule; the standard Compose daemon configuration remains unchanged.

Required legs:
- normal repair control;
- expiry during a real classifier HEAD read;
- expiry after a real REACHABLE classifier, before promotion;
- clean UNKNOWN retained repair from a real interrupted writer that lost HEAD;
- native wire loss during classification of an applied HEAD;
- native wire loss in the destructive pending-repair scan.

The interrupted not-yet-published UNKNOWN control is explicitly distinct from
an applied HEAD with an unavailable classifier. Neither injected outcomes nor
process-wide classifier replacement is evidence.

Capture exact HEAD/tree/fs_id, canonical P/K, original pub/up TTL, repair
identity/staged IDs, all refs, worker candidate/claim/lifecycle/recovery roots
and bytes. REACHABLE must acknowledge fs: before deleting repair; retry must
be no-op. UNKNOWN/errors retain repair and must not authorize a new D.

Mutation: remove only the destructive pending-repair lookup in a disposable
container source copy. Run the identical post-classifier reachable schedule;
require a real COMMITTED D as the failure reason, not a build/selector/early
assertion failure. Restore by discarding the container. Test cleanup must work
on both GREEN and RED, including exact K1 and owned repair rows.

Add an independent evidence gate with missing-leg, filtered-selector and
unavailable-backend negative controls. Repeat under race. Run W2/G4/G5 gates
and standard Docker go-all-test, then audit source, scope, cleanup and claims.

No runtime fix unless the unmutated schedule produces a RED. No TTL policy,
schema, scheduler, renewal ordering, health gate, upload cost or GC activation
changes. W2-12/13/14, concurrent cleanup, other funnels, retention/Phase5/6,
PC-D1B.5 and W2-10 remain OPEN. Any W2-11 disposition applies only to the
covered current-version non-expiring repair / destructive pre-D contract;
a late repair cannot revoke an already COMMITTED D. Results pending.

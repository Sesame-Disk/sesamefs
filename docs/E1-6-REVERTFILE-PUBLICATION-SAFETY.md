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

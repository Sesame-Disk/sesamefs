// Package publication is the common spine of the block-publication protocol
// characterized in docs/PUBLICATION-PROTOCOL-CHARACTERIZATION.md (PC-0). It
// holds the vocabulary every funnel already shares — publication attempt
// identity, the tri-state target HEAD outcome, the independently established
// attempt settlement disposition, and the opaque evidence boundary between an
// adapter and the coordinator — plus the PublicationCoordinator type itself.
//
// PC-1 status (skeleton only):
//
//   - No productive funnel imports this package. CreateFile, UploadFile,
//     CreateFileFromBlocks, cross-repo copy/move, OnlyOffice, SeafHTTP single
//     and multiblock, Sync HEAD promotion and auto-merge, and the
//     content-resurrection paths run exactly the sequences PC-0 inventoried;
//     internal/db's PC-1 source contracts fail as soon as one of them starts
//     calling in here.
//   - The coordinator executes nothing. PC-0 §4 demonstrated only a partial
//     order (stage < durable repair < HEAD, readiness optional and also before
//     HEAD, with the repair/readiness order funnel-specific). There is
//     deliberately no Publish/Stage/Repair/Head/Settle method that would
//     force a universal sequence the characterization did not prove.
//   - HEAD outcome and attempt settlement are separate dimensions. The common
//     validator rejects UNKNOWN cleanup/promotion and KNOWN_LOSER promotion;
//     adapters still own the evidence for promotion or exact attempt cleanup.
//   - PC-D1 selects a certified baseline frontier as the inherited-continuity
//     owner. PublishableInput and DependencyEvidence remain opaque, and
//     WorkSetScopeNewlyLive remains the sole declared incremental scope
//     ("newly live on the HEAD being published", the R3
//     LogicalPositiveBlockDelta shape). It is complete only when the durable
//     baseline witness is valid; otherwise baseline certification is required
//     before PC-2. No new scope value or runtime implementation is introduced
//     by the decision. The baseline contract is GC-aware per dependency:
//     resolve/capture exact physical incarnation P, establish non-expiring
//     current-library liveness, then revalidate exact P plus current GC
//     authority. A bounded-TTL pin only bridges certification and cannot
//     justify the witness; a late liveness write cannot revoke a zero-proof
//     already won by GC. Legacy deterministic locators must be rematerialized
//     to minted P before certification, and all coexisting HEAD writers and
//     frontier LWTs require one compatible global SERIAL Paxos domain.
//   - The existing HEAD classifiers are not migrated: v2's sentinel errors
//     (ErrLibraryHeadConflict, ErrLibraryHeadPublicationUnknown), Sync's
//     errSyncHeadCASUncertain / syncHeadConflictError, and the initializer's
//     InitialHeadOutcome keep their current classification. Unifying them is
//     a behavior-sensitive change that gets its own PR (PC-0 §9, §15 "HEAD
//     classify split").
//
// Multi-DC design constraints (PC-0 §14, binding from PC-1 on):
//
//   - The coordinator is stateless with respect to process ownership and runs
//     identically in any datacenter: no leader, no home DC, no in-memory
//     publication ownership, no process-local mutex as authority. Every
//     durable decision keeps coming from Cassandra and the protocol
//     (attempt-local pub: references, durable repair intent, the HEAD LWT),
//     never from a PublicationCoordinator value.
//   - This package imports nothing but the standard library and performs no
//     I/O. It adds no table, no CQL, no consistency level, and no TTL.
//
// This package does not close W2, R31, or X1.
package publication

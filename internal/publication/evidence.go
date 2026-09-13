package publication

// WorkSetScope names which physical dependencies a piece of dependency
// evidence claims to cover. PC-D1 keeps today's candidate as the only declared
// scope: newly-live dependencies are admissible incrementally only when a
// durable certified-baseline witness is valid. Inherited dependencies without
// that witness require baseline certification before PC-2; no new scope value
// or signature is needed for the decision.
type WorkSetScope string

const (
	// WorkSetScopeNewlyLive covers the dependencies the HEAD being published
	// will newly live on: the new HEAD's reachable blocks minus the old HEAD's,
	// the shape of R3's LogicalPositiveBlockDelta. It is the incremental scope
	// selected by PC-D1, not a complete-work-set claim without a valid certified
	// baseline witness.
	WorkSetScopeNewlyLive WorkSetScope = "newly-live"
)

// DependencyEvidence is the opaque boundary between an adapter's proof work
// (classification, own liveness, captured ExpectedP) and the coordinator. It
// deliberately exposes no block list: PC-D1 resolves the responsibility
// boundary with a certified baseline frontier, while implementation of its
// durable witness remains a later prerequisite and does not alter this API.
// That implementation must resolve/capture exact physical incarnation P,
// establish non-expiring current-library liveness, and perform a fresh exact-P
// plus GC-authority revalidation before it accepts each dependency; a bounded-
// TTL pin is only a certification bridge and cannot justify the witness, while
// a late liveness write does not revoke destructive authority already won by GC.
// Legacy deterministic locators must be rematerialized to minted P before
// certification, and the frontier requires one compatible global SERIAL domain
// for every coexisting HEAD writer and frontier LWT.
type DependencyEvidence interface {
	// WorkSetScope reports which scope this evidence claims to cover.
	WorkSetScope() WorkSetScope
}

// PublishableInput is the candidate coordinator input (PC-0 §2, §14): an
// attempt identity plus the adapter's dependency evidence. It carries no
// classification vocabulary because classified input is not publishable —
// UNPROVENANCED and ERROR are rejected by the adapter, BORROWED must first
// acquire durable own liveness — and no productive adapter implements it in
// PC-1.
type PublishableInput interface {
	Attempt() AttemptIdentity
	Dependencies() DependencyEvidence
}

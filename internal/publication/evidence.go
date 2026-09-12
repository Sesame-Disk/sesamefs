package publication

// WorkSetScope names which physical dependencies a piece of dependency
// evidence claims to cover. PC-1 declares only today's candidate. Widening the
// work set — for example to inherited dependencies whose continuity was never
// proven (ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01) — is an additive new
// value plus a documented decision, not a change to any existing signature.
type WorkSetScope string

const (
	// WorkSetScopeNewlyLive covers the dependencies the HEAD being published
	// will newly live on: the new HEAD's reachable blocks minus the old HEAD's,
	// the shape of R3's LogicalPositiveBlockDelta. It is the CANDIDATE scope,
	// not a claim that it is the complete work set.
	WorkSetScopeNewlyLive WorkSetScope = "newly-live"
)

// DependencyEvidence is the opaque boundary between an adapter's proof work
// (classification, own liveness, captured ExpectedP) and the coordinator. It
// deliberately exposes no block list: what the adapter must hand over is
// exactly the open question above, and this interface must stay implementable
// by a wider work set without breaking anyone.
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

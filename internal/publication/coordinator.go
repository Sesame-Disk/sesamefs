package publication

// PublicationCoordinator is the common publication boundary PC-0 §14
// recommended. It is a value with no fields on purpose: it owns no lock, no
// map of in-flight attempts, no home datacenter, and no process identity, so
// any node in any DC can construct one and reach the same validation result
// from the same explicit evidence. Durable authority stays in Cassandra and
// the protocol.
//
// PC-1 gives it exactly one capability: validating an already classified
// SettlementDecision. It does not derive attempt settlement from HeadOutcome,
// and it has no Publish, Stage, Repair, Head, or Settle method. No funnel is
// migrated. internal/db's PC-1 source contract inventories this method set;
// adding a method is a deliberate, reviewed step of a later PC.
type PublicationCoordinator struct{}

// NewPublicationCoordinator returns the stateless coordinator value.
func NewPublicationCoordinator() PublicationCoordinator {
	return PublicationCoordinator{}
}

// ValidateSettlement checks the common fail-closed settlement invariants. The
// adapter remains responsible for producing the explicit decision from
// durable, attempt-scoped evidence.
func (PublicationCoordinator) ValidateSettlement(decision SettlementDecision) error {
	return decision.Validate()
}

package publication

// PublicationCoordinator is the common publication boundary PC-0 §14
// recommended. It is a value with no fields on purpose: it owns no lock, no
// map of in-flight attempts, no home datacenter, and no process identity, so
// any node in any DC can construct one and reach the same decisions from the
// same durable state. Durable authority stays in Cassandra and the protocol.
//
// PC-1 gives it exactly one capability, the pure settlement rule. It has no
// Publish, Stage, Repair, Head, or Settle method: PC-0 did not demonstrate a
// universal order beyond stage < durable repair < HEAD, and no funnel is
// migrated. internal/db's PC-1 source contract inventories this method set;
// adding a method is a deliberate, reviewed step of a later PC.
type PublicationCoordinator struct{}

// NewPublicationCoordinator returns the (stateless) coordinator value.
func NewPublicationCoordinator() PublicationCoordinator {
	return PublicationCoordinator{}
}

// SettlementFor maps a classified HEAD outcome to its settlement disposition.
// It performs no I/O and holds no state; see DispositionFor.
func (PublicationCoordinator) SettlementFor(outcome HeadOutcome) (SettlementDisposition, error) {
	return DispositionFor(outcome)
}

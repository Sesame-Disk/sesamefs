package publication

// Phase names the control-flow phases of the observed publication kernel
// (PC-0 §4) for labels, logs, and audits. It carries no order: PC-0 proved
// only the partial order stage < durable repair < HEAD, with readiness
// optional and its position relative to repair funnel-specific
// (CreateFileFromBlocks: repair then final exact-P; Sync: readiness then
// repair). Do not add a Next() or a universal sequence here.
type Phase string

const (
	// PhaseStage: attempt-local pub: references are written.
	PhaseStage Phase = "stage"
	// PhaseRepairIntent: the durable repair row that can outlive the request.
	PhaseRepairIntent Phase = "repair-intent"
	// PhaseReadiness: funnel-specific readiness / final exact-P revalidation,
	// when the funnel has one; also before HEAD.
	PhaseReadiness Phase = "readiness"
	// PhaseHead: the HEAD LWT and its classification.
	PhaseHead Phase = "head"
	// PhaseSettlement: promote, retain, or exact attempt cleanup.
	PhaseSettlement Phase = "settlement"
)

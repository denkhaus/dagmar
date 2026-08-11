// Package controller contains dagmar's Kubernetes reconciliation logic.
//
// pipeline_fsm.go defines the orchestration pipeline as a declarative finite-state
// machine (ADR-0026). The FSM replaces the four hand-written advance* functions
// (advanceCoding/Gating/Reviewing/Adjudicating) with a single generic reconciliation
// loop that fires FSM events based on observed Sub-Run outcomes.
//
// The FSM is constructed via looplab/fsm (v1.0.3). States and event names are
// constants — never inline strings — so they are discoverable and lint-safe.
package controller

import (
	"github.com/looplab/fsm"
)

// Pipeline states (FSM node names). These are the values stored in
// RunStatus.PipelinePhase and used as FSM state identifiers.
// ALL FSM string identifiers in the package MUST use these constants.
const (
	StateCoding       = "coding"
	StateGating       = "gating"
	StateReviewing    = "reviewing"
	StateAdjudicating = "adjudicating"
	StateEscalated    = "escalated"
	StateDone         = "done"
)

// FSM event names. These describe the transitions that drive the pipeline forward.
// Fired by the generic reconcileOrchestration based on observed Sub-Run outcomes.
const (
	EventCoderSucceeded     = "coder_succeeded"
	EventCoderFailed        = "coder_failed"
	EventGateGreen          = "gate_green"
	EventGateRed            = "gate_red"
	EventReviewerApprove    = "reviewer_approve"
	EventReviewerVeto       = "reviewer_veto"
	EventAdjudicatorResolve = "adjudicator_resolved"
	EventAdjudicatorFail    = "adjudicator_failed"
	EventMaxRetriesReached  = "max_retries_reached"
)

// newPipelineFSM constructs the orchestration FSM for a given Run. The FSM encodes
// the complete pipeline topology (ADR-0023 D5, revised ADR-0026):
//
//	coding → gating → reviewing → done
//	    ↑       ↓        ↓
//	  revise  revise   adjudicating → done
//	                       ↓
//	                 escalated (unresolvable)
//
// Callbacks are registered per enter-state: each creates or observes the Sub-Run
// appropriate for that state. The callbacks close over the reconcile context
// (ctx, run, project, wf, client) to perform their K8s operations.
//
// The FSM is reconstructed on every reconcile (from the persisted PipelinePhase
// in RunStatus), consistent with K8s reconciliation semantics: state lives in
// etcd, the FSM is the transition logic applied to that state.
func newPipelineFSM() *fsm.FSM {
	return fsm.NewFSM(
		StateCoding,
		fsm.Events{
			// Coder outcomes
			{Name: EventCoderSucceeded, Src: []string{StateCoding}, Dst: StateGating},
			{Name: EventCoderFailed, Src: []string{StateCoding}, Dst: StateCoding}, // revise loop
			{Name: EventMaxRetriesReached, Src: []string{StateCoding}, Dst: StateEscalated},

			// Gate outcomes
			{Name: EventGateGreen, Src: []string{StateGating}, Dst: StateReviewing},
			{Name: EventGateRed, Src: []string{StateGating}, Dst: StateCoding}, // revise loop
			{Name: EventMaxRetriesReached, Src: []string{StateGating}, Dst: StateEscalated},

			// Reviewer outcomes
			{Name: EventReviewerApprove, Src: []string{StateReviewing}, Dst: StateDone},
			{Name: EventReviewerVeto, Src: []string{StateReviewing}, Dst: StateAdjudicating},
			{Name: EventMaxRetriesReached, Src: []string{StateReviewing, StateAdjudicating}, Dst: StateEscalated},

			// Adjudicator outcomes
			{Name: EventAdjudicatorResolve, Src: []string{StateAdjudicating}, Dst: StateDone},
			{Name: EventAdjudicatorFail, Src: []string{StateAdjudicating}, Dst: StateEscalated},
		},
		fsm.Callbacks{},
	)
}

// pipelineEventForResult maps a Sub-Run outcome (succeeded/failed) + pipeline state
// to the FSM event that should be fired. This is the bridge between the observed
// K8s state and the FSM transition.
//
// Returns the event name and whether the round counter should be incremented
// (for revise-loop tracking).
func pipelineEventForResult(state string, subRunSucceeded bool, gateGreen bool, verdictApproved bool) (event string, revise bool) {
	switch state {
	case StateCoding:
		if subRunSucceeded {
			return EventCoderSucceeded, false
		}
		return EventCoderFailed, true

	case StateGating:
		if gateGreen {
			return EventGateGreen, false
		}
		return EventGateRed, true

	case StateReviewing:
		if subRunSucceeded && verdictApproved {
			return EventReviewerApprove, false
		}
		return EventReviewerVeto, false

	case StateAdjudicating:
		if subRunSucceeded {
			return EventAdjudicatorResolve, false
		}
		return EventAdjudicatorFail, false
	}
	return "", false
}

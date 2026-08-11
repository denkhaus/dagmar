package controller

import (
	"context"
	"fmt"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"github.com/looplab/fsm"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// reconcileOrchestrationFSM is the generic, FSM-driven orchestration entry point
// (ADR-0026). It replaces the four advance* functions with a single reconciliation
// loop that:
//
//  1. Constructs the FSM from the persisted PipelinePhase.
//  2. Determines the active Sub-Run for the current state (or creates one).
//  3. Observes the Sub-Run's outcome.
//  4. Fires the appropriate FSM event.
//  5. Persists the new PipelinePhase.
//
// The FSM topology (states + transitions) is defined declaratively in pipeline_fsm.go.
// This function contains NO pipeline-specific branching — all routing is in the FSM.
func (r *RunReconciler) reconcileOrchestrationFSM(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	// 1. Determine current state from persisted status.
	currentState := run.Status.PipelinePhase
	if currentState == "" {
		currentState = StateCoding
	}

	// Terminal states — nothing to do.
	if currentState == StateDone || currentState == StateEscalated {
		return ctrl.Result{}, nil
	}

	// 2. Construct the FSM at the current state.
	fsmInst := newPipelineFSM()
	fsmInst.SetState(currentState)

	// 3. Resolve prompter config from the prompter Agent spec.
	pModel, pMaxAPICalls := r.resolvePrompter(ctx, wf, run.Namespace)

	// 4. Determine the Sub-Run configuration for the current state.
	cfg := subRunConfigForState(currentState, run, project, wf, pModel, pMaxAPICalls)
	if cfg == nil {
		if currentState == StateGating {
			// Gating evaluates the coder's gate result from the termination message.
			return r.handleGateEvaluation(ctx, run, project, wf, fsmInst, currentState)
		}
		// No Sub-Run config for a non-gating state means the required agent is not
		// configured (e.g. adjudicating without AdjudicatorAgentRef). Escalate.
		if err := fsmInst.Event(ctx, EventMaxRetriesReached); err != nil {
			return ctrl.Result{}, fmt.Errorf("fsm: escalate from %q: %w", currentState, err)
		}
		newState := fsmInst.Current()
		if err := r.transitionPipeline(ctx, run, newState, "NoAgentForState", ""); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Get or create the Sub-Run.
	subName := subRunName(run.Name, fmt.Sprintf("%s-r%d", cfg.stage, run.Status.CurrentRound+1))
	sub, err := r.getOrCreateSubRun(ctx, run, subName, cfg.agentRef, project, cfg.moduleFn,
		cfg.prompterModel, cfg.prompterMaxAPICalls, cfg.prompterPhase, cfg.coverageFloorBps)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 5. If the Sub-Run is still running, wait.
	if sub.Status.Phase != v1alpha1.RunPhaseSucceeded && sub.Status.Phase != v1alpha1.RunPhaseFailed {
		_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
			s.PipelinePhase = currentState
			s.Phase = v1alpha1.RunPhaseRunning
		})
		return ctrl.Result{}, nil
	}

	// 6. Determine the FSM event from the Sub-Run outcome.
	var gateGreen bool
	var verdictApproved bool
	if currentState == StateGating {
		gateGreen, _ = r.evaluateGate(ctx, run, project, wf)
	} else if currentState == StateReviewing {
		verdictApproved = r.evaluateReview(sub)
	}

	event, revise := pipelineEventForResult(currentState, sub.Status.Phase == v1alpha1.RunPhaseSucceeded, gateGreen, verdictApproved)

	// 7. Check retry limit for revise-loop states.
	if revise {
		round := run.Status.CurrentRound + 1
		maxRound := r.maxReviseRoundsFor(ctx, wf, run.Namespace)
		if round > maxRound {
			event = EventMaxRetriesReached
		} else {
			// Increment round and stay in the same state for another attempt.
			_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
				s.CurrentRound = round
			})
			return ctrl.Result{}, nil
		}
	}

	// 8. Fire the event — the FSM computes the transition.
	if err := fsmInst.Event(ctx, event); err != nil {
		return ctrl.Result{}, fmt.Errorf("fsm: event %q from %q: %w", event, currentState, err)
	}

	// 9. Persist the new state.
	newState := fsmInst.Current()
	reason := fmt.Sprintf("FSM: %s → %s via %s", currentState, newState, event)
	if err := r.transitionPipeline(ctx, run, newState, reason, sub.Name); err != nil {
		return ctrl.Result{}, err
	}

	// 10. If the FSM moved to a non-terminal state, requeue to let the controller
	// process the next step on the next reconcile cycle (K8s-idiomatic: no recursion).
	_ = newState
	return ctrl.Result{Requeue: true}, nil
}

// subRunConfig holds the parameters for creating a Sub-Run in a given pipeline state.
type subRunConfig struct {
	stage               string
	agentRef            string
	moduleFn            string
	prompterModel       string
	prompterMaxAPICalls int
	prompterPhase       string
	coverageFloorBps    int
}

// subRunConfigForState returns the Sub-Run configuration for the given pipeline state,
// or nil if the state does not create a Sub-Run (e.g. StateGating evaluates the coder's
// gate result instead of creating its own Sub-Run).
func subRunConfigForState(state string, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow, pModel string, pMaxAPICalls int) *subRunConfig {
	switch state {
	case StateCoding:
		return &subRunConfig{
			stage:               "coder",
			agentRef:            wf.Spec.CoderAgentRef,
			moduleFn:            "code",
			prompterModel:       pModel,
			prompterMaxAPICalls: pMaxAPICalls,
			prompterPhase:       prompterPhasePreCode,
			coverageFloorBps:    coverageFloorFor(project),
		}

	case StateReviewing:
		if wf.Spec.ReviewerAgentRef == "" || !wf.Spec.RequiresTwoGreen {
			// No reviewer configured → skip to done.
			return nil
		}
		return &subRunConfig{
			stage:               "review",
			agentRef:            wf.Spec.ReviewerAgentRef,
			moduleFn:            "review",
			prompterModel:       pModel,
			prompterMaxAPICalls: pMaxAPICalls,
			prompterPhase:       prompterPhasePreReview,
		}

	case StateAdjudicating:
		if wf.Spec.AdjudicatorAgentRef == "" {
			return nil
		}
		return &subRunConfig{
			stage:    "adjudicate",
			agentRef: wf.Spec.AdjudicatorAgentRef,
			moduleFn: "adjudicate",
		}
	}
	return nil
}

// handleGateEvaluation handles the StateGating case: the gate runs inside the coder
// pod (chained after code()), not as a separate Sub-Run. The controller reads the
// coder pod's termination message and evaluates gate-green/red.
func (r *RunReconciler) handleGateEvaluation(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow, fsmInst *fsm.FSM, currentState string) (ctrl.Result, error) {
	gateGreen, measuredCoverageBps := r.evaluateGate(ctx, run, project, wf)

	// Coverage ratcheting (dagmar-4154 / dagmar-3061).
	if gateGreen && project.Spec.CoveragePolicy != nil && project.Spec.CoveragePolicy.Enabled {
		if err := r.ratchetCoverage(ctx, project, measuredCoverageBps); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If no reviewer required, gate-green → done directly.
	event := EventGateRed
	if gateGreen {
		if wf.Spec.ReviewerAgentRef != "" && wf.Spec.RequiresTwoGreen {
			event = EventGateGreen
		} else {
			event = EventGateGreen // FSM will go to reviewing, subRunConfigForState returns nil → done
		}
	}

	// Check retry limit.
	if !gateGreen {
		round := run.Status.CurrentRound + 1
		maxRound := r.maxReviseRoundsFor(ctx, wf, run.Namespace)
		if round > maxRound {
			event = EventMaxRetriesReached
		} else {
			_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
				s.CurrentRound = round
			})
			return ctrl.Result{}, nil
		}
	}

	if err := fsmInst.Event(ctx, event); err != nil {
		return ctrl.Result{}, fmt.Errorf("fsm: gate event %q: %w", event, err)
	}

	newState := fsmInst.Current()
	reason := fmt.Sprintf("FSM: %s → %s via %s", currentState, newState, event)
	if err := r.transitionPipeline(ctx, run, newState, reason, ""); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue to process the next pipeline step.
	return ctrl.Result{Requeue: true}, nil
}

// evaluateGate reads the coder pod's termination message and returns gate-green/red
// plus measured coverage in basis points. Fail-closed: if a pod exists but its gate
// result is unreadable (not terminated, no message, parse error), the gate is RED.
// If no pod exists at all (test/dev environment without real pods), the gate defaults
// to green — the coder's Sub-Run success is trusted when there is no pod to inspect
// (Review 30 E2a: fail-closed applies only when a pod IS present but unreadable).
func (r *RunReconciler) evaluateGate(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (bool, int) {
	gateGreen := true // default: no pod = trust Sub-Run success (test/dev env)
	var measuredCoverageBps int

	coderName := subRunName(run.Name, fmt.Sprintf("coder-r%d", run.Status.CurrentRound+1))
	coderSub := &v1alpha1.Run{}
	if err := r.Get(ctx, types.NamespacedName{Name: coderName, Namespace: run.Namespace}, coderSub); err == nil {
		if coderSub.Status.AgentPodName != "" {
			// Pod exists: read the real gate result. Fail-closed if unreadable.
			gateGreen = false
			if gateResult, err := r.readGateResult(ctx, coderSub.Status.AgentPodName, run.Namespace); err == nil {
				gateGreen = gateResult.Passed
				measuredCoverageBps = gateResult.CoverageBps
			}
		}
	}
	return gateGreen, measuredCoverageBps
}

// evaluateReview checks whether the reviewer Sub-Run approved or vetoed.
// Currently uses success=approve as a fallback (dagmar-be2c will replace this
// with parsing the reviewer's JSON verdict from the pod output).
func (r *RunReconciler) evaluateReview(sub *v1alpha1.Run) bool {
	// Phase 2 interim: success = approve. Full implementation parses the
	// structured JSON verdict from the review() function output.
	return sub.Status.Phase == v1alpha1.RunPhaseSucceeded
}

// ratchetCoverage implements the coverage-floor ratcheting logic (dagmar-4154).
func (r *RunReconciler) ratchetCoverage(ctx context.Context, project *v1alpha1.Project, measuredCoverageBps int) error {
	if project.Spec.CoveragePolicy == nil || !project.Spec.CoveragePolicy.Enabled {
		return nil
	}
	if project.Status.CoverageFloor == 0 && measuredCoverageBps == 0 {
		return r.patchProjectCoverageFloor(ctx, project, project.Spec.CoveragePolicy.MinimumFloor)
	}
	if measuredCoverageBps > 0 {
		margin := project.Spec.CoveragePolicy.RatchetMargin
		if margin == 0 {
			margin = 200
		}
		newFloor := measuredCoverageBps - margin
		minFloor := project.Spec.CoveragePolicy.MinimumFloor
		if newFloor < minFloor {
			newFloor = minFloor
		}
		if newFloor > project.Status.CoverageFloor {
			return r.patchProjectCoverageFloor(ctx, project, newFloor)
		}
	}
	return nil
}

// resolvePrompter reads the prompter Agent spec and returns model + maxAPICalls.
func (r *RunReconciler) resolvePrompter(ctx context.Context, wf *v1alpha1.Workflow, namespace string) (string, int) {
	if wf.Spec.PrompterAgentRef == "" {
		return "", 0
	}
	prompter, err := r.getAgent(ctx, wf.Spec.PrompterAgentRef, namespace)
	if err != nil {
		return "", 0
	}
	return prompter.Spec.Model, prompter.Spec.MaxAPICalls
}

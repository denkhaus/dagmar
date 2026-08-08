package controller

import (
	"context"
	"fmt"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Pipeline phases (ADR-0016 §3).
const (
	PipelineCoder     = "coder"
	PipelineReview    = "review"
	PipelineDone      = "done"
	PipelineEscalated = "escalated"
)

// reconcileOrchestration drives an orchestration Run through the pipeline:
// coder → review → done (or revise loop → escalated).
//
// The controller creates atomic Sub-Runs for each stage and watches their
// status to decide transitions. Merge is a controller action performed when
// two-green is reached (ADR-0006) — not a Sub-Run.
//
// Phase 2 incremental: the pipeline is coder → review with a revise loop.
// Gate evaluation (QualityGate) happens on the coder's workspace post-Loop
// (ADR-0020 D3) — for now, gate-green is assumed when the coder Sub-Run
// succeeds. Full gate evaluation is a follow-up.
func (r *RunReconciler) reconcileOrchestration(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project) (ctrl.Result, error) {
	// Fetch the Workflow template.
	wf := &v1alpha1.Workflow{}
	if err := r.Get(ctx, types.NamespacedName{Name: run.Spec.WorkflowRef, Namespace: run.Namespace}, wf); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, r.failRun(ctx, run, "WorkflowNotFound",
				fmt.Sprintf("referenced Workflow %q not found", run.Spec.WorkflowRef))
		}
		return ctrl.Result{}, err
	}

	// Validate the Workflow's agent refs exist (fail fast).
	for _, ref := range []string{wf.Spec.CoderAgentRef, wf.Spec.ReviewerAgentRef} {
		if ref == "" {
			continue // reviewer is optional
		}
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: run.Namespace}, &v1alpha1.Agent{}); err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{}, r.failRun(ctx, run, "WorkflowAgentNotFound",
					fmt.Sprintf("Workflow %q references Agent %q not found", wf.Name, ref))
			}
			return ctrl.Result{}, err
		}
	}

	phase := run.Status.PipelinePhase
	if phase == "" {
		phase = PipelineCoder
	}

	switch phase {
	case PipelineCoder:
		return r.advanceCoder(ctx, run, project, wf)
	case PipelineReview:
		return r.advanceReview(ctx, run, project, wf)
	case PipelineDone, PipelineEscalated:
		// Terminal — nothing to do.
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, r.failRun(ctx, run, "UnknownPipelinePhase",
			fmt.Sprintf("unknown PipelinePhase %q", phase))
	}
}

// advanceCoder creates or watches the coder Sub-Run.
func (r *RunReconciler) advanceCoder(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	subName := subRunName(run.Name, fmt.Sprintf("coder-r%d", run.Status.CurrentRound+1))
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.CoderAgentRef, project, "code")
	if err != nil {
		return ctrl.Result{}, err
	}

	if sub.Status.Phase == v1alpha1.RunPhaseSucceeded {
		// Coder succeeded → advance to review (if reviewer configured) or done.
		if wf.Spec.ReviewerAgentRef != "" && wf.Spec.RequiresTwoGreen {
			// Transition to review, then immediately create the reviewer Sub-Run.
			if err := r.transitionPipeline(ctx, run, PipelineReview, "CoderSucceededAwaitingReview", sub.Name); err != nil {
				return ctrl.Result{}, err
			}
			return r.advanceReview(ctx, run, project, wf)
		}
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineDone, "CoderSucceeded", sub.Name)
	}
	if sub.Status.Phase == v1alpha1.RunPhaseFailed {
		// Coder failed → revise loop or escalate.
		round := run.Status.CurrentRound + 1
		maxRound := 3 // default; could read from QualityGate spec
		if round > maxRound {
			return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineEscalated,
				"MaxReviseRoundsExceeded", sub.Name)
		}
		// Revise: reset to coder with incremented round.
		return ctrl.Result{}, r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
			s.PipelinePhase = PipelineCoder
			s.CurrentRound = round
		})
	}
	// Still running — wait.
	_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = PipelineCoder
		s.Phase = v1alpha1.RunPhaseRunning
	})
	return ctrl.Result{}, nil
}

// advanceReview creates or watches the reviewer Sub-Run.
func (r *RunReconciler) advanceReview(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	subName := subRunName(run.Name, fmt.Sprintf("review-r%d", run.Status.CurrentRound+1))
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.ReviewerAgentRef, project, "code")
	if err != nil {
		return ctrl.Result{}, err
	}

	if sub.Status.Phase == v1alpha1.RunPhaseSucceeded {
		// Reviewer succeeded → two-green reached → done.
		// (Full implementation: parse reviewer output for approve/veto.
		// For now, success = approve.)
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineDone,
			"TwoGreenReached", sub.Name)
	}
	if sub.Status.Phase == v1alpha1.RunPhaseFailed {
		// Reviewer failed → revise loop or escalate.
		round := run.Status.CurrentRound + 1
		maxRound := 3
		if round > maxRound {
			return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineEscalated,
				"MaxReviseRoundsExceeded", sub.Name)
		}
		return ctrl.Result{}, r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
			s.PipelinePhase = PipelineCoder
			s.CurrentRound = round
		})
	}
	_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = PipelineReview
		s.Phase = v1alpha1.RunPhaseRunning
	})
	return ctrl.Result{}, nil
}

// getOrCreateSubRun creates an atomic Sub-Run if it doesn't exist, then returns it.
// The Sub-Run carries ModuleFunction + AgentRef + ParentRun.
func (r *RunReconciler) getOrCreateSubRun(ctx context.Context, parent *v1alpha1.Run, name string, agentRef string, project *v1alpha1.Project, fn string) (*v1alpha1.Run, error) {
	sub := &v1alpha1.Run{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: parent.Namespace}, sub)
	if err == nil {
		return sub, nil // already exists
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}
	sub = &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: parent.Namespace,
			Labels: map[string]string{
				"dagmar.denkhaus.io/parent-run": parent.Name,
				"app.kubernetes.io/managed-by":  "dagmar-controller",
			},
		},
		Spec: v1alpha1.RunSpec{
			ProjectRef:     parent.Spec.ProjectRef,
			AgentRef:       agentRef,
			ModuleFunction: fn,
			ParentRun:      parent.Name,
		},
	}
	if err := ctrl.SetControllerReference(parent, sub, r.Scheme); err != nil {
		return nil, fmt.Errorf("set controller reference for Sub-Run: %w", err)
	}
	if err := r.Create(ctx, sub); err != nil && !errors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create Sub-Run %q: %w", name, err)
	}
	return sub, nil
}

// transitionPipeline updates the orchestration Run's pipeline phase + status.
func (r *RunReconciler) transitionPipeline(ctx context.Context, run *v1alpha1.Run, phase string, reason string, subRunRef string) error {
	return r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = phase
		switch phase {
		case PipelineDone:
			s.Phase = v1alpha1.RunPhaseSucceeded
		case PipelineEscalated:
			s.Phase = v1alpha1.RunPhaseFailed
		default:
			s.Phase = v1alpha1.RunPhaseRunning
		}
		// Track the latest Sub-Run ref.
		found := false
		for _, ref := range s.SubRunRefs {
			if ref == subRunRef {
				found = true
				break
			}
		}
		if !found {
			s.SubRunRefs = append(s.SubRunRefs, subRunRef)
		}
		// Update conditions.
		gen := run.Generation
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionProgressing,
			map[bool]metav1.ConditionStatus{true: metav1.ConditionTrue, false: metav1.ConditionFalse}[phase == PipelineCoder || phase == PipelineReview],
			reason, reason, gen))
		if phase == PipelineDone {
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionSucceeded,
				metav1.ConditionTrue, reason, reason, gen))
		}
		if phase == PipelineEscalated {
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionFailed,
				metav1.ConditionTrue, reason, reason, gen))
		}
	})
}

// subRunName builds a deterministic Sub-Run name from parent + stage.
func subRunName(parentRun, stage string) string {
	return fmt.Sprintf("%s-%s", parentRun, stage)
}

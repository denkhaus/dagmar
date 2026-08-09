package controller

import (
	"context"
	"fmt"
	"strconv"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Annotation keys for prompter configuration on Sub-Runs (ADR-0023 D3). The
// orchestration controller annotates each coder/reviewer Sub-Run with the
// prompter model, maxAPICalls, and phase so the atomic-Run Reconcile path can
// chain the prompt() call into the Pod command.
const (
	annPrompterModel       = "dagmar.denkhaus.io/prompter-model"
	annPrompterMaxAPICalls = "dagmar.denkhaus.io/prompter-max-apicalls"
	annPrompterPhase       = "dagmar.denkhaus.io/prompter-phase"
)

// Prompter phases (ADR-0023 D2). These select the meta-prompt the prompter uses.
const (
	prompterPhasePreCode   = "pre-code"
	prompterPhasePreReview = "pre-review"
)

// Pipeline phases (ADR-0023 D5).
const (
	PipelineCoding       = "coding"
	PipelineGating       = "gating"
	PipelineReviewing    = "reviewing"
	PipelineAdjudicating = "adjudicating"
	PipelineEscalated    = "escalated"
	PipelineDone         = "done"
)

// reconcileOrchestration drives an orchestration Run through the revised pipeline
// (ADR-0023 D5):
//
//	coding → gating → reviewing → done
//	    |        |        |
//	    v        v        v
//	  revise   revise   adjudicating / escalated
//
// The prompter→coder and prompter→reviewer calls are chained in the same Pod
// (no controller decision point between them — D3). Gate and Reviewer remain
// separate Sub-Runs (the controller evaluates gate-green between them).
//
// Phase 2 incremental: gate-green is assumed when the coder Sub-Run succeeds
// (full gate evaluation is a follow-up, ADR-0020 D3).
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

	// PrompterAgentRef is required — every LLM workflow needs a prompter (ADR-0023 D7).
	if wf.Spec.PrompterAgentRef == "" {
		return ctrl.Result{}, r.failRun(ctx, run, "PrompterAgentRefRequired",
			"Workflow must set prompterAgentRef (ADR-0023 D7)")
	}

	// Validate the Workflow's agent refs exist (fail fast).
	for _, ref := range []string{wf.Spec.PrompterAgentRef, wf.Spec.CoderAgentRef, wf.Spec.ReviewerAgentRef, wf.Spec.AdjudicatorAgentRef} {
		if ref == "" {
			continue // reviewer and adjudicator are optional
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
		phase = PipelineCoding
	}

	switch phase {
	case PipelineCoding:
		return r.advanceCoding(ctx, run, project, wf)
	case PipelineGating:
		return r.advanceGating(ctx, run, project, wf)
	case PipelineReviewing:
		return r.advanceReviewing(ctx, run, project, wf)
	case PipelineAdjudicating:
		return r.advanceAdjudicating(ctx, run, project, wf)
	case PipelineDone, PipelineEscalated:
		// Terminal — nothing to do.
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, r.failRun(ctx, run, "UnknownPipelinePhase",
			fmt.Sprintf("unknown PipelinePhase %q", phase))
	}
}

// advanceCoding creates or watches the coder Sub-Run with a chained prompter
// (prompt→code in the same Pod, ADR-0023 D3). On success → gating.
// On failure → revise loop or escalation.
func (r *RunReconciler) advanceCoding(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	// Resolve the prompter agent for the chained prompt() call.
	prompter, err := r.getAgent(ctx, wf.Spec.PrompterAgentRef, run.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	subName := subRunName(run.Name, fmt.Sprintf("coder-r%d", run.Status.CurrentRound+1))
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.CoderAgentRef, project, "code",
		prompter.Spec.Model, prompter.Spec.MaxAPICalls, prompterPhasePreCode)
	if err != nil {
		return ctrl.Result{}, err
	}

	if sub.Status.Phase == v1alpha1.RunPhaseSucceeded {
		// Coder succeeded → advance to gating.
		if err := r.transitionPipeline(ctx, run, PipelineGating, "CoderSucceededAwaitingGate", sub.Name); err != nil {
			return ctrl.Result{}, err
		}
		return r.advanceGating(ctx, run, project, wf)
	}
	if sub.Status.Phase == v1alpha1.RunPhaseFailed {
		// Coder failed → revise loop or escalate.
		round := run.Status.CurrentRound + 1
		maxRound := r.maxReviseRoundsFor(ctx, wf, run.Namespace)
		if round > maxRound {
			return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineEscalated,
				"MaxReviseRoundsExceeded", sub.Name)
		}
		// Revise: reset to coding with incremented round.
		return ctrl.Result{}, r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
			s.PipelinePhase = PipelineCoding
			s.CurrentRound = round
		})
	}
	// Still running — wait.
	_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = PipelineCoding
		s.Phase = v1alpha1.RunPhaseRunning
	})
	return ctrl.Result{}, nil
}

// advanceGating evaluates dagmar-gate after the coder Sub-Run succeeded.
// Phase 2: gate-green is assumed when the coder Sub-Run succeeded (full gate
// evaluation via `dagger call gate` is a follow-up, ADR-0020 D3). Gate green →
// reviewing (if reviewer configured) or done. Gate red → revise loop.
func (r *RunReconciler) advanceGating(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	// TODO(follow-up): call `dagger call gate --source <coder-result>` and parse
	// the deterministic output (green/red). For now, gate-green is assumed when
	// the coder Sub-Run succeeded (ADR-0020 D3).
	gateGreen := true

	if gateGreen {
		if wf.Spec.ReviewerAgentRef != "" && wf.Spec.RequiresTwoGreen {
			// Transition to reviewing, then immediately create the reviewer Sub-Run.
			if err := r.transitionPipeline(ctx, run, PipelineReviewing, "GateGreenAwaitingReview", ""); err != nil {
				return ctrl.Result{}, err
			}
			return r.advanceReviewing(ctx, run, project, wf)
		}
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineDone, "GateGreen", "")
	}

	// Gate red → revise loop.
	round := run.Status.CurrentRound + 1
	maxRound := r.maxReviseRoundsFor(ctx, wf, run.Namespace)
	if round > maxRound {
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineEscalated,
			"MaxReviseRoundsExceeded", "")
	}
	return ctrl.Result{}, r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = PipelineCoding
		s.CurrentRound = round
	})
}

// advanceReviewing creates or watches the reviewer Sub-Run with a chained
// prompter (prompt→code with --phase pre-review, ADR-0023 D3). On success →
// two-green → done. On failure (veto) → adjudicating (if adjudicator configured)
// or escalated.
func (r *RunReconciler) advanceReviewing(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	// Resolve the prompter agent for the chained prompt() call.
	prompter, err := r.getAgent(ctx, wf.Spec.PrompterAgentRef, run.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	subName := subRunName(run.Name, fmt.Sprintf("review-r%d", run.Status.CurrentRound+1))
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.ReviewerAgentRef, project, "code",
		prompter.Spec.Model, prompter.Spec.MaxAPICalls, prompterPhasePreReview)
	if err != nil {
		return ctrl.Result{}, err
	}

	if sub.Status.Phase == v1alpha1.RunPhaseSucceeded {
		// Reviewer succeeded (approve) → two-green reached → done.
		// (Full implementation: parse reviewer output for approve/veto.
		// For now, success = approve.)
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineDone,
			"TwoGreenReached", sub.Name)
	}
	if sub.Status.Phase == v1alpha1.RunPhaseFailed {
		// Reviewer veto → disagreement with gate (gate green + reviewer veto).
		// Adjudicate if configured, else escalate to human.
		if wf.Spec.AdjudicatorAgentRef != "" {
			if err := r.transitionPipeline(ctx, run, PipelineAdjudicating, "ReviewerVetoDisagreement", sub.Name); err != nil {
				return ctrl.Result{}, err
			}
			return r.advanceAdjudicating(ctx, run, project, wf)
		}
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineEscalated,
			"ReviewerVetoNoAdjudicator", sub.Name)
	}
	// Still running — wait.
	_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = PipelineReviewing
		s.Phase = v1alpha1.RunPhaseRunning
	})
	return ctrl.Result{}, nil
}

// advanceAdjudicating creates or watches the adjudicator Sub-Run (ADR-0023 D4).
// The adjudicator runs without a prompter chain — it analyzes the disagreement
// directly. On success → conflict resolved → done. On failure → escalated.
func (r *RunReconciler) advanceAdjudicating(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	subName := subRunName(run.Name, fmt.Sprintf("adjudicate-r%d", run.Status.CurrentRound+1))
	// The adjudicator runs without a chained prompter (empty model/phase).
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.AdjudicatorAgentRef, project, "code",
		"", 0, "")
	if err != nil {
		return ctrl.Result{}, err
	}

	if sub.Status.Phase == v1alpha1.RunPhaseSucceeded {
		// Adjudicator resolved the conflict → done.
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineDone,
			"AdjudicatorResolved", sub.Name)
	}
	if sub.Status.Phase == v1alpha1.RunPhaseFailed {
		// Adjudicator could not resolve → escalate to human.
		return ctrl.Result{}, r.transitionPipeline(ctx, run, PipelineEscalated,
			"AdjudicatorUnresolvable", sub.Name)
	}
	// Still running — wait.
	_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		s.PipelinePhase = PipelineAdjudicating
		s.Phase = v1alpha1.RunPhaseRunning
	})
	return ctrl.Result{}, nil
}

// getAgent fetches an Agent by name+namespace, returning a typed error on failure.
func (r *RunReconciler) getAgent(ctx context.Context, name, namespace string) (*v1alpha1.Agent, error) {
	agent := &v1alpha1.Agent{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, agent); err != nil {
		return nil, fmt.Errorf("get Agent %q: %w", name, err)
	}
	return agent, nil
}

// getOrCreateSubRun creates an atomic Sub-Run if it doesn't exist, then returns it.
// The Sub-Run carries ModuleFunction + AgentRef + ParentRun + prompter annotations.
// The prompter annotations (model, maxAPICalls, phase) drive the chained prompt()
// call in the Pod command built by agentPodFor (ADR-0023 D3).
func (r *RunReconciler) getOrCreateSubRun(ctx context.Context, parent *v1alpha1.Run, name string, agentRef string, project *v1alpha1.Project, fn string, prompterModel string, prompterMaxAPICalls int, prompterPhase string) (*v1alpha1.Run, error) {
	sub := &v1alpha1.Run{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: parent.Namespace}, sub)
	if err == nil {
		return sub, nil // already exists
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}
	annotations := map[string]string{}
	if prompterPhase != "" {
		annotations[annPrompterModel] = prompterModel
		annotations[annPrompterMaxAPICalls] = strconv.Itoa(prompterMaxAPICalls)
		annotations[annPrompterPhase] = prompterPhase
	}
	sub = &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: parent.Namespace,
			Labels: map[string]string{
				"dagmar.denkhaus.io/parent-run": parent.Name,
				"app.kubernetes.io/managed-by":  "dagmar-controller",
			},
			Annotations: annotations,
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
		if subRunRef != "" {
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
		}
		// Update conditions.
		gen := run.Generation
		active := phase == PipelineCoding || phase == PipelineGating ||
			phase == PipelineReviewing || phase == PipelineAdjudicating
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionProgressing,
			map[bool]metav1.ConditionStatus{true: metav1.ConditionTrue, false: metav1.ConditionFalse}[active],
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

// maxReviseRoundsFor reads the QualityGate referenced by the Workflow and returns
// its MaxReviseRounds (default 3 if unset or QualityGate missing).
func (r *RunReconciler) maxReviseRoundsFor(ctx context.Context, wf *v1alpha1.Workflow, namespace string) int {
	if wf.Spec.QualityGateRef == "" {
		return 3
	}
	qg := &v1alpha1.QualityGate{}
	if err := r.Get(ctx, types.NamespacedName{Name: wf.Spec.QualityGateRef, Namespace: namespace}, qg); err != nil {
		return 3 // default if not found
	}
	if qg.Spec.MaxReviseRounds > 0 {
		return qg.Spec.MaxReviseRounds
	}
	return 3
}

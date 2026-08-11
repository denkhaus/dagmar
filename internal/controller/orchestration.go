package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"github.com/denkhaus/dagmar/manifest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Annotation keys for prompter configuration on Sub-Runs (ADR-0023 D3). The
// orchestration controller annotates each coder/reviewer Sub-Run with the
// prompter model, maxAPICalls, and phase so the atomic-Run Reconcile path can
// chain the prompt() call into the Pod command.
const (
	annPrompterModel       = "dagmar.denkhaus.io/prompter-model"
	annPrompterMaxAPICalls = "dagmar.denkhaus.io/prompter-max-apicalls"
	annPrompterPhase       = "dagmar.denkhaus.io/prompter-phase"
	// annCoverageFloor is set on the coder Sub-Run when CoveragePolicy is enabled
	// (dagmar-4154). The atomic-Run Reconcile path reads it and passes
	// --coverage-floor-bps to `dagger call dagmar-gate` in the agent pod, so the
	// gate runs inside the coder's pod as self-verification.
	annCoverageFloor = "dagmar.denkhaus.io/coverage-floor"
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

	// Inject prompter config into the run's annotations (ADR-0023 D3).
	if err := r.resolvePrompterWrapper(ctx, run, wf); err != nil {
		return ctrl.Result{}, err
	}

	// Delegate to the FSM-driven generic orchestration (ADR-0026).
	return r.reconcileOrchestrationFSM(ctx, run, project, wf)
}

// resolvePrompterWrapper injects prompter config into the run's annotations so
// the Sub-Run creation path (getOrCreateSubRun) picks them up via the existing
// annotation mechanism (annPrompterModel/annPrompterMaxAPICalls/annPrompterPhase).
func (r *RunReconciler) resolvePrompterWrapper(ctx context.Context, run *v1alpha1.Run, wf *v1alpha1.Workflow) error {
	pModel, pMaxAPICalls := r.resolvePrompter(ctx, wf, run.Namespace)
	if pModel == "" {
		return nil
	}
	// Patch annotations on the run.
	fresh := &v1alpha1.Run{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), fresh); err != nil {
		return err
	}
	base := fresh.DeepCopy()
	if fresh.Annotations == nil {
		fresh.Annotations = map[string]string{}
	}
	fresh.Annotations[annPrompterModel] = pModel
	fresh.Annotations[annPrompterMaxAPICalls] = strconv.Itoa(pMaxAPICalls)
	return r.Patch(ctx, fresh, client.MergeFrom(base))
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
	// Coverage floor for the coder Sub-Run (dagmar-4154): when CoveragePolicy is
	// enabled, annotate the coder Sub-Run with the ratcheted floor so the agent
	// pod passes --coverage-floor-bps to `dagger call dagmar-gate` during
	// self-verification. The actual gate call runs inside the pod; the
	// controller's gate evaluation stays a stub (advanceGating).
	coverageFloorBps := coverageFloorFor(project)

	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.CoderAgentRef, project, "code",
		prompter.Spec.Model, prompter.Spec.MaxAPICalls, prompterPhasePreCode, coverageFloorBps)
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

// advanceGating evaluates dagmar-gate after the coder Sub-Run succeeded. The gate
// runs inside the coder's agent pod (chained after code() — the JSON result goes to
// /dev/termination-log). The controller reads the pod's termination message, parses
// the GateResult JSON, and decides gate-green/red. On green, the coverage floor is
// ratcheted upward (dagmar-4154 / dagmar-3061).
func (r *RunReconciler) advanceGating(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, wf *v1alpha1.Workflow) (ctrl.Result, error) {
	// Read the gate result from the coder pod's termination message (dagmar-60c3). The gate
	// runs chained after code() in the coder pod; its JSON output goes to /dev/termination-log.
	// If the pod hasn't terminated or the termination message is unavailable (e.g. test
	// environment without real pods), fall back to assuming gate-green on coder success.
	gateGreen := true
	var measuredCoverageBps int

	coderName := subRunName(run.Name, fmt.Sprintf("coder-r%d", run.Status.CurrentRound+1))
	coderSub := &v1alpha1.Run{}
	if err := r.Get(ctx, types.NamespacedName{Name: coderName, Namespace: run.Namespace}, coderSub); err == nil {
		if coderSub.Status.AgentPodName != "" {
			if gateResult, err := r.readGateResult(ctx, coderSub.Status.AgentPodName, run.Namespace); err == nil {
				gateGreen = gateResult.Passed
				measuredCoverageBps = gateResult.CoverageBps
			}
			// If readGateResult fails (pod not terminated, no message), gateGreen stays true
			// (fallback: assume green on coder success — ADR-0020 D3 stub behavior).
		}
	}

	// Coverage ratcheting (dagmar-4154 / dagmar-3061). Two paths:
	// 1. Initialization: when the floor is still 0 and coverage policy is enabled,
	//    set the floor to MinimumFloor (the absolute lower bound).
	// 2. Upward ratchet: when actual coverage was measured (from gate JSON), ratchet
	//    the floor upward: newFloor = max(currentFloor, coverage - RatchetMargin).
	if gateGreen && project.Spec.CoveragePolicy != nil && project.Spec.CoveragePolicy.Enabled {
		if project.Status.CoverageFloor == 0 && measuredCoverageBps == 0 {
			// Initialization: set floor to MinimumFloor (no coverage measured yet —
			// the gate runs in the pod but its result may not be available yet).
			if err := r.patchProjectCoverageFloor(ctx, project, project.Spec.CoveragePolicy.MinimumFloor); err != nil {
				return ctrl.Result{}, err
			}
		} else if measuredCoverageBps > 0 {
			margin := project.Spec.CoveragePolicy.RatchetMargin
			if margin == 0 {
				margin = 200 // default 2.00%
			}
			newFloor := measuredCoverageBps - margin
			minFloor := project.Spec.CoveragePolicy.MinimumFloor
			if newFloor < minFloor {
				newFloor = minFloor
			}
			if newFloor > project.Status.CoverageFloor {
				if err := r.patchProjectCoverageFloor(ctx, project, newFloor); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	}

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
	// Reviewer Sub-Run: no coverage floor (only the coder runs the gate).
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.ReviewerAgentRef, project, "code",
		prompter.Spec.Model, prompter.Spec.MaxAPICalls, prompterPhasePreReview, 0)
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
	// Adjudicator Sub-Run: no coverage floor.
	sub, err := r.getOrCreateSubRun(ctx, run, subName, wf.Spec.AdjudicatorAgentRef, project, "code",
		"", 0, "", 0)
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
func (r *RunReconciler) getOrCreateSubRun(ctx context.Context, parent *v1alpha1.Run, name string, agentRef string, project *v1alpha1.Project, fn string, prompterModel string, prompterMaxAPICalls int, prompterPhase string, coverageFloorBps int) (*v1alpha1.Run, error) {
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
	// Coverage floor annotation (dagmar-4154): the atomic-Run Reconcile path reads
	// this and passes --coverage-floor-bps to `dagger call dagmar-gate` in the pod.
	if coverageFloorBps > 0 {
		annotations[annCoverageFloor] = strconv.Itoa(coverageFloorBps)
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
			TaskContext:    parent.Spec.TaskContext,
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

// coverageFloorFor resolves the coverage floor (basis points) to annotate on the
// coder Sub-Run. Returns 0 (disabled) when CoveragePolicy is not set or not
// enabled. When enabled, returns the current ratcheted floor (Status.CoverageFloor),
// or MinimumFloor if the floor has not yet been initialized (dagmar-4154).
func coverageFloorFor(project *v1alpha1.Project) int {
	if project.Spec.CoveragePolicy == nil || !project.Spec.CoveragePolicy.Enabled {
		return 0
	}
	if project.Status.CoverageFloor > 0 {
		return project.Status.CoverageFloor
	}
	return project.Spec.CoveragePolicy.MinimumFloor
}

// readGateResult reads the coder pod's termination message and parses it as a GateResult JSON.
// The pod writes the gate output to /dev/termination-log (K8s surfaces it as
// pod.Status.ContainerStatuses[0].State.Terminated.Message). Returns an error if the pod has
// not terminated or the message is empty/unparseable.
func (r *RunReconciler) readGateResult(ctx context.Context, podName, namespace string) (*manifest.GateResult, error) {
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, fmt.Errorf("read gate result: get pod %q: %w", podName, err)
	}
	if len(pod.Status.ContainerStatuses) == 0 {
		return nil, fmt.Errorf("no container statuses")
	}
	termState := pod.Status.ContainerStatuses[0].State.Terminated
	if termState == nil {
		return nil, fmt.Errorf("pod not terminated yet")
	}
	msg := strings.TrimSpace(termState.Message)
	if msg == "" {
		return nil, fmt.Errorf("termination message empty")
	}
	var result manifest.GateResult
	if err := json.Unmarshal([]byte(msg), &result); err != nil {
		return nil, fmt.Errorf("parse gate JSON: %w (message: %s)", err, msg[:min(len(msg), 200)])
	}
	return &result, nil
}

// patchProjectCoverageFloor patches the Project's Status.CoverageFloor via the
// status subresource (dagmar-4154). Used for ratcheting: the controller updates
// the floor after a successful gate.
func (r *RunReconciler) patchProjectCoverageFloor(ctx context.Context, project *v1alpha1.Project, floorBps int) error {
	fresh := &v1alpha1.Project{}
	if err := r.Get(ctx, types.NamespacedName{Name: project.Name, Namespace: project.Namespace}, fresh); err != nil {
		return fmt.Errorf("get project for coverage-floor patch: %w", err)
	}
	base := fresh.DeepCopy()
	fresh.Status.CoverageFloor = floorBps
	if err := r.Status().Patch(ctx, fresh, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch project coverage-floor: %w", err)
	}
	return nil
}

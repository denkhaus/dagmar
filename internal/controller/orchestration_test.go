package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ctx = context.Background()

func newTestOrchestrationReconciler(t *testing.T, run *v1alpha1.Run) (*RunReconciler, client.Client) {
	t.Helper()

	coderAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "coder-agent", Namespace: run.Namespace},
		Spec: v1alpha1.AgentSpec{
			Model:       "test-model",
			MaxAPICalls: 50,
		},
	}
	prompterAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "prompter-agent", Namespace: run.Namespace},
		Spec: v1alpha1.AgentSpec{
			Model:       "test-prompter-model",
			MaxAPICalls: 10,
		},
	}
	reviewerAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "reviewer-agent", Namespace: run.Namespace},
		Spec: v1alpha1.AgentSpec{
			Model:       "test-model",
			MaxAPICalls: 30,
		},
	}
	adjudicatorAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "adjudicator-agent", Namespace: run.Namespace},
		Spec: v1alpha1.AgentSpec{
			Model:       "test-adjudicator-model",
			MaxAPICalls: 40,
		},
	}
	wf := &v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-workflow", Namespace: run.Namespace},
		Spec: v1alpha1.WorkflowSpec{
			CoderAgentRef:    "coder-agent",
			PrompterAgentRef: "prompter-agent",
			ReviewerAgentRef: "reviewer-agent",
			QualityGateRef:   "test-gate",
			RequiresTwoGreen: true,
		},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: run.Spec.ProjectRef, Namespace: run.Namespace},
		Spec: v1alpha1.ProjectSpec{
			Repo:          "https://github.com/test/repo",
			AgentPodImage: "alpine:3.20",
			ModuleRef:     "github.com/test/repo/.dagger",
		},
	}

	r, cl := newTestReconciler(t, run)
	for _, obj := range []client.Object{coderAgent, prompterAgent, reviewerAgent, adjudicatorAgent, wf, project} {
		if err := cl.Create(ctx, obj); err != nil {
			t.Fatalf("create %T: %v", obj, err)
		}
	}
	return r, cl
}

func TestReconcile_OrchestrationCreatesCoderSubRun(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-1", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "orch-1", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	subName := subRunName("orch-1", "coder-r1")
	sub := &v1alpha1.Run{}
	if err := cl.Get(ctx, types.NamespacedName{Name: subName, Namespace: "default"}, sub); err != nil {
		t.Fatalf("expected Sub-Run %q: %v", subName, err)
	}
	if sub.Spec.AgentRef != "coder-agent" {
		t.Errorf("Sub-Run AgentRef = %q, want coder-agent", sub.Spec.AgentRef)
	}
	if sub.Spec.ModuleFunction != "code" {
		t.Errorf("Sub-Run ModuleFunction = %q, want code", sub.Spec.ModuleFunction)
	}
	if sub.Spec.ParentRun != "orch-1" {
		t.Errorf("Sub-Run ParentRun = %q, want orch-1", sub.Spec.ParentRun)
	}
	// Prompter annotations should be set on the coder Sub-Run.
	if sub.Annotations[annPrompterPhase] != "pre-code" {
		t.Errorf("prompter phase annotation = %q, want pre-code", sub.Annotations[annPrompterPhase])
	}
	if sub.Annotations[annPrompterModel] != "test-prompter-model" {
		t.Errorf("prompter model annotation = %q, want test-prompter-model", sub.Annotations[annPrompterModel])
	}
}

func TestReconcile_OrchestrationAdvancesToReviewing(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-2", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)

	// First reconcile: creates coder Sub-Run.
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: types.NamespacedName{Name: "orch-2", Namespace: "default"}})

	// Mark coder Sub-Run as Succeeded.
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-2", "coder-r1"), Namespace: "default"}, v1alpha1.RunPhaseSucceeded)

	// Second reconcile: should advance through gating to reviewing and create reviewer Sub-Run.
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: types.NamespacedName{Name: "orch-2", Namespace: "default"}})

	// Reviewer Sub-Run should exist.
	reviewName := subRunName("orch-2", "review-r1")
	review := &v1alpha1.Run{}
	if err := cl.Get(ctx, types.NamespacedName{Name: reviewName, Namespace: "default"}, review); err != nil {
		t.Fatalf("expected reviewer Sub-Run %q: %v", reviewName, err)
	}
	if review.Spec.AgentRef != "reviewer-agent" {
		t.Errorf("reviewer AgentRef = %q, want reviewer-agent", review.Spec.AgentRef)
	}
	// Reviewer Sub-Run should have pre-review prompter annotations.
	if review.Annotations[annPrompterPhase] != "pre-review" {
		t.Errorf("prompter phase annotation = %q, want pre-review", review.Annotations[annPrompterPhase])
	}

	// Orchestration Run should be in PipelinePhase=reviewing.
	updated := fetchRun(t, cl, types.NamespacedName{Name: "orch-2", Namespace: "default"})
	if updated.Status.PipelinePhase != PipelineReviewing {
		t.Errorf("PipelinePhase = %q, want %q", updated.Status.PipelinePhase, PipelineReviewing)
	}
}

func TestReconcile_OrchestrationReachesDone(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-3", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)

	// coder succeeds.
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: types.NamespacedName{Name: "orch-3", Namespace: "default"}})
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-3", "coder-r1"), Namespace: "default"}, v1alpha1.RunPhaseSucceeded)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: types.NamespacedName{Name: "orch-3", Namespace: "default"}})

	// reviewer succeeds.
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-3", "review-r1"), Namespace: "default"}, v1alpha1.RunPhaseSucceeded)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: types.NamespacedName{Name: "orch-3", Namespace: "default"}})

	// Orchestration Run should be done (Succeeded).
	updated := fetchRun(t, cl, types.NamespacedName{Name: "orch-3", Namespace: "default"})
	if updated.Status.PipelinePhase != PipelineDone {
		t.Errorf("PipelinePhase = %q, want %q", updated.Status.PipelinePhase, PipelineDone)
	}
	if updated.Status.Phase != v1alpha1.RunPhaseSucceeded {
		t.Errorf("Phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhaseSucceeded)
	}
}

func TestReconcile_OrchestrationEscalatesAfterMaxRounds(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-4", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)

	key := types.NamespacedName{Name: "orch-4", Namespace: "default"}

	// Drive the pipeline: each coder round fails. The revise loop needs enough
	// reconciles to create, fail, and process each round.
	for i := 0; i < 20; i++ {
		mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

		// Mark any existing coder Sub-Run that is not yet Failed as Failed.
		updated := fetchRun(t, cl, key)
		if updated.Status.PipelinePhase == PipelineEscalated {
			break
		}
		// Find the latest coder Sub-Run and fail it.
		round := updated.Status.CurrentRound + 1
		coderKey := types.NamespacedName{
			Name:      subRunName("orch-4", fmt.Sprintf("coder-r%d", round)),
			Namespace: "default",
		}
		coderRun := &v1alpha1.Run{}
		if err := cl.Get(ctx, coderKey, coderRun); err == nil && coderRun.Status.Phase != v1alpha1.RunPhaseFailed {
			setRunStatusPhase(t, cl, coderKey, v1alpha1.RunPhaseFailed)
		}
	}

	updated := fetchRun(t, cl, key)
	if updated.Status.PipelinePhase != PipelineEscalated {
		t.Errorf("PipelinePhase = %q, want %q", updated.Status.PipelinePhase, PipelineEscalated)
	}
	if updated.Status.Phase != v1alpha1.RunPhaseFailed {
		t.Errorf("Phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhaseFailed)
	}
}

// TestReconcile_OrchestrationReviewerVetoEscalatesWithoutAdjudicator verifies that a
// reviewer veto (fail) escalates to a human when no AdjudicatorAgentRef is configured.
func TestReconcile_OrchestrationReviewerVetoEscalatesWithoutAdjudicator(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-veto-1", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)
	key := types.NamespacedName{Name: "orch-veto-1", Namespace: "default"}

	// coder succeeds.
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-veto-1", "coder-r1"), Namespace: "default"}, v1alpha1.RunPhaseSucceeded)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// reviewer fails (veto).
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-veto-1", "review-r1"), Namespace: "default"}, v1alpha1.RunPhaseFailed)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// Should escalate (no adjudicator configured in the default test workflow).
	updated := fetchRun(t, cl, key)
	if updated.Status.PipelinePhase != PipelineEscalated {
		t.Errorf("PipelinePhase = %q, want %q (escalate on veto without adjudicator)", updated.Status.PipelinePhase, PipelineEscalated)
	}
	if updated.Status.Phase != v1alpha1.RunPhaseFailed {
		t.Errorf("Phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhaseFailed)
	}
}

// TestReconcile_OrchestrationReviewerVetoAdjudicates verifies that a reviewer veto
// enters adjudication when AdjudicatorAgentRef is configured, and that adjudicator
// success resolves to done.
func TestReconcile_OrchestrationReviewerVetoAdjudicates(t *testing.T) {
	r, cl := setupAdjudicatorTest(t, "orch-adj-1", true)

	key := types.NamespacedName{Name: "orch-adj-1", Namespace: "default"}

	// coder succeeds.
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-adj-1", "coder-r1"), Namespace: "default"}, v1alpha1.RunPhaseSucceeded)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// reviewer fails (veto).
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-adj-1", "review-r1"), Namespace: "default"}, v1alpha1.RunPhaseFailed)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// Should enter adjudicating (adjudicator configured).
	updated := fetchRun(t, cl, key)
	if updated.Status.PipelinePhase != PipelineAdjudicating {
		t.Fatalf("PipelinePhase = %q, want %q", updated.Status.PipelinePhase, PipelineAdjudicating)
	}

	// Adjudicator Sub-Run should exist.
	adjName := subRunName("orch-adj-1", "adjudicate-r1")
	adj := &v1alpha1.Run{}
	if err := cl.Get(ctx, types.NamespacedName{Name: adjName, Namespace: "default"}, adj); err != nil {
		t.Fatalf("expected adjudicator Sub-Run %q: %v", adjName, err)
	}
	if adj.Spec.AgentRef != "adjudicator-agent" {
		t.Errorf("adjudicator AgentRef = %q, want adjudicator-agent", adj.Spec.AgentRef)
	}

	// Adjudicator succeeds → conflict resolved → done.
	setRunStatusPhase(t, cl, types.NamespacedName{Name: adjName, Namespace: "default"}, v1alpha1.RunPhaseSucceeded)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	updated = fetchRun(t, cl, key)
	if updated.Status.PipelinePhase != PipelineDone {
		t.Errorf("PipelinePhase = %q, want %q (adjudicator resolved)", updated.Status.PipelinePhase, PipelineDone)
	}
	if updated.Status.Phase != v1alpha1.RunPhaseSucceeded {
		t.Errorf("Phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhaseSucceeded)
	}
}

// TestReconcile_OrchestrationAdjudicatorFailEscalates verifies that adjudicator
// failure escalates.
func TestReconcile_OrchestrationAdjudicatorFailEscalates(t *testing.T) {
	r, cl := setupAdjudicatorTest(t, "orch-adj-2", true)

	key := types.NamespacedName{Name: "orch-adj-2", Namespace: "default"}

	// coder succeeds.
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-adj-2", "coder-r1"), Namespace: "default"}, v1alpha1.RunPhaseSucceeded)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// reviewer fails (veto).
	setRunStatusPhase(t, cl, types.NamespacedName{Name: subRunName("orch-adj-2", "review-r1"), Namespace: "default"}, v1alpha1.RunPhaseFailed)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// Adjudicator fails → escalate.
	adjName := subRunName("orch-adj-2", "adjudicate-r1")
	setRunStatusPhase(t, cl, types.NamespacedName{Name: adjName, Namespace: "default"}, v1alpha1.RunPhaseFailed)
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	updated := fetchRun(t, cl, key)
	if updated.Status.PipelinePhase != PipelineEscalated {
		t.Errorf("PipelinePhase = %q, want %q (adjudicator unresolvable)", updated.Status.PipelinePhase, PipelineEscalated)
	}
}

// setupAdjudicatorTest creates a reconciler with a workflow that has an AdjudicatorAgentRef.
func setupAdjudicatorTest(t *testing.T, runName string, _ bool) (*RunReconciler, client.Client) {
	t.Helper()
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: runName, Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow-adj",
		},
	}
	r, cl := newTestReconciler(t, run)

	coderAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "coder-agent", Namespace: "default"},
		Spec:       v1alpha1.AgentSpec{Model: "test-model", MaxAPICalls: 50},
	}
	prompterAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "prompter-agent", Namespace: "default"},
		Spec:       v1alpha1.AgentSpec{Model: "test-prompter-model", MaxAPICalls: 10},
	}
	reviewerAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "reviewer-agent", Namespace: "default"},
		Spec:       v1alpha1.AgentSpec{Model: "test-model", MaxAPICalls: 30},
	}
	adjudicatorAgent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "adjudicator-agent", Namespace: "default"},
		Spec:       v1alpha1.AgentSpec{Model: "test-adjudicator-model", MaxAPICalls: 40},
	}
	// Workflow with adjudicator configured.
	wf := &v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-workflow-adj", Namespace: "default"},
		Spec: v1alpha1.WorkflowSpec{
			CoderAgentRef:       "coder-agent",
			PrompterAgentRef:    "prompter-agent",
			ReviewerAgentRef:    "reviewer-agent",
			AdjudicatorAgentRef: "adjudicator-agent",
			QualityGateRef:      "test-gate",
			RequiresTwoGreen:    true,
		},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proj", Namespace: "default"},
		Spec: v1alpha1.ProjectSpec{
			Repo:          "https://github.com/test/repo",
			AgentPodImage: "alpine:3.20",
			ModuleRef:     "github.com/test/repo/.dagger",
		},
	}
	for _, obj := range []client.Object{coderAgent, prompterAgent, reviewerAgent, adjudicatorAgent, wf, project} {
		if err := cl.Create(ctx, obj); err != nil {
			t.Fatalf("create %T: %v", obj, err)
		}
	}
	return r, cl
}

// setRunStatusPhase patches a Run's status phase (for test simulation).
func setRunStatusPhase(t *testing.T, cl client.Client, key types.NamespacedName, phase string) {
	t.Helper()
	run := &v1alpha1.Run{}
	if err := cl.Get(ctx, key, run); err != nil {
		t.Fatalf("get run %v: %v", key, err)
	}
	base := run.DeepCopy()
	run.Status.Phase = phase
	if err := cl.Status().Patch(ctx, run, client.MergeFrom(base)); err != nil {
		t.Fatalf("patch run status: %v", err)
	}
}

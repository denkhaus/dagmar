package controller

import (
	"context"
	"testing"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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

// NEW TESTS (ADR-0027): orchestration Runs dispatch cognition-run as a single pod.

func TestReconcile_OrchestrationDispatchesCognitionRun(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-pipe-1", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
			TaskContext: "Implement feature X",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)

	key := types.NamespacedName{Name: "orch-pipe-1", Namespace: "default"}
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	// The pod should exist with the cognition-run command.
	podName := "orch-pipe-1-agent"
	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err != nil {
		t.Fatalf("expected agent pod %q: %v", podName, err)
	}
	cmd := pod.Spec.Containers[0].Command[2]
	assertContains(t, cmd, "cognition-run")
	assertContains(t, cmd, "--task-context")
	assertContains(t, cmd, "--model")
}

func TestReconcile_OrchestrationPipelineAccepted(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-pipe-2", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
		},
	}
	r, _ := newTestOrchestrationReconciler(t, run)

	key := types.NamespacedName{Name: "orch-pipe-2", Namespace: "default"}
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	updated := &v1alpha1.Run{}
	if err := r.Get(ctx, key, updated); err != nil {
		t.Fatalf("get run: %v", err)
	}

	// PipelinePhase should be dispatching or running (not the old step-level phases).
	phase := updated.Status.PipelinePhase
	if phase == "coding" || phase == "gating" || phase == "reviewing" || phase == "adjudicating" {
		t.Errorf("PipelinePhase = %q (step-level), expected policy-level (dispatching/running)", phase)
	}
}

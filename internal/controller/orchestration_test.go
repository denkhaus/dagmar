package controller

import (
	"context"
	"encoding/json"
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
	wf := &v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-workflow", Namespace: run.Namespace},
		Spec: v1alpha1.WorkflowSpec{
			CoderAgentRef: "coder-agent",
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
	for _, obj := range []client.Object{coderAgent, wf, project} {
		if err := cl.Create(ctx, obj); err != nil {
			t.Fatalf("create %T: %v", obj, err)
		}
	}
	return r, cl
}

// TestReconcile_OrchestrationDispatchesCognitionRun verifies the pod is created
// with the cognition-run command and NO ConfigMap postCall (ADR-0027 D3/D6).
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
	// No ConfigMap — results arrive via HTTP push.
	assertNotContains(t, cmd, "kubectl create configmap")
	assertNotContains(t, cmd, "/tmp/result.json")
}

// TestReconcile_OrchestrationPipelineAccepted verifies the PipelinePhase is policy-level
// (dispatching/running), not the old step-level phases.
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

	phase := updated.Status.PipelinePhase
	if phase == "coding" || phase == "gating" || phase == "reviewing" || phase == "adjudicating" {
		t.Errorf("PipelinePhase = %q (step-level), expected policy-level (dispatching/running)", phase)
	}
}

// TestReconcile_CognitionRunCallbackArgs verifies that when the CollectorServer
// and CollectorURL are configured, the dispatch injects --callback-url and
// --callback-token into the cognition-run args.
func TestReconcile_CognitionRunCallbackArgs(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-cb-1", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
			TaskContext: "Do thing",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)
	r.Collector = &CollectorServer{}
	r.CollectorURL = "http://collector.test:8082/step-result"

	key := types.NamespacedName{Name: "orch-cb-1", Namespace: "default"}
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "orch-cb-1-agent", Namespace: "default"}, pod); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	cmd := pod.Spec.Containers[0].Command[2]
	assertContains(t, cmd, "--callback-url")
	assertContains(t, cmd, "http://collector.test:8082/step-result")
	assertContains(t, cmd, "--callback-token")
}

// TestReconcile_CognitionRunNoCallbackWhenUnconfigured verifies that without
// a CollectorURL, no callback args are injected (standalone mode).
func TestReconcile_CognitionRunNoCallbackWhenUnconfigured(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "orch-cb-2", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:  "test-proj",
			WorkflowRef: "test-workflow",
			TaskContext: "Do thing",
		},
	}
	r, cl := newTestOrchestrationReconciler(t, run)
	// No Collector, no CollectorURL — standalone mode.

	key := types.NamespacedName{Name: "orch-cb-2", Namespace: "default"}
	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: key})

	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "orch-cb-2-agent", Namespace: "default"}, pod); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	cmd := pod.Spec.Containers[0].Command[2]
	assertNotContains(t, cmd, "--callback-url")
	assertNotContains(t, cmd, "--callback-token")
}

// TestEvaluatePipelineOutcome_Approve tests the outcome evaluator for an approve result.
func TestEvaluatePipelineOutcome_Approve(t *testing.T) {
	result, _ := json.Marshal(map[string]any{
		"outcome":    "approve",
		"gate_passed": true,
		"rounds":     1,
	})
	srs := []v1alpha1.StepResult{{Step: "gate", Result: string(result)}}
	outcome, terminal := evaluatePipelineOutcome(srs)
	if !terminal {
		t.Error("expected terminal")
	}
	if outcome != "approve" {
		t.Errorf("outcome = %q, want approve", outcome)
	}
}

// TestEvaluatePipelineOutcome_MaxRetries tests the outcome evaluator for max_retries.
func TestEvaluatePipelineOutcome_MaxRetries(t *testing.T) {
	// Simulate multiple pushes: first gate_red (interim), then max_retries (terminal).
	gateRed, _ := json.Marshal(map[string]any{"outcome": "gate_red"})
	maxRetries, _ := json.Marshal(map[string]any{"outcome": "max_retries"})
	srs := []v1alpha1.StepResult{
		{Step: "gate", Result: string(gateRed)},
		{Step: "gate", Result: string(maxRetries)},
	}
	outcome, terminal := evaluatePipelineOutcome(srs)
	if !terminal {
		t.Error("expected terminal for max_retries")
	}
	if outcome != "max_retries" {
		t.Errorf("outcome = %q, want max_retries", outcome)
	}
}

// TestEvaluatePipelineOutcome_Empty tests the evaluator with no step results.
func TestEvaluatePipelineOutcome_Empty(t *testing.T) {
	outcome, terminal := evaluatePipelineOutcome(nil)
	if terminal {
		t.Error("expected non-terminal for empty results")
	}
	if outcome != "" {
		t.Errorf("outcome = %q, want empty", outcome)
	}
}

// TestExtractCoverageBps tests coverage extraction from step results.
func TestExtractCoverageBps(t *testing.T) {
	gateJSON := `{"passed":true,"coverage_bps":7500}`
	outer, _ := json.Marshal(map[string]any{
		"outcome":     "approve",
		"gate_result": gateJSON,
	})
	srs := []v1alpha1.StepResult{{Step: "gate", Result: string(outer)}}
	bps := extractCoverageBps(srs)
	if bps != 7500 {
		t.Errorf("coverage = %d, want 7500", bps)
	}
}

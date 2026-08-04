package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newTestReconciler builds a RunReconciler over a fake client seeded with a Project (dagmar-own)
// and the singleton engine pod, returning the client so the test can mutate cluster state.
func newTestReconciler(t *testing.T, run *v1alpha1.Run) (*RunReconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("clientgo scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("v1alpha1 scheme: %v", err)
	}
	enginePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dagger-engine-0", Namespace: engineNamespace,
			Labels: map[string]string{"name": "dagger-dagger-helm-engine"},
		},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: run.Namespace},
		Spec:       v1alpha1.ProjectSpec{Repo: "https://github.com/denkhaus/dagmar"},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.Run{}).
		WithObjects(project, enginePod, run).
		Build()
	return &RunReconciler{Client: cl, Scheme: scheme}, cl
}

func TestReconcile_CreatesAgentPodAndSetsPendingStatus(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "smoke", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	r, cl := newTestReconciler(t, run)

	if res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "smoke", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	} else if res.Requeue {
		t.Fatalf("did not expect requeue on first reconcile, got %+v", res)
	}

	// The agent pod was created and carries the dagger call + runner host env.
	pod := &corev1.Pod{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "smoke-agent", Namespace: "default"}, pod); err != nil {
		t.Fatalf("expected agent pod created: %v", err)
	}
	if got := pod.Spec.Containers[0].Env[0].Name; got != "_EXPERIMENTAL_DAGGER_RUNNER_HOST" {
		t.Errorf("agent pod env[0] = %q, want _EXPERIMENTAL_DAGGER_RUNNER_HOST", got)
	}
	if cmd := pod.Spec.Containers[0].Command[2]; !strings.Contains(cmd, "dagger call probe-net") {
		t.Errorf("agent pod command missing `dagger call probe-net`: %q", cmd)
	}

	// Run status reflects the freshly-created (Pending) pod.
	updated := &v1alpha1.Run{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "smoke", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated run: %v", err)
	}
	if updated.Status.Phase != v1alpha1.RunPhasePending {
		t.Errorf("status phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhasePending)
	}
	if updated.Status.AgentPodName != "smoke-agent" {
		t.Errorf("status agentPodName = %q, want smoke-agent", updated.Status.AgentPodName)
	}
}

func TestReconcile_MirrorsSucceededPodPhase(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "smoke", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	r, cl := newTestReconciler(t, run)
	ctx := context.Background()
	key := types.NamespacedName{Name: "smoke", Namespace: "default"}
	req := ctrl.Request{NamespacedName: key}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Simulate the agent pod completing successfully.
	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "smoke-agent", Namespace: "default"}, pod); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	pod.Status.Phase = corev1.PodSucceeded
	if err := cl.Status().Update(ctx, pod); err != nil {
		t.Fatalf("update pod status: %v", err)
	}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	updated := &v1alpha1.Run{}
	if err := cl.Get(ctx, key, updated); err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.Status.Phase != v1alpha1.RunPhaseSucceeded {
		t.Errorf("status phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhaseSucceeded)
	}
	found := false
	for _, c := range updated.Status.Conditions {
		if c.Type == conditionAvailable && c.Status == metav1.ConditionTrue {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Available=True condition; got %+v", updated.Status.Conditions)
	}
}

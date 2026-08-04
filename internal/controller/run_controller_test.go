package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testModuleRef = "github.com/denkhaus/dagmar"

// newTestReconciler builds a RunReconciler over a fake client seeded with a Running engine pod,
// returning the client so the test can mutate cluster state. The Run is added by the caller.
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
			Labels: map[string]string{engineLabelKey: engineLabelVal},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: run.Namespace},
		Spec:       v1alpha1.ProjectSpec{Repo: "https://github.com/denkhaus/dagmar", ModuleRef: testModuleRef},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.Run{}).
		WithObjects(project, enginePod, run).
		Build()
	return &RunReconciler{Client: cl, Scheme: scheme}, cl
}

func TestReconcile_CreatesAgentIdentityAndPod(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "smoke", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	r, cl := newTestReconciler(t, run)
	ctx := context.Background()

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "smoke", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Agent identity (Phase-0 RBAC for pods/exec on the engine).
	if err := cl.Get(ctx, types.NamespacedName{Name: agentSA, Namespace: "default"}, &corev1.ServiceAccount{}); err != nil {
		t.Errorf("agent SA not created: %v", err)
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: agentRole, Namespace: engineNamespace}, &rbacv1.Role{}); err != nil {
		t.Errorf("agent Role not created: %v", err)
	}
	rb := &rbacv1.RoleBinding{}
	if err := cl.Get(ctx, types.NamespacedName{Name: agentRole, Namespace: engineNamespace}, rb); err != nil {
		t.Fatalf("agent RoleBinding not created: %v", err)
	}
	if rb.Subjects[0].Namespace != "default" || rb.Subjects[0].Name != agentSA {
		t.Errorf("RoleBinding subject = %+v, want SA dagmar-agent in default", rb.Subjects[0])
	}

	// The agent pod carries the module ref, the runner host, and the agent SA.
	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "smoke-agent", Namespace: "default"}, pod); err != nil {
		t.Fatalf("expected agent pod created: %v", err)
	}
	if pod.Spec.ServiceAccountName != agentSA {
		t.Errorf("agent pod ServiceAccountName = %q, want %q", pod.Spec.ServiceAccountName, agentSA)
	}
	if cmd := pod.Spec.Containers[0].Command[2]; !strings.Contains(cmd, "dagger call -m "+testModuleRef+" probe-net") {
		t.Errorf("agent pod command missing `dagger call -m %s probe-net`: %q", testModuleRef, cmd)
	}
	if env := pod.Spec.Containers[0].Env[0]; env.Name != "_EXPERIMENTAL_DAGGER_RUNNER_HOST" || !strings.Contains(env.Value, "dagger-engine-0") {
		t.Errorf("runner host env = %+v, want kube-pod://dagger-engine-0…", env)
	}

	// Run status reflects the freshly-created (Pending) pod.
	updated := &v1alpha1.Run{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "smoke", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated run: %v", err)
	}
	if updated.Status.Phase != v1alpha1.RunPhasePending {
		t.Errorf("status phase = %q, want %q", updated.Status.Phase, v1alpha1.RunPhasePending)
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
}

func TestReconcile_EmptyModuleRefIsTerminalFailed(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "no-mod", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	// Project with an empty ModuleRef (the Phase-0 load-bearing read).
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: "default"},
		Spec:       v1alpha1.ProjectSpec{Repo: "x"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.Run{}).WithObjects(project, run).Build()
	r := &RunReconciler{Client: cl, Scheme: scheme}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-mod", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("empty ModuleRef must be terminal, got requeue %+v", res)
	}
	updated := &v1alpha1.Run{}
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "no-mod", Namespace: "default"}, updated)
	if updated.Status.Phase != v1alpha1.RunPhaseFailed {
		t.Errorf("status phase = %q, want Failed", updated.Status.Phase)
	}
}

func TestReconcile_EngineNotReadyRequeues(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "early", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: "default"},
		Spec:       v1alpha1.ProjectSpec{Repo: "x", ModuleRef: testModuleRef},
	}
	// Engine pod exists but is NOT Running yet.
	enginePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "eng", Namespace: engineNamespace, Labels: map[string]string{engineLabelKey: engineLabelVal}},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.Run{}).WithObjects(project, enginePod, run).Build()
	r := &RunReconciler{Client: cl, Scheme: scheme}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "early", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter != engineRequeueAfter {
		t.Errorf("engine not ready must requeue after %v, got %+v", engineRequeueAfter, res)
	}
}

package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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
	// D5: the Succeeded terminal sets Succeeded=True, Progressing=False, Accepted=True (ran + succeeded).
	assertCondition(t, updated, v1alpha1.RunConditionSucceeded, metav1.ConditionTrue)
	assertCondition(t, updated, v1alpha1.RunConditionProgressing, metav1.ConditionFalse)
	assertCondition(t, updated, v1alpha1.RunConditionAccepted, metav1.ConditionTrue)
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
	// D5 (ADR-0013): a validation rejection is Accepted=False + Failed=True (not a dispatched Run
	// that failed — it was never admitted). No AgentPodName (no pod on this path).
	assertCondition(t, updated, v1alpha1.RunConditionAccepted, metav1.ConditionFalse)
	assertCondition(t, updated, v1alpha1.RunConditionFailed, metav1.ConditionTrue)
	if updated.Status.AgentPodName != "" {
		t.Errorf("rejected Run must not set AgentPodName, got %q", updated.Status.AgentPodName)
	}
}

func TestReconcile_NeitherModuleFunctionNorWorkflowRefIsTerminalFailed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	// Run with an empty ModuleFunction (review-13 HOUSE-4: symmetric with ModuleRef).
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "no-fn", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: ""},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: "default"},
		Spec:       v1alpha1.ProjectSpec{Repo: "x", ModuleRef: testModuleRef},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.Run{}).WithObjects(project, run).Build()
	r := &RunReconciler{Client: cl, Scheme: scheme}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-fn", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("empty ModuleFunction must be terminal, got requeue %+v", res)
	}
	updated := &v1alpha1.Run{}
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "no-fn", Namespace: "default"}, updated)
	if updated.Status.Phase != v1alpha1.RunPhaseFailed {
		t.Errorf("status phase = %q, want Failed (ModuleFunctionRequired)", updated.Status.Phase)
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

func TestReconcile_GitCredentialsRefProjectsPATAndHelper(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "priv", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: "default"},
		Spec: v1alpha1.ProjectSpec{
			Repo: "https://github.com/denkhaus/dagmar", ModuleRef: testModuleRef,
			// Key empty ⇒ defaults to "token" (exercises the default-key path).
			GitCredentialsRef: &v1alpha1.GitCredentialsRef{Name: "dagmar-git-creds"},
		},
	}
	enginePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "eng", Namespace: engineNamespace, Labels: map[string]string{engineLabelKey: engineLabelVal}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dagmar-git-creds", Namespace: "default"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.Run{}).
		WithObjects(project, enginePod, run, secret).Build()
	r := &RunReconciler{Client: cl, Scheme: scheme}
	ctx := context.Background()

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "priv", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "priv-agent", Namespace: "default"}, pod); err != nil {
		t.Fatalf("agent pod not created: %v", err)
	}
	cmd := pod.Spec.Containers[0].Command[2]
	for want, why := range map[string]string{
		"apk add --no-cache kubectl curl git":            "git installed for the credential helper",
		"git config --global credential.helper":          "headless git credential helper configured",
		`echo password="$DAGMAR_GIT_PAT"`:                "helper emits the projected PAT (quoted)",
		"dagger call -m " + testModuleRef + " probe-net": "module call still present",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("agent pod command missing %q (%s): %q", want, why, cmd)
		}
	}

	// The PAT is projected from the Secret (default key "token"), never interpolated into the cmd.
	var patEnv *corev1.EnvVar
	for i := range pod.Spec.Containers[0].Env {
		if pod.Spec.Containers[0].Env[i].Name == gitCredsEnvVar {
			patEnv = &pod.Spec.Containers[0].Env[i]
			break
		}
	}
	if patEnv == nil {
		t.Fatalf("agent pod env missing %q", gitCredsEnvVar)
	}
	if patEnv.ValueFrom == nil || patEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("DAGMAR_GIT_PAT must be a secretKeyRef, got %+v", patEnv)
	}
	if sr := patEnv.ValueFrom.SecretKeyRef; sr.Name != "dagmar-git-creds" || sr.Key != gitCredsDefaultKey {
		t.Errorf("secretKeyRef = {Name:%s, Key:%s}, want {dagmar-git-creds, %s}", sr.Name, sr.Key, gitCredsDefaultKey)
	}
}

func TestReconcile_MissingGitCredentialsSecretIsTerminalFailed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "nocred", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	project := &v1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "dagmar-own", Namespace: "default"},
		Spec: v1alpha1.ProjectSpec{
			Repo: "x", ModuleRef: testModuleRef,
			GitCredentialsRef: &v1alpha1.GitCredentialsRef{Name: "missing-creds"},
		},
	}
	// No Secret "missing-creds" is seeded.
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.Run{}).WithObjects(project, run).Build()
	r := &RunReconciler{Client: cl, Scheme: scheme}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "nocred", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("missing git-creds Secret must be terminal, got requeue %+v", res)
	}
	updated := &v1alpha1.Run{}
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "nocred", Namespace: "default"}, updated)
	if updated.Status.Phase != v1alpha1.RunPhaseFailed {
		t.Errorf("status phase = %q, want Failed (GitCredentialsSecretNotFound)", updated.Status.Phase)
	}
}

// TestReconcile_WritesPinnedConditionSet exercises the ADR-0013 D5 condition taxonomy across the
// happy-path pod lifecycle: a freshly-created pod (Pending) → Accepted=True/Progressing=False; the
// pod Running → Progressing=True; the pod Succeeded → Succeeded=True/Progressing=False. Phase is
// derived from the conditions at each step.
func TestReconcile_WritesPinnedConditionSet(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "conds", Namespace: "default"},
		Spec:       v1alpha1.RunSpec{ProjectRef: "dagmar-own", ModuleFunction: "probe-net"},
	}
	r, cl := newTestReconciler(t, run)
	ctx := context.Background()
	key := types.NamespacedName{Name: "conds", Namespace: "default"}
	req := ctrl.Request{NamespacedName: key}
	podKey := types.NamespacedName{Name: "conds-agent", Namespace: "default"}

	// (a) Freshly-created pod (phase "") → Accepted=True, Progressing=False, Phase=Pending. This is
	// the "dispatched but not yet executing" state — distinct from Running (Progressing=True).
	mustReconcile(t, ctx, r, req)
	updated := fetchRun(t, cl, key)
	assertCondition(t, updated, v1alpha1.RunConditionAccepted, metav1.ConditionTrue)
	assertCondition(t, updated, v1alpha1.RunConditionProgressing, metav1.ConditionFalse)
	if updated.Status.Phase != v1alpha1.RunPhasePending {
		t.Errorf("(a) phase = %q, want Pending", updated.Status.Phase)
	}
	if updated.Status.AgentPodName != "conds-agent" {
		t.Errorf("(a) AgentPodName = %q, want conds-agent", updated.Status.AgentPodName)
	}

	// (b) Pod Running → Progressing=True, Phase=Running.
	setPodPhase(t, cl, podKey, corev1.PodRunning)
	mustReconcile(t, ctx, r, req)
	updated = fetchRun(t, cl, key)
	assertCondition(t, updated, v1alpha1.RunConditionProgressing, metav1.ConditionTrue)
	if updated.Status.Phase != v1alpha1.RunPhaseRunning {
		t.Errorf("(b) phase = %q, want Running", updated.Status.Phase)
	}

	// (c) Pod Failed → Failed=True, Accepted STAYS True (ran-but-failed — distinct from a rejection
	// where Accepted=False), Progressing=False, Phase=Failed. AgentPodName is kept.
	setPodPhase(t, cl, podKey, corev1.PodFailed)
	mustReconcile(t, ctx, r, req)
	updated = fetchRun(t, cl, key)
	assertCondition(t, updated, v1alpha1.RunConditionAccepted, metav1.ConditionTrue)
	assertCondition(t, updated, v1alpha1.RunConditionFailed, metav1.ConditionTrue)
	assertCondition(t, updated, v1alpha1.RunConditionProgressing, metav1.ConditionFalse)
	if updated.Status.Phase != v1alpha1.RunPhaseFailed {
		t.Errorf("(c) phase = %q, want Failed", updated.Status.Phase)
	}
	if updated.Status.AgentPodName != "conds-agent" {
		t.Errorf("(c) AgentPodName = %q, want conds-agent (pod-Failed keeps the pod name)", updated.Status.AgentPodName)
	}
}

// assertCondition fails the test if run's condition condType is absent or not the wanted status.
func assertCondition(t *testing.T, run *v1alpha1.Run, condType string, want metav1.ConditionStatus) {
	t.Helper()
	c := meta.FindStatusCondition(run.Status.Conditions, condType)
	if c == nil {
		types := make([]string, len(run.Status.Conditions))
		for i, x := range run.Status.Conditions {
			types[i] = x.Type
		}
		t.Errorf("condition %q missing (have %v)", condType, types)
		return
	}
	if c.Status != want {
		t.Errorf("condition %q = %s, want %s", condType, c.Status, want)
	}
}

// setPodPhase updates the agent pod's status phase via the fake client (simulating the pod-watch
// transition the kubelet would drive).
func setPodPhase(t *testing.T, cl client.Client, key types.NamespacedName, phase corev1.PodPhase) {
	t.Helper()
	pod := &corev1.Pod{}
	if err := cl.Get(context.Background(), key, pod); err != nil {
		t.Fatalf("get pod %s: %v", key.Name, err)
	}
	pod.Status.Phase = phase
	if err := cl.Status().Update(context.Background(), pod); err != nil {
		t.Fatalf("update pod status: %v", err)
	}
}

func mustReconcile(t *testing.T, ctx context.Context, r *RunReconciler, req ctrl.Request) {
	t.Helper()
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func fetchRun(t *testing.T, cl client.Client, key types.NamespacedName) *v1alpha1.Run {
	t.Helper()
	run := &v1alpha1.Run{}
	if err := cl.Get(context.Background(), key, run); err != nil {
		t.Fatalf("get run %s: %v", key.Name, err)
	}
	return run
}

func TestReconcile_CognitionRunInjectsWorkspaceAndPrompt(t *testing.T) {
	run := &v1alpha1.Run{
		ObjectMeta: metav1.ObjectMeta{Name: "cog-1", Namespace: "default"},
		Spec: v1alpha1.RunSpec{
			ProjectRef:     "dagmar-own",
			AgentRef:       "coder-agent",
			ModuleFunction: "code",
		},
	}
	r, cl := newTestReconciler(t, run)

	// Create the Agent.
	agent := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "coder-agent", Namespace: "default"},
		Spec: v1alpha1.AgentSpec{
			Model:       "test-model",
			MaxAPICalls: 50,
			Prompt:      v1alpha1.PromptRef{ProjectPrompt: "coder-prompt"},
		},
	}
	if err := cl.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mustReconcile(t, ctx, r, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cog-1", Namespace: "default"}})

	// The pod should exist with the cognition-specific command.
	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: agentPodName("cog-1"), Namespace: "default"}, pod); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	cmd := pod.Spec.Containers[0].Command[2]
	// Should contain workspace clone + prompt + code() args.
	assertContains(t, cmd, "git clone")
	assertContains(t, cmd, "/workspace")
	assertContains(t, cmd, "/tmp/prompt.md")
	assertContains(t, cmd, "coder-prompt")
	assertContains(t, cmd, "--model")
	assertContains(t, cmd, "test-model")
	assertContains(t, cmd, "--max-api-calls")
	assertContains(t, cmd, "50")
	assertContains(t, cmd, "--module-ref")
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected command to contain %q, got: %s", substr, s[:min(len(s), 200)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
